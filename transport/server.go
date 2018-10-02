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
	publicAddr  string
	privateAddr string
	handler     ServerHandler
	key         string
	config      config.Config

	publicListener  *net.TCPListener
	privateListener *net.TCPListener

	Mtx   *sync.Mutex
	Conns map[string]*ServerConn
}

func NewServer(publicAddr, privateAddr string, handler ServerHandler, key string) *Server {
	srv := &Server{
		publicAddr:  publicAddr,
		privateAddr: privateAddr,
		handler:     handler,
		key:         key,
		Conns:       make(map[string]*ServerConn),
		Mtx:         &sync.Mutex{},
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

func (s *Server) processPrivate() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("private server panic: %s", err)
		}
		log.Printf("private server closed")
	}()
	for {
		tcpAddr, err := net.ResolveTCPAddr("tcp", s.privateAddr)
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
			log.Printf("dump conn map, %v", s.Conns)

			// s.Mtx.Lock()
			// s.Conns[remoteAddr] = serverConn
			// s.Mtx.Unlock()
			go serverConn.run(func() {
				s.Mtx.Lock()
				delete(s.Conns, remoteAddr)
				s.Mtx.Unlock()
			})
		}
	}
}

func (s *Server) GetConnsByAddr(dst string) *ServerConn {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	conn, ok := s.Conns[dst]
	if ok {
		return conn
	}
	return nil
}

func (s *Server) SetConns(dst string, conns *net.TCPConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	s.Conns[dst] = NewServerConn(conns, s.key, s.handler)
}
