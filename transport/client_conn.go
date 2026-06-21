package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/matthewgao/qtun/utils"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

var nilBuf = make([]byte, 0)

type ClientConn struct {
	remoteAddr string
	key        string
	conn       *quic.Stream
	session    *quic.Conn
	index      int
	mutex      sync.RWMutex
	aesgcm     cipher.AEAD
	chanWrite  chan []byte
	chanClose  chan bool
	wg         sync.WaitGroup
	parentWG   *sync.WaitGroup
	connected  bool
	buf        *bytes.Buffer
	readBuf    []byte
	handler    GrpcHandler
	reader     *bufio.Reader
	noDelay    bool
}

func NewClientConn(remoteAddr, key string, index int, parentWG *sync.WaitGroup, noDelay bool) *ClientConn {
	return &ClientConn{
		remoteAddr: remoteAddr,
		key:        key,
		index:      index,
		chanWrite:  make(chan []byte, 256), // Optimized: Increased from unbuffered to 256
		chanClose:  make(chan bool, 1),     // Optimized: Added buffer
		parentWG:   parentWG,
		buf:        &bytes.Buffer{},
		readBuf:    make([]byte, 65536),
		noDelay:    noDelay,
	}
}

func (this *ClientConn) SetHander(handler GrpcHandler) {
	this.handler = handler
}

func (this *ClientConn) IsConnected() bool {
	this.mutex.RLock()
	connected := this.connected
	this.mutex.RUnlock()
	return connected
}

func (this *ClientConn) tryConnect() error {
	// tcpAddr, err := net.ResolveTCPAddr("tcp", this.remoteAddr)
	// if err != nil {
	// 	return err
	// }
	if this.IsConnected() {
		log.Info().Str("server_addr", this.remoteAddr).Msg("connection is alive skip try connect")
		return nil
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}

	// Optimized: Configure QUIC with performance parameters
	// 流控窗口需与服务端 (server.go) 保持一致，见那里的说明。
	quicConfig := &quic.Config{
		MaxIncomingStreams:             1000,             // Allow more concurrent streams
		InitialStreamReceiveWindow:     2 * 1024 * 1024,  // 2MB initial stream window
		MaxStreamReceiveWindow:         16 * 1024 * 1024, // 16MB max stream window
		InitialConnectionReceiveWindow: 2 * 1024 * 1024,  // 2MB initial connection window
		MaxConnectionReceiveWindow:     24 * 1024 * 1024, // 24MB max connection window
		// 与 server.go 保持一致：把 idle timeout 从默认 30s 压到 5s、keepalive 2s，
		// 缩短 client 重启后 server 发现旧连接已死的时间，减小静默丢包窗口。
		MaxIdleTimeout:                 5 * time.Second,
		KeepAlivePeriod:                2 * time.Second,
		EnableDatagrams:                true,
	}

	ctx := context.Background()

	session, err := quic.DialAddr(ctx, this.remoteAddr, tlsConf, quicConfig)
	if err != nil {
		return err
	}

	this.session = session

	this.conn, err = this.session.OpenStreamSync(context.Background())
	if err != nil {
		// this.session.Close()
		this.session.CloseWithError(0x2, "fail to open stream sync")
		return err
	}

	// this.conn.SetReadBuffer(1024 * 1024)
	// this.conn.SetWriteBuffer(1024 * 1024)
	// this.conn.SetNoDelay(this.noDelay)
	// this.conn.SetKeepAlive(true)
	// this.conn.SetKeepAlivePeriod(time.Second * 10)
	// this.conn.SetReadDeadline(time.Now().Add(timeoutDuration))
	log.Info().Str("server_addr", this.remoteAddr).Msg("try connect success")
	return nil
}

func (this *ClientConn) crypto() (err error) {
	if this.key == "" {
		log.Info().Str("server_addr", this.remoteAddr).
			Msg("outgoing encryption disabled")
		return nil
	}

	this.aesgcm, err = makeAES128GCM(this.key)
	return
}

func (this *ClientConn) InitConn() error {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).Int("thread_index", this.index).
				Str("server_addr", this.remoteAddr).
				Msg("InitConn::client connection thread panic")
		}
	}()

	err := this.crypto()
	utils.POE(err)

	err = this.tryConnect()
	if err != nil {
		log.Error().Err(err).Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
			Msg("connect server fail")
		return err
	}

	this.setConnected(true)
	return nil
}

func (this *ClientConn) run() {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).Int("thread_index", this.index).
				Str("server_addr", this.remoteAddr).
				Msg("client connection thread panic")
		}
		this.wg.Done()
	}()

	this.wg.Add(1)
	err := this.crypto()
	utils.POE(err)
	for {
		select {
		case <-this.chanClose:
			return
		default:
		}

		err := this.tryConnect()
		if err != nil {
			log.Error().Err(err).Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
				Msg("connect server fail")
			time.Sleep(time.Millisecond * 1000)
		} else {
			go this.writeProcess()    // 控制面：stream 发送 ping
			go this.readDatagrams()   // 数据面：接收 datagram IP 报文
			err = this.readProcess()  // 控制面：stream 接收 + 关闭检测
			if err == nil {
				log.Error().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
					Msg("client exit from process ")
				break
			}
		}
	}
}

func (this *ClientConn) Close() {
	this.chanClose <- true
	this.wg.Wait()
	if this.parentWG != nil {
		this.parentWG.Done()
	}
}

func (this *ClientConn) setConnected(value bool) {
	this.mutex.Lock()
	this.connected = value
	this.mutex.Unlock()
}

func (this *ClientConn) writeProcess() (err error) {
	defer func() {
		if perr := recover(); perr != nil {
			err = fmt.Errorf("client process panic: %s", perr)
		}

		this.setConnected(false)
		this.conn.Close()
		// this.session.Close()
		this.session.CloseWithError(0x1, "fail to write")

		log.Error().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
			Msg("client conn closed")

	}()
	this.setConnected(true)

	log.Info().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
		Msg("success connect to server")

	// pingTicker := time.NewTicker(time.Second * 1)
	// defer pingTicker.Stop()
	for {
		select {
		case <-this.chanClose:
			return nil
		// case <-pingTicker.C:
		// 	// err = this.write(nilBuf)
		case buf := <-this.chanWrite:
			err = this.write(buf)
		}
		if err != nil {
			return err
		}
	}
}

func (this *ClientConn) write(data []byte) error {
	if this.conn == nil {
		return fmt.Errorf("no connection")
	}
	var err error
	this.buf.Reset()
	var secure uint8 = 0
	if this.aesgcm != nil {
		secure = 1
	}
	err = binary.Write(this.buf, binary.LittleEndian, &secure)
	if err != nil {
		return err
	}
	if secure == 0 {
		dataLen := uint16(len(data))
		err = binary.Write(this.buf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(data)
		if err != nil {
			return err
		}
	} else {
		// Optimized: Use nonce from pool
		nonce := getNonce()
		defer putNonce(nonce)

		_, err = io.ReadFull(crand.Reader, nonce)
		if err != nil {
			return err
		}
		data2 := this.aesgcm.Seal(nil, nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(this.buf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(data2)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(nonce)
		if err != nil {
			return err
		}
	}
	if err == nil {
		// this.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
		_, err = this.buf.WriteTo(this.conn)
	}
	return err
}

func (this *ClientConn) Write(data []byte) {
	if this == nil || this.chanWrite == nil {
		log.Warn().Msg("ClientConn::write conn not init, retry later")
		return
	}
	this.chanWrite <- data
}

// WriteDatagram 把一条已 marshal 的 Envelope（数据面 IP 报文）封包后通过 QUIC datagram
// 发送（不可靠，规避可靠 stream 的队头阻塞/双重重传）。可被多个 TUN worker 并发调用。
func (this *ClientConn) WriteDatagram(data []byte) {
	if this == nil || this.session == nil {
		return
	}
	framed := frameDatagram(this.aesgcm, data)
	if framed == nil {
		return
	}
	if err := this.session.SendDatagram(framed); err != nil {
		warnDatagramDrop(err, len(framed))
	}
}

// readDatagrams 接收数据面 datagram，解密后交给 handler.ClientOnData。控制面（关闭检测）
// 仍走 stream 的 readProcess。捕获本地 session 引用，避免重连时 this.session 被重新赋值
// 引发竞争；连接关闭时 ReceiveDatagram 返回 error，goroutine 退出。
func (this *ClientConn) readDatagrams() {
	sess := this.session
	if sess == nil {
		return
	}
	for {
		d, err := sess.ReceiveDatagram(context.Background())
		if err != nil {
			log.Debug().Err(err).Int("thread_index", this.index).
				Msg("ClientConn::readDatagrams exit")
			return
		}
		msg, err := decodeDatagram(this.aesgcm, d)
		if err != nil {
			log.Debug().Err(err).Msg("ClientConn::readDatagrams decode fail, drop")
			continue
		}
		if this.handler != nil {
			this.handler.ClientOnData(msg)
		}
	}
}

// func (this *ClientConn) WriteNow(data []byte) error {
// 	this.chanWrite <- data
// 	return nil
// }

func (sc *ClientConn) readProcess() error {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).Int("thread_index", sc.index).
				Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead painc")
		}
		if sc.conn != nil {
			sc.conn.Close()
		}
		// sc.session.Close()
		sc.session.CloseWithError(0x1, "fail to read")
		sc.setConnected(false)
	}()
	// crypto() 已在 run() 启动各 goroutine 前同步调用，这里不再重复，避免与
	// readDatagrams 并发读写 sc.aesgcm 造成数据竞争。

	// sc.conn.SetReadBuffer(1024 * 1024)
	// sc.conn.SetWriteBuffer(1024 * 1024)
	// sc.conn.SetNoDelay(sc.noDelay)

	// Optimized: Increased buffer from 4KB to 64KB for better throughput
	sc.reader = bufio.NewReaderSize(sc.conn, 1024*64)
	for {
		// sc.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
		data, err := sc.read()

		// if err == io.EOF {
		// 	log.Error().Err(err).Int("thread_index", sc.index).Str("server_addr", sc.remoteAddr).
		// 		Msg("ClientConn::runRead conn read fail, connection is closed by server")
		// 	return
		// }

		if err != nil {
			log.Error().Err(err).Int("thread_index", sc.index).Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead conn read fail, break")
			// break
			return err
		}

		if sc.handler != nil {
			sc.handler.ClientOnData(data)
		} else {
			log.Error().Int("thread_index", sc.index).Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead handler is null")
		}
	}
}

func (sc *ClientConn) read() ([]byte, error) {
	var err error
	var secure uint8 = 0
	reader := sc.reader
	err = binary.Read(reader, binary.LittleEndian, &secure)
	if err != nil {
		return nil, err
	}
	var dataLen uint16
	err = binary.Read(reader, binary.LittleEndian, &dataLen)
	if err != nil {
		return nil, err
	}
	_, err = io.ReadFull(reader, sc.readBuf[:dataLen])
	if err != nil {
		return nil, err
	}
	if secure == 0 {
		return sc.readBuf[:dataLen], err
	}

	// Optimized: Use nonce from pool
	nonce := getNonce()
	defer putNonce(nonce)

	_, err = io.ReadFull(reader, nonce)
	if err != nil {
		return nil, err
	}
	plain, err := sc.aesgcm.Open(nil, nonce, sc.readBuf[:dataLen], nil)
	if err != nil {
		return nil, err
	}

	return plain, nil
}

// 为了使用 10.4.4.3:port 这样的格式来表示一条tcp连接
func (sc *ClientConn) GetConnPort() string {
	// fullWithPort := sc.conn.LocalAddr().String()
	fullWithPort := sc.session.LocalAddr().String()
	addrPair := strings.Split(fullWithPort, ":")
	// log.Printf("get local tcp addr %s", fullWithPort)
	// fmt.Printf("get conn port %v\n", fullWithPort)
	if len(addrPair) < 3 {
		panic("fail to get local tcp port")
	}
	return addrPair[3]
}
