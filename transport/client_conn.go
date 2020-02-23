package transport

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/matthewgao/qtun/utils"
	"github.com/rs/zerolog/log"
)

var nilBuf = make([]byte, 0)

const BUF_SIZE = 1024 * 1024 * 1

type ClientConn struct {
	remoteAddr string
	key        string
	conn       *net.TCPConn
	index      int
	mutex      sync.RWMutex
	aesgcm     cipher.AEAD
	// aesgcmwrt  cipher.AEAD
	chanWrite chan []byte
	chanClose chan bool
	wg        sync.WaitGroup
	parentWG  *sync.WaitGroup
	connected bool
	// nonce     []byte
	buf     *bytes.Buffer
	readBuf []byte
	handler GrpcHandler
	reader  *bufio.Reader
	noDelay bool
}

func NewClientConn(remoteAddr, key string, index int, parentWG *sync.WaitGroup, noDelay bool) *ClientConn {
	return &ClientConn{
		remoteAddr: remoteAddr,
		key:        key,
		index:      index,
		chanWrite:  make(chan []byte, 2),
		chanClose:  make(chan bool),
		parentWG:   parentWG,
		// nonce:      make([]byte, 12),
		// buf:        &bytes.Buffer{},
		buf:     bytes.NewBuffer(make([]byte, 4096)),
		readBuf: make([]byte, 65535),
		noDelay: noDelay,
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
	if this.IsConnected() {
		log.Info().Str("server_addr", this.remoteAddr).Msg("connection is alive skip try connect")
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", this.remoteAddr)
	if err != nil {
		return err
	}

	this.conn, err = net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return err
	}

	this.conn.SetReadBuffer(BUF_SIZE)
	this.conn.SetWriteBuffer(BUF_SIZE)

	this.conn.SetNoDelay(this.noDelay)
	this.conn.SetKeepAlive(true)
	this.conn.SetKeepAlivePeriod(time.Second * 10)
	// this.conn.SetReadDeadline(time.Now().Add(timeoutDuration))

	return nil
}

func (this *ClientConn) crypto() (err error) {
	if this.key == "" {
		log.Info().Str("server_addr", this.remoteAddr).
			Msg("outgoing encryption disabled")
		return nil
	}

	this.aesgcm, err = makeAES128GCM(this.key)
	// this.aesgcmwrt, err = makeAES128GCM(this.key)
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
			go this.writeProcess()
			err = this.readProcess()
			if err != nil {
				log.Error().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
					Msg("client exit from process ")
				// break
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
		if this.conn != nil {
			this.conn.Close()
		}

		log.Error().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
			Msg("client conn closed")

	}()
	// this.setConnected(true)

	log.Info().Int("thread_index", this.index).Str("server_addr", this.remoteAddr).
		Msg("success connect to server")

	// pingTicker := time.NewTicker(time.Second * 1)
	// defer pingTicker.Stop()
	for {
		select {
		case <-this.chanClose:
			return nil
		// case <-pingTicker.C:
		// err = this.write(nilBuf)
		case buf := <-this.chanWrite:
			err = this.write(buf)
		}

		if err != nil {
			log.Error().Err(err).Str("server_addr", this.remoteAddr).
				Msg("process fail")
			return err
		}
	}
}

func (this *ClientConn) write(data []byte) error {
	if this.conn == nil {
		return fmt.Errorf("no connection")
	}

	if len(data) > 1600 {
		log.Warn().Int("data_size", len(data)).Msg("write data size gt 1600")
	}

	log.Debug().Int("data_size", len(data)).Msg("write data size")

	var err error
	this.buf.Reset()
	var secure uint8 = 0
	if this.aesgcm != nil {
		secure = 1
	}
	err = binary.Write(this.buf, binary.LittleEndian, secure)
	if err != nil {
		return err
	}

	// magicNum := uint16(11111)
	// err = binary.Write(this.buf, binary.LittleEndian, magicNum)
	// if err != nil {
	// 	return err
	// }

	if secure == 0 {
		dataLen := uint16(len(data))
		err = binary.Write(this.buf, binary.LittleEndian, dataLen)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(data)
		if err != nil {
			return err
		}
	} else {
		nonce := make([]byte, this.aesgcm.NonceSize())
		// nonce := []byte("xxxxxxxxxxxx")
		_, err = io.ReadFull(crand.Reader, nonce)
		if err != nil {
			return err
		}

		data2 := this.aesgcm.Seal(nil, nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(this.buf, binary.LittleEndian, &dataLen)
		if err != nil {
			log.Error().Err(err).Uint16("datalen", dataLen).Msg("ClientConn::write data len fail")
			return err
		}
		n, err := this.buf.Write(data2)
		if err != nil {
			log.Error().Err(err).Int("dataSize", len(data2)).Int("writeSize", n).Msg("ClientConn::write data fail")
			return err
		}

		if n != len(data2) {
			log.Error().Int("dataSize", len(data2)).Int("writeSize", n).Msg("ClientConn::write is not eq data size")
		}

		_, err = this.buf.Write(nonce)
		if err != nil {
			log.Error().Err(err).Int("dataSize", len(data2)).Int("writeSize", n).Msg("ClientConn::write nonce fail")
			return err
		}
	}

	if err == nil {
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

func (this *ClientConn) WriteNow(data []byte) error {
	// return this.write(data)
	this.chanWrite <- data
	return nil
}

func (sc *ClientConn) readProcess() error {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Err(err.(error)).Int("thread_index", sc.index).
				Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead painc")
		}
		if sc.conn != nil {
			sc.conn.Close()
		}
		sc.setConnected(false)
	}()
	var err error
	err = sc.crypto()
	utils.POE(err)

	sc.reader = bufio.NewReaderSize(sc.conn, BUF_SIZE)
	for {
		data, err := sc.read()

		if err != nil {
			log.Error().Err(err).Int("thread_index", sc.index).Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead conn read fail, break")
			// break
			return err
		}

		if sc.handler != nil {
			sc.handler.OnData(data, sc.conn)
		} else {
			log.Error().Int("thread_index", sc.index).Str("server_addr", sc.remoteAddr).
				Msg("ClientConn::runRead handler is null")
		}
	}
}

func (sc *ClientConn) read() ([]byte, error) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		log.Error().Err(err.(error)).
	// 			Msg("ClientConn::read conn run fail")
	// 		fmt.Println("stacktrace from panic: \n" + string(debug.Stack()))
	// 	}
	// }()
	var err error
	var secure uint8 = 0
	reader := sc.reader
	err = binary.Read(reader, binary.LittleEndian, &secure)
	if err != nil {
		return nil, err
	}

	// var magicNum uint16
	// err = binary.Read(reader, binary.LittleEndian, &magicNum)
	// if err != nil {
	// 	return nil, err
	// }

	var dataLen uint16 = 0
	err = binary.Read(reader, binary.LittleEndian, &dataLen)
	if err != nil {
		return nil, err
	}

	// log.Debug().Uint16("dataLen", dataLen).Uint16("magic_num", magicNum).Msg("ClientConn::read datelen")

	_, err = io.ReadFull(reader, sc.readBuf[:dataLen])
	// _, err = io.ReadFull(reader, sc.readBuf)
	if err != nil {
		return nil, err
	}
	if secure == 0 {
		return sc.readBuf[:dataLen], err
	}

	nonce := make([]byte, sc.aesgcm.NonceSize())
	_, err = io.ReadFull(reader, nonce)
	if err != nil {
		return nil, err
	}

	plain, err := sc.aesgcm.Open(nil, nonce, sc.readBuf[:dataLen], nil)
	if err != nil {
		log.Error().Str("plain", string(plain)).Str("nonce", string(nonce)).Int("datalen", int(dataLen)).Err(err).
			Int("bufsize", len(sc.readBuf)).Bytes("data", sc.readBuf[:dataLen]).Msg("ClientConn::runRead aesgcm open fail")
		return nil, err
	}

	// log.Info().Int("plain_len", len(plain)).Str("nonce", string(sc.nonce)).Int("datalen", int(dataLen)).Err(err).
	// 	Int("bufsize", len(sc.readBuf)).Msg("ClientConn::runRead")

	return plain, nil
}

//为了使用 10.4.4.3:port 这样的格式来表示一条tcp连接
func (sc *ClientConn) GetConnPort() string {
	fullWithPort := sc.conn.LocalAddr().String()
	addrPair := strings.Split(fullWithPort, ":")
	// log.Printf("get local tcp addr %s", fullWithPort)
	if len(addrPair) < 2 {
		panic("fail to get local tcp port")
	}
	return addrPair[1]
}
