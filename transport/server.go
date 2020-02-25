package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"

	// "log"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/matthewgao/qtun/config"
	"github.com/rs/zerolog/log"
)

type Server struct {
	publicAddr string
	handler    GrpcHandler
	key        string
	// publicListener *net.TCPListener
	publicListener quic.Listener
	Mtx            *sync.Mutex

	//为了能够删除已经断开的连接，并能够反过来查询连接，所以有两个map
	Conns        map[string]*ServerConn
	ConnsReverse map[*ServerConn]string
}

func NewServer(publicAddr string, handler GrpcHandler, key string) *Server {
	srv := &Server{
		publicAddr:   publicAddr,
		handler:      handler,
		key:          key,
		Conns:        make(map[string]*ServerConn),
		ConnsReverse: make(map[*ServerConn]string),
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
		// tcpAddr, err := net.ResolveTCPAddr("tcp", s.publicAddr)
		// if err != nil {
		// 	log.Error().Err(err).Str("addr", s.publicAddr).Msg("net resolve tcp addr fail")
		// 	time.Sleep(time.Second * 5)
		// 	continue
		// }
		err := s.listen()
		if err != nil {
			log.Error().Err(err).Str("addr", s.publicAddr).Msg("server listen fail")
		}
		time.Sleep(time.Second)
	}
}

// listener, err := quic.ListenAddr(addr, generateTLSConfig(), nil)
// if err != nil {
// 	return err
// }
// sess, err := listener.Accept(context.Background())
// if err != nil {
// 	return err
// }
// stream, err := sess.AcceptStream(context.Background())
// if err != nil {
// 	panic(err)
// }

func (s *Server) listen() error {
	defer func() {
		log.Info().Str("addr", s.publicAddr).Msg("server listener closed")
	}()

	// listener, err := net.ListenTCP("tcp", tcpAddr)
	listener, err := quic.ListenAddr(s.publicAddr, s.generateTLSConfig(), nil)
	if err != nil {
		return fmt.Errorf("Server::Listen::net listen tcp err: %s", err)
	}

	defer listener.Close()
	for {
		sess, err := listener.Accept(context.Background())
		if err != nil {
			return err
		}

		log.Debug().Str("addr", s.publicAddr).Msg("server accept start accept stream")
		stream, err := sess.AcceptStream(context.Background())
		if nil != err {

			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}

			log.Warn().Err(err).Str("addr", s.publicAddr).Msg("server accept fail")
			continue
		}

		log.Info().Str("from", sess.RemoteAddr().Network()).Msg("server new accept")
		// log.Info().Interface("from", stream).Msg("server new accept")

		serverConn := NewServerConn(stream, sess, s.key, s.handler, config.GetInstance().NoDelay)
		// s.ClientConns[sess.RemoteAddr().String()] = serverConn
		// log.Info().Int("conn_size", len(s.Conns)).
		// 	Int("reverse_size", len(s.ConnsReverse)).
		// 	Int("client_conn_size", len(s.ClientConns)).
		// 	Str("from", sess.RemoteAddr().String()).Msg("server start to read from connection")

		//start to read pkt from connection
		go serverConn.writeProcess()
		go serverConn.readProcess(func() {
			s.RemoveConnByConnPointer(serverConn)
			// log.Warn().Str("from", serverConn.conn.RemoteAddr().String()).
			// 	Interface("alive_conns", s.Conns).Msg("server read thread exit")
			log.Warn().Str("from", sess.RemoteAddr().String()).Msg("server read thread exit")
		})
	}
}

func (s *Server) generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
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
	}

	if conn != nil {
		delete(s.ConnsReverse, conn)
	}

	log.Warn().Str("dest", dst).Int("conn_size", len(s.Conns)).
		Int("reverse_size", len(s.ConnsReverse)).Msg("delete dead conn")
}

func (s *Server) SetConns(dst string, serverConn *ServerConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	if v, ok := s.Conns[dst]; !ok {
		if serverConn == nil {
			return
		}

		// serverConn := NewServerConn(conn, s.key, s.handler, config.GetInstance().NoDelay)
		//Urgly 应该保证连接只run一下，只为了writebuf里面的内容可以正确的被处理，现在run了两次, 应该把读和写都统一在一个对象里管理
		// go serverConn.ProcessWrite()
		s.Conns[dst] = serverConn
		s.ConnsReverse[serverConn] = dst
	} else {
		if v.conn == nil {
			v.Stop()
			delete(s.Conns, dst)
		}
	}
}

func (s *Server) RemoveConnByConnPointer(conn *ServerConn) {
	s.Mtx.Lock()
	defer s.Mtx.Unlock()
	dst, ok := s.ConnsReverse[conn]
	if ok {
		delete(s.Conns, dst)
	}
	delete(s.ConnsReverse, conn)
}
