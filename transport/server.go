package transport

import (
	"fmt"
	// "log"
	"net"
	"sync"
	"time"

	"github.com/matthewgao/qtun/config"
	"github.com/rs/zerolog/log"
)

type Server struct {
	publicAddr     string
	handler        GrpcHandler
	key            string
	publicListener *net.TCPListener
	Mtx            *sync.Mutex

	//为了能够删除已经断开的连接，并能够反过来查询连接，所以有两个map
	Conns        map[string]*ServerConn
	ConnsReverse map[*net.TCPConn]string
}

func NewServer(publicAddr string, handler GrpcHandler, key string) *Server {
	srv := &Server{
		publicAddr:   publicAddr,
		handler:      handler,
		key:          key,
		Conns:        make(map[string]*ServerConn),
		ConnsReverse: make(map[*net.TCPConn]string),
		Mtx:          &sync.Mutex{},
	}
	return srv
}

func (s *Server) Start() {
	if config.GetInstance().ServerMode {
		go s.StartListen()
	}
}

func (s *Server) StartListen() {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).
				Msg("server listen panic")
		}

		log.Info().Str("addr", s.publicAddr).Msg("server listen exit, server closed")
	}()
	for {
		tcpAddr, err := net.ResolveTCPAddr("tcp", s.publicAddr)
		if err != nil {
			log.Error().Err(err).Str("addr", s.publicAddr).Msg("net resolve tcp addr fail")
			time.Sleep(time.Second * 5)
			continue
		}
		err = s.listen(tcpAddr)
		if err != nil {
			log.Error().Err(err).Str("addr", s.publicAddr).Msg("server listen fail")
		}
		time.Sleep(time.Second)
	}
}

func (s *Server) listen(tcpAddr *net.TCPAddr) error {
	defer func() {
		log.Info().Str("addr", s.publicAddr).Msg("server listener closed")
	}()

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("Server::Listen::net listen tcp err: %s", err)
	}
	defer listener.Close()
	for {
		tcpConn, err := listener.AcceptTCP()
		if nil != err {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}

			log.Warn().Err(err).Str("addr", s.publicAddr).Msg("server accept fail")
			continue
		}

		log.Info().Str("from", tcpConn.RemoteAddr().String()).Msg("server new accept")

		serverConn := NewServerConn(tcpConn, s.key, s.handler, config.GetInstance().NoDelay)
		log.Info().Int("conn_size", len(s.Conns)).
			Int("reverse_size", len(s.ConnsReverse)).
			Str("from", tcpConn.RemoteAddr().String()).Msg("server start to read from connection")
		//start to read pkt from connection
		go serverConn.run(func() {
			s.RemoveConnByConnPointer(serverConn.conn)
			log.Warn().Str("from", serverConn.conn.RemoteAddr().String()).
				Interface("alive_conns", s.Conns).Msg("server read thread exit")
		})
	}
}

func (s *Server) GetConnsByAddr(dst string) *ServerConn {
	//No need to add lock
	conn, ok := s.Conns[dst]
	if ok {
		return conn
	}
	return nil
}

func (s *Server) DeleteDeadConn(dst string) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()

	conn, ok := s.Conns[dst]
	if ok {
		delete(s.Conns, dst)
		if conn != nil {
			delete(s.ConnsReverse, conn.conn)
		}
	}

	log.Warn().Str("dest", dst).Int("conn_size", len(s.Conns)).
		Int("reverse_size", len(s.ConnsReverse)).Msg("delete dead conn")
}

func (s *Server) SetConns(dst string, conn *net.TCPConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	if v, ok := s.Conns[dst]; !ok {
		if conn == nil {
			return
		}

		serverConn := NewServerConn(conn, s.key, s.handler, config.GetInstance().NoDelay)
		//Urgly 应该保证连接只run一下，只为了writebuf里面的内容可以正确的被处理，现在run了两次, 应该把读和写都统一在一个对象里管理
		go serverConn.ProcessWrite()
		s.Conns[dst] = serverConn
		s.ConnsReverse[conn] = dst
	} else {
		if v.conn == nil {
			delete(s.Conns, dst)
		}
	}
}

func (s *Server) RemoveConnByConnPointer(conn *net.TCPConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	dst, ok := s.ConnsReverse[conn]
	if ok {
		delete(s.Conns, dst)
		delete(s.ConnsReverse, conn)
	}
}
