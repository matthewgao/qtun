package iface

import (
	"net"
	"sync"
)

type PacketIP []byte

// Optimized: Object pool for PacketIP to reduce allocations
var packetIPPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 2048) // Default size, will be resized as needed
		return &buf
	},
}

func NewPacketIP(size int) PacketIP {
	// Try to get from pool first
	bufPtr := packetIPPool.Get().(*[]byte)
	buf := *bufPtr
	
	// Ensure capacity
	if cap(buf) < size {
		buf = make([]byte, size)
	} else {
		buf = buf[:size]
	}
	
	return PacketIP(buf)
}

// PutPacketIP returns a PacketIP to the pool
func PutPacketIP(p PacketIP) {
	if cap(p) >= 1500 && cap(p) <= 65536 {
		buf := []byte(p)
		packetIPPool.Put(&buf)
	}
}

func (p PacketIP) GetSourceIP() net.IP {
	return net.IP(p[12:16])
}

func (p PacketIP) GetDestinationIP() net.IP {
	return net.IP(p[16:20])
}