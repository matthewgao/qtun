package transport

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	// "log"
	"net"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/utils"
	"github.com/rs/zerolog/log"
)

type ServerConn struct {
	conn      *net.TCPConn
	key       string
	nonce     []byte
	buf       []byte
	aesgcm    cipher.AEAD
	handler   GrpcHandler
	reader    *bufio.Reader
	writeBuf  *bytes.Buffer
	chanWrite chan []byte
	// chanClose chan bool
}

func NewServerConn(conn *net.TCPConn, key string, handler GrpcHandler) *ServerConn {
	return &ServerConn{
		conn:      conn,
		key:       key,
		handler:   handler,
		nonce:     make([]byte, 12),
		buf:       make([]byte, 65536),
		writeBuf:  &bytes.Buffer{},
		chanWrite: make(chan []byte, 65536),
		// chanClose: make(chan bool),
	}
}

func (sc *ServerConn) run(cleanup func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).
				Msg("ServerConn::run conn run fail")
		}
		cleanup()
		sc.conn.Close()
	}()
	var err error
	err = sc.crypto()
	utils.POE(err)

	sc.conn.SetReadBuffer(1024 * 1024)
	sc.conn.SetWriteBuffer(1024 * 1024)
	sc.conn.SetNoDelay(true) // close it, and see if the bandwidth can be increased

	sc.reader = bufio.NewReader(sc.conn)
	for {
		data, err := sc.read()
		if err != nil {
			log.Error().Err(err).Msg("ServerConn::run conn read fail")
			return
		}

		if sc.handler != nil {
			sc.handler.OnData(data, sc.conn)
		} else {
			log.Warn().Msg("ServerConn::run sever_conn is nil")
		}
	}
}

func (sc *ServerConn) crypto() error {
	if sc.key == "" {
		log.Warn().Str("client_addr", sc.conn.RemoteAddr().String()).
			Msg("incoming encryption disabled")
		return nil
	}
	var err error
	sc.aesgcm, err = makeAES128GCM(sc.key)
	return err
}

func (sc *ServerConn) read() ([]byte, error) {
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
	_, err = io.ReadFull(reader, sc.buf[:dataLen])
	if err != nil {
		return nil, err
	}
	if secure == 0 {
		return sc.buf[:dataLen], err
	} else {
		_, err = io.ReadFull(reader, sc.nonce)
		if err != nil {
			return nil, err
		}
		plain, err := sc.aesgcm.Open(nil, sc.nonce, sc.buf[:dataLen], nil)
		if err != nil {
			return nil, err
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
		_, err = io.ReadFull(crand.Reader, cc.nonce)
		if err != nil {
			return err
		}
		data2 := cc.aesgcm.Seal(nil, cc.nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(cc.writeBuf, binary.LittleEndian, &dataLen)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(data2)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(cc.nonce)
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
	cc.chanWrite <- data
}

func (cc *ServerConn) WriteNow(data []byte) error {
	return cc.write(data)
}

func (sc *ServerConn) SendPacket(pkt iface.PacketIP) {
	data, _ := proto.Marshal(&protocol.Envelope{
		Type: &protocol.Envelope_Packet{
			Packet: &protocol.MessagePacket{Payload: pkt},
		},
	})

	sc.Write(data)
}

func (cc *ServerConn) ProcessWrite() (err error) {
	defer func() {
		if perr := recover(); perr != nil {
			err = fmt.Errorf("server process write panic: %s", perr)
		}

		cc.conn.Close()
		log.Warn().Str("client_addr", cc.conn.RemoteAddr().String()).
			Msg("ServerConn::ProcessWrite conn closedd")
	}()

	log.Info().Str("client_addr", cc.conn.RemoteAddr().String()).Msg("ServerConn::ProcessWrite Start")

	for {
		select {
		case buf := <-cc.chanWrite:
			err = cc.write(buf)
		}

		if err != nil {
			log.Warn().Err(err).Str("client_addr", cc.conn.RemoteAddr().String()).
				Msg("ServerConn::ProcessWrite End with error")
			return err
		}
	}
	return err
}
