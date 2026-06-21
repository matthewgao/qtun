package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	// "log"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog/log"
)

var ErrCiperNotMatch = fmt.Errorf("fail to match key")

type ServerConn struct {
	conn      *quic.Stream
	sess      *quic.Conn
	key       string
	buf       []byte
	aesgcm    cipher.AEAD
	handler   GrpcHandler
	reader    *bufio.Reader
	writeBuf  *bytes.Buffer
	chanWrite chan []byte
	chanClose chan bool
	isClosed  bool
	noDelay   bool
}

func NewServerConn(conn *quic.Stream, sess *quic.Conn, key string, handler GrpcHandler, noDelay bool) *ServerConn {
	return &ServerConn{
		conn:      conn,
		sess:      sess,
		key:       key,
		handler:   handler,
		buf:       make([]byte, 65536),
		writeBuf:  &bytes.Buffer{},
		chanWrite: make(chan []byte, 256), // Optimized: Increased from 2 to 256
		chanClose: make(chan bool, 1),
		noDelay:   noDelay,
	}
}

func (this *ServerConn) Stop() {
	this.chanClose <- true
	close(this.chanWrite)
}

func (sc *ServerConn) readProcess(cleanup func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).
				Msg("ServerConn::run conn run fail, exit")
		}

		log.Warn().Msg("ServerConn::conn run, exit")
		cleanup()

		sc.conn.Close()
		// sc.sess.Close()
		sc.sess.CloseWithError(0x1, "fail to write")
		sc.isClosed = true
		sc.Stop()
		log.Warn().Msg("ServerConn::conn run, exit1")
	}()

	// 注意：crypto() 已在 server.go listen 中于启动各 goroutine 前同步调用，这里不再重复
	// 调用，避免与 readDatagrams 并发读写 sc.aesgcm 造成数据竞争。

	// sc.conn.SetReadBuffer(1024 * 1024)
	// sc.conn.SetWriteBuffer(1024 * 1024)
	// sc.conn.
	// sc.conn.SetNoDelay(sc.noDelay) // close it, and see if the bandwidth can be increased
	// sc.conn.SetKeepAlive(true)
	// sc.conn.SetKeepAlivePeriod(time.Second * 10)
	// sc.conn.SetDeadline(time.Second * 30)
	//
	//FIXME:seems like the bufio buf is not release even if readProcess has fully exit
	// Optimized: Increased buffer from 4KB to 64KB for better throughput
	reader := bufio.NewReaderSize(sc.conn, 1024*64)
	for {
		// sc.conn.SetReadDeadline(time.Now().Add(time.Second * 10))
		data, err := sc.read(reader)
		//FIXME: if it's EOF then need to exit, if it's not should continue
		// if err == io.EOF || err == io.ErrUnexpectedEOF {
		// 	log.Error().Err(err).Msg("ServerConn::run conn read fail, it's closed by client")
		// }

		if err == ErrCiperNotMatch {
			// log.Error().Err(err).Str("from", sc.conn.RemoteAddr().String()).Msg("fail to match key, break")
			log.Error().Err(err).Msg("fail to match key, break")
			break
		}

		if err != nil {
			log.Error().Err(err).Msg("ServerConn::run conn read fail, break")
			break
		}

		if sc.handler != nil {
			sc.handler.ServerOnData(data, sc)
		} else {
			log.Warn().Msg("ServerConn::run sever_conn is nil")
		}
	}
}

func (sc *ServerConn) crypto() error {
	if sc.key == "" {
		// log.Warn().Str("client_addr", sc.conn.RemoteAddr().String()).
		// 	Msg("incoming encryption disabled")
		log.Warn().Msg("incoming encryption disabled")
		return nil
	}
	var err error
	sc.aesgcm, err = makeAES128GCM(sc.key)
	return err
}

func (sc *ServerConn) read(reader *bufio.Reader) ([]byte, error) {
	var err error
	var secure uint8 = 0

	// reader := sc.reader
	err = binary.Read(reader, binary.LittleEndian, &secure)
	if err != nil {
		return nil, err
	}
	var dataLen uint16
	err = binary.Read(reader, binary.LittleEndian, &dataLen)
	if err != nil {
		return nil, err
	}
	_, err = io.ReadFull(reader, sc.buf[:dataLen])
	if err != nil {
		return nil, err
	}
	if secure == 0 {
		return sc.buf[:dataLen], err
	} else {
		// Optimized: Use nonce from pool
		nonce := getNonce()
		defer putNonce(nonce)

		_, err = io.ReadFull(reader, nonce)
		if err != nil {
			return nil, err
		}
		plain, err := sc.aesgcm.Open(nil, nonce, sc.buf[:dataLen], nil)
		if err != nil {
			// return nil, err
			return nil, ErrCiperNotMatch
		}
		return plain, nil
	}
}

func (cc *ServerConn) write(data []byte) error {
	if cc.conn == nil {
		return fmt.Errorf("no connection")
	}
	var err error
	cc.writeBuf.Reset()
	var secure uint8 = 0
	if cc.aesgcm != nil {
		secure = 1
	}
	err = binary.Write(cc.writeBuf, binary.LittleEndian, &secure)
	if err != nil {
		return err
	}
	if secure == 0 {
		dataLen := uint16(len(data))
		err = binary.Write(cc.writeBuf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(data)
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
		data2 := cc.aesgcm.Seal(nil, nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(cc.writeBuf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(data2)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(nonce)
		if err != nil {
			return err
		}
	}
	if err == nil {
		_, err = cc.writeBuf.WriteTo(cc.conn)
	}

	return err
}

func (cc *ServerConn) Write(data []byte) {
	// log.Printf("server conn write chan %d", len(cc.chanWrite))
	defer func() {
		if recover() != nil {
			// the return result can be altered
			// in a defer function call
			log.Warn().Msg("ServerConn::write to close channel")
		}
	}()

	cc.chanWrite <- data
}

func (sc *ServerConn) SendPacket(pkt iface.PacketIP) {
	// Optimized: Reuse protobuf messages from pool
	env := getEnvelope()
	pktMsg := getPacketMessage()

	pktMsg.Payload = pkt
	env.Type = &protocol.Envelope_Packet{Packet: pktMsg}

	data, _ := proto.Marshal(env)

	// Return to pool after marshaling（data 已是独立副本，pkt 也已被拷贝，可安全复用）
	putEnvelope(env)
	putPacketMessage(pktMsg)

	// 数据面走 datagram（不可靠），规避可靠 stream 的队头阻塞/双重重传。frameDatagram
	// 每次新分配缓冲、用 pool nonce + crypto/rand，可被多个 TUN worker 并发调用，无需单
	// writer。aesgcm 在 listen 中启动 goroutine 前已就绪。
	framed := frameDatagram(sc.aesgcm, data)
	if framed == nil {
		return
	}
	if err := sc.sess.SendDatagram(framed); err != nil {
		warnDatagramDrop(err, len(framed))
	}
}

// readDatagrams 接收数据面 datagram（IP 报文），解密后交给 handler。控制面（ping、
// 关闭检测）仍走 stream 的 readProcess。连接关闭时 ReceiveDatagram 返回 error，退出。
func (cc *ServerConn) readDatagrams() {
	sess := cc.sess
	if sess == nil {
		return
	}
	for {
		d, err := sess.ReceiveDatagram(context.Background())
		if err != nil {
			log.Debug().Err(err).Msg("ServerConn::readDatagrams exit")
			return
		}
		msg, err := decodeDatagram(cc.aesgcm, d)
		if err != nil {
			log.Debug().Err(err).Msg("ServerConn::readDatagrams decode fail, drop")
			continue
		}
		if cc.handler != nil {
			cc.handler.ServerOnData(msg, cc)
		}
	}
}

func (cc *ServerConn) writeProcess() (err error) {
	defer func() {
		if perr := recover(); perr != nil {
			err = fmt.Errorf("server process write panic: %s", perr)
		}

		cc.conn.Close()
		// cc.sess.Close()
		cc.sess.CloseWithError(0x1, "fail to read")
		cc.isClosed = true
		// log.Warn().Str("client_addr", cc.conn.RemoteAddr().String()).
		// 	Msg("ServerConn::ProcessWrite conn closedd")
		log.Warn().Msg("ServerConn::ProcessWrite conn closedd")
	}()

	// log.Info().Str("client_addr", cc.conn.RemoteAddr().String()).Msg("ServerConn::ProcessWrite Start")
	log.Info().Msg("ServerConn::ProcessWrite Start")

	for {
		select {
		case buf := <-cc.chanWrite:
			err = cc.write(buf)
		case stop := <-cc.chanClose:
			if stop {
				log.Info().Err(err).
					// Str("client_addr", cc.conn.).
					Msg("ServerConn::ProcessWrite stop")
				return err
			}
		}

		if err != nil {
			// log.Warn().Err(err).Str("client_addr", cc.conn.RemoteAddr().String()).
			// 	Msg("ServerConn::ProcessWrite End with error")
			log.Warn().Err(err).Msg("ServerConn::ProcessWrite End with error")
			return err
		}
	}
	// return err
}

func (this *ServerConn) IsClosed() bool {
	return this.isClosed
}
