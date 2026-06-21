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

// FlowHash 计算 IPv4 五元组(源/目的 IP + 源/目的端口)的 FNV-1a 哈希，用于「流亲和」
// 分发：同一条 TCP/UDP 流的所有包得到相同哈希，被固定分发到同一条 QUIC 连接，避免被撒
// 到多条连接造成乱序(datagram 跨连接无序，会让内层 TCP 误判重传)。不同流哈希不同，自然
// 散布到多条连接以并行打满带宽。非 TCP/UDP 仅按 IP 对计算。
func (p PacketIP) FlowHash() uint32 {
	if len(p) < 20 {
		return 0
	}
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 12; i < 20; i++ { // 源 + 目的 IP
		h = (h ^ uint32(p[i])) * prime32
	}
	if proto := p[9]; proto == 6 || proto == 17 { // TCP / UDP：混入端口
		ihl := int(p[0]&0x0f) * 4
		if ihl >= 20 && len(p) >= ihl+4 {
			for i := ihl; i < ihl+4; i++ {
				h = (h ^ uint32(p[i])) * prime32
			}
		}
	}
	return h
}