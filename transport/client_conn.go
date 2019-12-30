package transport

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/matthewgao/qtun/utils"
)

var nilBuf = make([]byte, 0)

type ClientConn struct {
	remoteAddr string
	key        string
	conn       *net.TCPConn
	index      int
	mutex      sync.RWMutex
	aesgcm     cipher.AEAD
	chanWrite  chan []byte
	chanClose  chan bool
	wg         sync.WaitGroup
	parentWG   *sync.WaitGroup
	connected  bool
	nonce      []byte
	buf        *bytes.Buffer
	readBuf    []byte
	handler    GrpcHandler
	reader     *bufio.Reader
}

func NewClientConn(remoteAddr, key string, index int, parentWG *sync.WaitGroup) *ClientConn {
	return &ClientConn{
		remoteAddr: remoteAddr,
		key:        key,
		index:      index,
		chanWrite:  make(chan []byte),
		chanClose:  make(chan bool),
		parentWG:   parentWG,
		nonce:      make([]byte, 12),
		buf:        &bytes.Buffer{},
		readBuf:    make([]byte, 65536),
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
	tcpAddr, err := net.ResolveTCPAddr("tcp", this.remoteAddr)
	if err != nil {
		return err
	}

	this.conn, err = net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return err
	}

	this.conn.SetReadBuffer(1024 * 1024)
	this.conn.SetWriteBuffer(1024 * 1024)
	this.conn.SetNoDelay(true)

	return nil
}

func (this *ClientConn) crypto() (err error) {
	if this.key == "" {
		log.Printf("outgoing encryption disabled for %s", this.remoteAddr)
		return nil
	}
	this.aesgcm, err = makeAES128GCM(this.key)
	return
}

func (this *ClientConn) run() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("transport thread %d addr %s panic: %s", this.index, this.remoteAddr, err)
		}
		this.wg.Done()
	}()
	this.wg.Add(1)
	var err error
	err = this.crypto()
	utils.POE(err)
	for {
		select {
		case <-this.chanClose:
			return
		default:
		}
		err := this.tryConnect()
		if err != nil {
			log.Printf("transport thread %d addr %s connect err: %s", this.index, this.remoteAddr, err)
			time.Sleep(time.Millisecond * 1000)
		} else {
			if err == nil {
				go this.runRead()
				err = this.process()
				if err == nil {
					break
				}
			}
			if err != nil {
				log.Printf("client err: %s", err)
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

func (this *ClientConn) process() (err error) {
	defer func() {
		if perr := recover(); perr != nil {
			err = fmt.Errorf("client process panic: %s", perr)
		}
		this.setConnected(false)
		this.conn.Close()
		log.Printf("client conn closed")
	}()
	this.setConnected(true)
	log.Printf("connection good to %s : %d", this.remoteAddr, this.index)
	pingTicker := time.NewTicker(time.Second * 1)
	defer pingTicker.Stop()
	for {
		select {
		case <-this.chanClose:
			return nil
		case <-pingTicker.C:
			err = this.write(nilBuf)
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
		_, err = io.ReadFull(crand.Reader, this.nonce)
		if err != nil {
			return err
		}
		data2 := this.aesgcm.Seal(nil, this.nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(this.buf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(data2)
		if err != nil {
			return err
		}
		_, err = this.buf.Write(this.nonce)
		if err != nil {
			return err
		}
	}
	if err == nil {
		_, err = this.buf.WriteTo(this.conn)
	}
	return err
}

func (this *ClientConn) Write(data []byte) {
	this.chanWrite <- data
}

func (this *ClientConn) WriteNow(data []byte) error {
	return this.write(data)
}

func (sc *ClientConn) runRead() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("ClientConn::runRead::painc::conn run err: %s", err)
		}
		if sc.conn != nil {
			sc.conn.Close()
		}
	}()
	var err error
	err = sc.crypto()
	utils.POE(err)

	sc.conn.SetReadBuffer(1024 * 1024)
	sc.conn.SetWriteBuffer(1024 * 1024)
	sc.conn.SetNoDelay(true)

	sc.reader = bufio.NewReader(sc.conn)
	for {
		data, err := sc.read()
		if err != nil {
			log.Printf("ClientConn::runRead:conn read err: %s", err)
			return
		}

		if sc.handler != nil {
			sc.handler.OnData(data, sc.conn)
		} else {
			log.Printf("ClientConn::runRead:handler is null")
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

	_, err = io.ReadFull(reader, sc.nonce)
	if err != nil {
		return nil, err
	}
	plain, err := sc.aesgcm.Open(nil, sc.nonce, sc.readBuf[:dataLen], nil)
	if err != nil {
		return nil, err
	}

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
