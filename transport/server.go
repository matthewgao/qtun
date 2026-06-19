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

	"github.com/matthewgao/qtun/config"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

type Server struct {
	publicAddr string
	handler    GrpcHandler
	key        string
	// publicListener *net.TCPListener
	publicListener quic.Listener
	Mtx            *sync.Mutex

	// Optimized: Use sync.Map for better concurrent performance
	//为了能够删除已经断开的连接，并能够反过来查询连接，所以有两个map
	Conns        sync.Map // map[string]*ServerConn
	ConnsReverse sync.Map // map[*ServerConn]string
}

func NewServer(publicAddr string, handler GrpcHandler, key string) *Server {
	srv := &Server{
		publicAddr: publicAddr,
		handler:    handler,
		key:        key,
		Mtx:        &sync.Mutex{},
		// Optimized: sync.Map doesn't need initialization
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

	// Optimized: Configure QUIC with performance parameters
	quicConfig := &quic.Config{
		MaxIncomingStreams: 1000, // Allow more concurrent streams
		// 针对 1Gbps@40ms 链路：BDP = 1e9*0.04/8 ≈ 5MB。默认 6MB 窗口仅 ~1.2×BDP，
		// 自动调窗没有上探空间。把上限提到 16MB（~3×BDP），并把初始窗口从默认 512KB
		// 提到 2MB，缩短窗口爬坡时间。两端（server/client_conn）必须保持一致。
		InitialStreamReceiveWindow:     2 * 1024 * 1024,  // 2MB initial stream window
		MaxStreamReceiveWindow:         16 * 1024 * 1024, // 16MB max stream window
		InitialConnectionReceiveWindow: 2 * 1024 * 1024,  // 2MB initial connection window
		MaxConnectionReceiveWindow:     24 * 1024 * 1024, // 24MB max connection window
		// client 重启后旧连接已死，但 server 要等 QUIC idle timeout 才发现；在此之前
		// 下行包会被 rand 分发到死连接上静默丢弃，表现为 client 侧间歇 ping timeout、
		// 内层 TCP connection reset。默认 idle timeout = 30s，故障窗口长达 30s。
		// 这里压到 5s，keepalive 2s（远小于 idle 的一半，避免空闲活连接被误断）。
		// idle timeout 取两端协商的较小值，server/client_conn 必须保持一致。
		MaxIdleTimeout:                 5 * time.Second,
		KeepAlivePeriod:                2 * time.Second,
		EnableDatagrams:                true,
	}

	// listener, err := net.ListenTCP("tcp", tcpAddr)
	listener, err := quic.ListenAddr(s.publicAddr, s.generateTLSConfig(), quicConfig)
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
	// Optimized: Increased RSA key size from 1024 to 2048 bits for better security
	key, err := rsa.GenerateKey(rand.Reader, 2048)
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
	// Optimized: Use sync.Map Load (lock-free reads)
	if conn, ok := s.Conns.Load(dst); ok {
		return conn.(*ServerConn)
	}
	return nil
}

func (s *Server) DeleteDeadConn(dst string) {
	// Optimized: Use sync.Map Delete (no lock needed)
	if connVal, ok := s.Conns.LoadAndDelete(dst); ok {
		conn := connVal.(*ServerConn)
		s.ConnsReverse.Delete(conn)
		log.Warn().Str("dest", dst).Msg("delete dead conn")
	}
}

func (s *Server) SetConns(dst string, serverConn *ServerConn) {
	// Optimized: Use sync.Map Store/LoadOrStore (no lock needed)
	if serverConn == nil {
		return
	}

	if existingVal, loaded := s.Conns.LoadOrStore(dst, serverConn); loaded {
		// Connection already exists
		existing := existingVal.(*ServerConn)
		if existing.conn == nil {
			existing.Stop()
			s.Conns.Store(dst, serverConn)
			s.ConnsReverse.Store(serverConn, dst)
		}
	} else {
		// New connection
		s.ConnsReverse.Store(serverConn, dst)
	}
}

func (s *Server) RemoveConnByConnPointer(conn *ServerConn) {
	// Optimized: Use sync.Map Delete (no lock needed)
	if dstVal, ok := s.ConnsReverse.LoadAndDelete(conn); ok {
		dst := dstVal.(string)
		s.Conns.Delete(dst)
	}
}
