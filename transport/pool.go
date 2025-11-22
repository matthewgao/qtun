package transport

import (
	"bytes"
	"sync"

	"github.com/matthewgao/qtun/protocol"
)

// Optimized: Expanded object pools for better memory reuse
var noncePool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 12)
	},
}

var bufPool = sync.Pool{
	New: func() interface{} {
		return &bytes.Buffer{}
	},
}

// Pool for protobuf envelopes
var envelopePool = sync.Pool{
	New: func() interface{} {
		return &protocol.Envelope{}
	},
}

// Pool for protobuf ping messages
var pingMessagePool = sync.Pool{
	New: func() interface{} {
		return &protocol.MessagePing{}
	},
}

// Pool for protobuf packet messages
var packetMessagePool = sync.Pool{
	New: func() interface{} {
		return &protocol.MessagePacket{}
	},
}

// Pool for read buffers (64KB)
var readBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 65536)
		return &buf
	},
}

// Helper functions for pool management
func getNonce() []byte {
	return noncePool.Get().([]byte)
}

func putNonce(nonce []byte) {
	if len(nonce) == 12 {
		noncePool.Put(nonce)
	}
}

func getBuffer() *bytes.Buffer {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putBuffer(buf *bytes.Buffer) {
	buf.Reset()
	bufPool.Put(buf)
}

func getEnvelope() *protocol.Envelope {
	return envelopePool.Get().(*protocol.Envelope)
}

func putEnvelope(env *protocol.Envelope) {
	env.Reset()
	envelopePool.Put(env)
}

func getPingMessage() *protocol.MessagePing {
	return pingMessagePool.Get().(*protocol.MessagePing)
}

func putPingMessage(msg *protocol.MessagePing) {
	msg.Reset()
	pingMessagePool.Put(msg)
}

func getPacketMessage() *protocol.MessagePacket {
	return packetMessagePool.Get().(*protocol.MessagePacket)
}

func putPacketMessage(msg *protocol.MessagePacket) {
	msg.Reset()
	packetMessagePool.Put(msg)
}

func getReadBuf() *[]byte {
	return readBufPool.Get().(*[]byte)
}

func putReadBuf(buf *[]byte) {
	readBufPool.Put(buf)
}
