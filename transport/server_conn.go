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

var ErrCiperNotMatch = fmt.Errorf("fail to match key")

type ServerConn struct {
	conn *net.TCPConn
	key  string
	// nonce  []byte
	buf    []byte
	aesgcm cipher.AEAD
	// aesgcmwrt cipher.AEAD
	handler   GrpcHandler
	reader    *bufio.Reader
	writeBuf  *bytes.Buffer
	chanWrite chan []byte
	chanClose chan bool
	isClosed  bool
	noDelay   bool
}

func NewServerConn(conn *net.TCPConn, key string, handler GrpcHandler, noDelay bool) *ServerConn {
	return &ServerConn{
		conn:    conn,
		key:     key,
		handler: handler,
		// nonce:     make([]byte, 12),
		buf:       make([]byte, 65536),
		writeBuf:  bytes.NewBuffer(make([]byte, 4096)),
		chanWrite: make(chan []byte, 2),
		chanClose: make(chan bool, 1),
		noDelay:   noDelay,
	}
}

func (sc *ServerConn) readProcess(cleanup func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Err(err.(error)).
				Msg("ServerConn::run conn run fail, exit")
		}
		cleanup()
		sc.conn.Close()
		sc.isClosed = true
		sc.Stop()
	}()
	var err error
	err = sc.crypto()
	utils.POE(err)

	sc.conn.SetReadBuffer(BUF_SIZE)
	sc.conn.SetWriteBuffer(BUF_SIZE)
	sc.conn.SetNoDelay(sc.noDelay) // close it, and see if the bandwidth can be increased

	sc.reader = bufio.NewReaderSize(sc.conn, BUF_SIZE)
	for {
		data, err := sc.read()
		//FIXME: if it's EOF then need to exit, if it's not should continue
		// if err == io.EOF || err == io.ErrUnexpectedEOF {
		// 	log.Error().Err(err).Msg("ServerConn::run conn read fail, it's closed by client")
		// }

		if err == ErrCiperNotMatch {
			log.Error().Err(err).Str("from", sc.conn.RemoteAddr().String()).Msg("fail to match key, break")
			break
		}

		if err != nil {
			log.Error().Err(err).Msg("ServerConn::run conn read fail, break")
			break
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
	// sc.aesgcmwrt, err = makeAES128GCM(sc.key)
	return err
}

func (sc *ServerConn) read() ([]byte, error) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		log.Error().Err(err.(error)).
	// 			Msg("ServerConn::read conn run fail")
	// 	}
	// }()

	var err error
	var secure uint8 = 0

	err = binary.Read(sc.reader, binary.LittleEndian, &secure)
	if err != nil {
		log.Warn().Err(err).Uint8("secure", secure).Msg("ServerConn::read secure fail")
		return nil, err
	}

	// var magicNum uint16
	// err = binary.Read(sc.reader, binary.LittleEndian, &magicNum)
	// if err != nil {
	// 	log.Warn().Err(err).Uint16("magicNum", magicNum).Msg("ServerConn::read magicNum fail")
	// 	return nil, err
	// }

	var dataLen uint16 = 0
	err = binary.Read(sc.reader, binary.LittleEndian, &dataLen)
	if err != nil {
		log.Warn().Err(err).Uint16("dataLen", dataLen).Msg("ServerConn::read dataLen fail")
		return nil, err
	}

	// log.Debug().Uint16("dataLen", dataLen).Uint16("magic_num", magicNum).Msg("ServerConn::read datelen")

	// sc.buf = make([]byte, dataLen)
	n, err := io.ReadFull(sc.reader, sc.buf[:dataLen])
	// _, err = io.ReadFull(sc.reader, sc.buf)
	if err != nil {
		log.Warn().Err(err).Int("data", n).Msg("ServerConn::read data fail")
		return nil, err
	}
	if secure == 0 {
		return sc.buf[:dataLen], err
	} else {
		nonce := make([]byte, sc.aesgcm.NonceSize())
		_, err = io.ReadFull(sc.reader, nonce)
		if err != nil {
			log.Warn().Err(err).Bytes("nonce", nonce).Msg("ServerConn::read nonce fail")
			return nil, err
		}
		// aesgcm, _ := makeAES128GCM(sc.key)
		plain, err := sc.aesgcm.Open(nil, nonce, sc.buf[:dataLen], nil)
		if err != nil {
			// return nil, err
			log.Error().Str("plain", string(plain)).Str("nonce", string(nonce)).Int("datalen", int(dataLen)).Err(err).
				Int("bufsize", len(sc.buf)).Bytes("data", sc.buf[:dataLen]).Msg("ServerConn::runRead aesgcm open fail")
			return nil, ErrCiperNotMatch
		}
		return plain, nil
	}
}

func (cc *ServerConn) write(data []byte) error {
	if cc.conn == nil {
		return fmt.Errorf("no connection")
	}

	if len(data) > 1600 {
		log.Warn().Int("data_size", len(data)).Msg("write data size gt 1600")
	}

	log.Debug().Int("data_size", len(data)).
		Msg("write data size")

	var err error
	cc.writeBuf.Reset()
	var secure uint8 = 0
	if cc.aesgcm != nil {
		secure = 1
	}

	err = binary.Write(cc.writeBuf, binary.LittleEndian, secure)
	if err != nil {
		return err
	}

	// magicNum := uint16(22222)
	// err = binary.Write(cc.writeBuf, binary.LittleEndian, magicNum)
	// if err != nil {
	// 	return err
	// }

	if secure == 0 {
		dataLen := uint16(len(data))
		err = binary.Write(cc.writeBuf, binary.LittleEndian, dataLen)
		if err != nil {
			return err
		}
		_, err = cc.writeBuf.Write(data)
		if err != nil {
			return err
		}
	} else {
		nonce := make([]byte, cc.aesgcm.NonceSize())
		// nonce := []byte("xxxxxxxxxxxx")
		_, err = io.ReadFull(crand.Reader, nonce)
		if err != nil {
			return err
		}

		data2 := cc.aesgcm.Seal(nil, nonce, data, nil)
		dataLen := uint16(len(data2))
		err = binary.Write(cc.writeBuf, binary.LittleEndian, dataLen)
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

	log.Debug().Int("writeBuf_size", cc.writeBuf.Len()).
		Msg("write data size before send")

	if err == nil {
		// var n int64
		_, err = cc.writeBuf.WriteTo(cc.conn)
	}
	return err
}

func (cc *ServerConn) Write(data []byte) {
	// log.Printf("server conn write chan %d", len(cc.chanWrite))
	cc.chanWrite <- data
}

// func (cc *ServerConn) WriteNow(data []byte) error {
// 	// return cc.write(data)
// 	cc.chanWrite <- data
// 	return nil
// }

func (sc *ServerConn) SendPacket(pkt iface.PacketIP) {
	data, _ := proto.Marshal(&protocol.Envelope{
		Type: &protocol.Envelope_Packet{
			Packet: &protocol.MessagePacket{Payload: pkt},
		},
	})

	sc.Write(data)
}

func (cc *ServerConn) writeProcess() (err error) {
	defer func() {
		if perr := recover(); perr != nil {
			err = fmt.Errorf("server process write panic: %s", perr)
		}

		cc.conn.Close()
		cc.isClosed = true
		log.Warn().Str("client_addr", cc.conn.RemoteAddr().String()).
			Msg("ServerConn::ProcessWrite conn closed")
	}()

	log.Info().Str("client_addr", cc.conn.RemoteAddr().String()).Msg("ServerConn::ProcessWrite Start")

	for {
		select {
		case buf := <-cc.chanWrite:
			err = cc.write(buf)
		case stop := <-cc.chanClose:
			if stop {
				log.Info().Err(err).Str("client_addr", cc.conn.RemoteAddr().String()).
					Msg("ServerConn::ProcessWrite stop")
				return nil
			}
		}

		if err != nil {
			log.Warn().Err(err).Str("client_addr", cc.conn.RemoteAddr().String()).
				Msg("ServerConn::ProcessWrite End with error")
			return err
		}
	}
}

func (this *ServerConn) Stop() {
	this.chanClose <- true
}

func (this *ServerConn) IsClosed() bool {
	return this.isClosed
}
