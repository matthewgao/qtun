package transport

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/matthewgao/qtun/config"
)

type Server struct {
	publicAddr string
	// privateAddr string
	handler ServerHandler
	key     string
	config  config.Config

	publicListener *net.TCPListener
	// privateListener *net.TCPListener

	Mtx *sync.Mutex

	//为了能够删除已经断开的连接，并能够反过来查询连接，所以有两个map
	Conns        map[string]*ServerConn
	ConnsReverse map[*net.TCPConn]string
}

func NewServer(publicAddr, privateAddr string, handler ServerHandler, key string) *Server {
	srv := &Server{
		publicAddr: publicAddr,
		// privateAddr:  privateAddr,
		handler:      handler,
		key:          key,
		Conns:        make(map[string]*ServerConn),
		ConnsReverse: make(map[*net.TCPConn]string),
		Mtx:          &sync.Mutex{},
	}
	return srv
}

func (s *Server) SetConfig(cfg config.Config) {
	s.config = cfg
}

func (s *Server) Start() {
	if s.config.ServerMode == 1 {
		go s.processPublic()
	}
	//Private is only use to get packet from local
	// go s.processPrivate()
}

func (s *Server) processPublic() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("public server panic: %s", err)
		}
		log.Printf("public server closed")
	}()
	for {
		tcpAddr, err := net.ResolveTCPAddr("tcp", s.publicAddr)
		if err != nil {
			log.Printf("net resolve tcp addr err: %s", err)
			time.Sleep(time.Second * 5)
			continue
		}
		err = s.listen(tcpAddr)
		if err != nil {
			log.Printf("server listen err: %s", err)
		}
		time.Sleep(time.Millisecond * 1000)
	}
}

// func (s *Server) processPrivate() {
// 	defer func() {
// 		if err := recover(); err != nil {
// 			log.Printf("private server panic: %s", err)
// 		}
// 		log.Printf("private server closed")
// 	}()
// 	for {
// 		tcpAddr, err := net.ResolveTCPAddr("tcp", s.privateAddr)
// 		if err != nil {
// 			log.Printf("net resolve tcp addr err: %s", err)
// 			time.Sleep(time.Second * 5)
// 			continue
// 		}

// 		err = s.listen(tcpAddr)
// 		if err != nil {
// 			log.Printf("server listen err: %s", err)
// 		}
// 		time.Sleep(time.Millisecond * 1000)
// 	}
// }

func (s *Server) listen(tcpAddr *net.TCPAddr) error {
	defer func() {
		log.Printf("server listener closed")
	}()
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("net listen tcp err: %s", err)
	}
	defer listener.Close()
	for {
		tcpConn, err := listener.AcceptTCP()
		if nil != err {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			return fmt.Errorf("server accept err: %s", err)
		} else {
			log.Printf("new accept from %s", tcpConn.RemoteAddr())
			serverConn := NewServerConn(tcpConn, s.key, s.handler)
			//FIXME:have to control only one can connect to this now
			remoteAddr := tcpConn.RemoteAddr().String()
			log.Printf("add to conn map, %s", remoteAddr)
			// log.Printf("dump conn map, %v", s.Conns)

			go serverConn.run(func() {
				s.RemoveConnByConnPointer(serverConn.conn)
				log.Printf("Connections map: %v", s.Conns)
			})
		}
	}
}

func (s *Server) GetConnsByAddr(dst string) *ServerConn {
	// s.Mtx.Lock()
	// defer s.Mtx.Unlock()
	conn, ok := s.Conns[dst]
	if ok {
		return conn
	}
	return nil
}

func (s *Server) SetConns(dst string, conn *net.TCPConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	if _, ok := s.Conns[dst]; !ok {
		s.Conns[dst] = NewServerConn(conn, s.key, s.handler)
		//Urgly 应该保证连接只run一下，只为了writebuf里面的内容可以正确的被处理，现在run了两次, 应该把读和写都统一在一个对象里管理
		go s.Conns[dst].ProcessWrite()
		s.ConnsReverse[conn] = dst
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
