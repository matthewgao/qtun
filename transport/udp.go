package transport

import (
	"crypto/cipher"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/rs/zerolog/log"
)

// 裸 UDP 传输（类 WireGuard）：数据面 + 控制面都走「UDP + AES-GCM」，不再经过 QUIC。
//
// 为什么要它：QUIC 即便用 datagram，其拥塞控制(CC) + pacer + 32 深发送队列会在「线路并不
// 丢包」的情况下把单条流限速（实测单流 ~20Mbps，pprof 显示 CPU 仅 ~35%、且 RTT 不抬头，
// 说明不是 CPU/丢包，而是 QUIC 这层在无丢包地节流）。隧道本应是「哑管道」，把拥塞控制交还
// 给内层 TCP。裸 UDP 去掉 QUIC 整层，单条流即可吃到真实路径带宽。
//
// 封包格式直接复用 datagram.go 的 frameDatagram/decodeDatagram：
//   [secure(1)][密文][nonce(12)]（加密）/ [secure(1)][明文]（无 --key）
// 负载是 marshal 后的 protocol.Envelope（oneof ping/packet），与 QUIC 路径完全一致，因此
// ping/路由/数据包的语义沿用 app.go 不变。鉴权同样由 AES-GCM 解密成败充当（key 不对则解不开
// 直接丢弃），与 QUIC 路径一致。

const (
	// udpSocketBuffer 设大内核收发缓冲，降低突发下的内核丢包（裸 UDP 没有 QUIC 的应用层
	// 队列兜底，缓冲不足时内核直接丢，对内层 TCP 是真丢包）。
	udpSocketBuffer = 4 * 1024 * 1024
	// udpReadBufSize 单次 ReadFrom 的缓冲；UDP 自带消息边界，一个报文一次读完。
	udpReadBufSize = 65536
	// udpRouteStaleTimeout：某 VIP 客户端超过此时长没有刷新 ping（client 每秒 ping 一次），
	// 即视为陈旧，下行包不再发往该地址。与 QUIC 路径的 routeStaleTimeout 口径一致（3 个 ping
	// 周期容错）。
	udpRouteStaleTimeout = 3 * time.Second
)

// udpRouteFresh 判断某连接最近一次 ping 是否仍在新鲜窗口内。
func udpRouteFresh(now, lastPing int64) bool {
	return now-lastPing <= int64(udpRouteStaleTimeout)
}

// PacketSink 是 UDP 传输向上层（App）回吐「已解包的内层 IP 报文」的出口。App 实现它，
// 把包投递给单个 tunWriter（保序写 TUN）。与 QUIC 路径的 enqueueTunWrite 等价。
type PacketSink interface {
	WriteToTun(pkt iface.PacketIP)
}

// marshalEnvelope 复用对象池把一条 Envelope 序列化为字节（数据面 packet）。
func marshalPacketEnvelope(pkt iface.PacketIP) []byte {
	env := getEnvelope()
	pktMsg := getPacketMessage()
	pktMsg.Payload = pkt
	env.Type = &protocol.Envelope_Packet{Packet: pktMsg}
	data, _ := proto.Marshal(env)
	putEnvelope(env)
	putPacketMessage(pktMsg)
	return data
}

// ============================ UDP 客户端 ============================

type UDPClient struct {
	remoteAddr string
	key        string
	sink       PacketSink
	aesgcm     cipher.AEAD

	mu             sync.RWMutex
	conn           udpPacketConn // 已 connect 的 socket（只与服务端通信）
	dialUDP        udpDialer
	reconnectCh    chan struct{}
	reconnectDelay time.Duration
	pingNow        chan struct{} // 重连成功后请求立即补发一次 ping
	closed         chan struct{}
}

type udpPacketConn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type udpDialer func() (udpPacketConn, error)

func NewUDPClient(remoteAddr, key string, sink PacketSink) *UDPClient {
	c := &UDPClient{
		remoteAddr: remoteAddr,
		key:        key,
		sink:       sink,
	}
	c.ensureRuntimeDefaults()
	return c
}

func (c *UDPClient) ensureRuntimeDefaults() {
	if c.closed == nil {
		c.closed = make(chan struct{})
	}
	if c.reconnectCh == nil {
		c.reconnectCh = make(chan struct{}, 1)
	}
	if c.pingNow == nil {
		c.pingNow = make(chan struct{}, 1)
	}
	if c.reconnectDelay == 0 {
		c.reconnectDelay = time.Second
	}
	if c.dialUDP == nil {
		c.dialUDP = c.defaultDial
	}
}

func (c *UDPClient) Start() error {
	c.ensureRuntimeDefaults()
	if c.key != "" {
		aesgcm, err := makeAES128GCM(c.key)
		if err != nil {
			return err
		}
		c.aesgcm = aesgcm
	}

	if err := c.dial(); err != nil {
		return err
	}

	go c.readLoop()
	go c.pingLoop()
	go c.reconnectLoop()
	log.Info().Str("server_addr", c.remoteAddr).Msg("UDPClient started")
	return nil
}

func (c *UDPClient) defaultDial() (udpPacketConn, error) {
	raddr, err := net.ResolveUDPAddr("udp", c.remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("UDPClient resolve %s: %w", c.remoteAddr, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("UDPClient dial %s: %w", c.remoteAddr, err)
	}
	_ = conn.SetReadBuffer(udpSocketBuffer)
	_ = conn.SetWriteBuffer(udpSocketBuffer)
	return conn, nil
}

func (c *UDPClient) dial() error {
	c.ensureRuntimeDefaults()
	conn, err := c.dialUDP()
	if err != nil {
		return err
	}

	c.mu.Lock()
	old := c.conn
	c.conn = conn
	c.mu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
	return nil
}

func (c *UDPClient) getConn() udpPacketConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *UDPClient) isCurrentConn(conn udpPacketConn) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn == conn
}

// SendPacket 把内层 IP 报文封包后经 UDP 发往服务端。可被多个 TUN worker 并发调用
// （*net.UDPConn 的 Write 是并发安全的）。
func (c *UDPClient) SendPacket(pkt iface.PacketIP) {
	conn := c.getConn()
	if conn == nil {
		return
	}
	data := marshalPacketEnvelope(pkt)
	framed := frameDatagram(c.aesgcm, data)
	if framed == nil {
		return
	}
	if _, err := conn.Write(framed); err != nil {
		warnDatagramDrop(err, len(framed))
		c.requestReconnect(err)
	}
}

// pingLoop 每秒发一次 ping：让服务端学习/刷新本客户端的「VIP → UDP 源地址」映射
// （NAT 后地址只能由服务端从收到的报文里观测到），同时充当 NAT keepalive 与存活信号。
func (c *UDPClient) pingLoop() {
	ip, _, err := net.ParseCIDR(config.GetInstance().Ip)
	if err != nil {
		log.Error().Err(err).Msg("UDPClient parse ip fail, ping disabled")
		return
	}
	vip := ip.String()
	c.sendPing(vip)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-c.pingNow:
			c.sendPing(vip)
		case <-ticker.C:
			c.sendPing(vip)
		}
	}
}

func (c *UDPClient) sendPing(vip string) {
	conn := c.getConn()
	if conn == nil {
		return
	}
	env := getEnvelope()
	ping := getPingMessage()
	ping.Timestamp = time.Now().UnixNano()
	ping.IP = vip
	ping.DC = "client"
	ping.LocalAddr = vip // 仅作标识；服务端实际按 UDP 源地址回包
	env.Type = &protocol.Envelope_Ping{Ping: ping}
	data, _ := proto.Marshal(env)
	putEnvelope(env)
	putPingMessage(ping)

	framed := frameDatagram(c.aesgcm, data)
	if framed == nil {
		return
	}
	if _, err := conn.Write(framed); err != nil {
		log.Debug().Err(err).Msg("UDPClient send ping fail")
		c.requestReconnect(err)
	}
}

func (c *UDPClient) readLoop() {
	buf := make([]byte, udpReadBufSize)
	for {
		conn := c.getConn()
		if conn == nil {
			if !c.sleepOrClosed(200 * time.Millisecond) {
				return
			}
			continue
		}
		n, err := conn.Read(buf)
		if err != nil {
			select {
			case <-c.closed:
				return
			default:
			}
			if !c.isCurrentConn(conn) {
				continue
			}
			// 连接型 UDP：服务端未启动/重启时，内核把 ICMP port-unreachable 以
			// ECONNREFUSED 的形式在下一次 Read 上返回。绝不能因此退出（否则入向永久失效，
			// 表现为"换了 UDP 反而零吞吐"）；歇一下继续，服务端起来后自动恢复。
			c.requestReconnect(err)
			log.Debug().Err(err).Msg("UDPClient read err, continue")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		msg, err := decodeDatagram(c.aesgcm, buf[:n])
		if err != nil {
			log.Debug().Err(err).Msg("UDPClient decode fail, drop")
			continue
		}
		dispatchToSink(msg, c.sink)
	}
}

func (c *UDPClient) requestReconnect(err error) {
	if !shouldReconnectUDP(err) {
		return
	}
	c.ensureRuntimeDefaults()
	select {
	case c.reconnectCh <- struct{}{}:
		log.Warn().Err(err).Str("server_addr", c.remoteAddr).
			Msg("UDPClient UDP socket invalid, scheduling reconnect")
	default:
	}
}

func (c *UDPClient) reconnectLoop() {
	c.ensureRuntimeDefaults()
	for {
		select {
		case <-c.closed:
			return
		case <-c.reconnectCh:
		}

		for {
			if c.isClosed() {
				return
			}
			if err := c.dial(); err != nil {
				log.Warn().Err(err).Str("server_addr", c.remoteAddr).
					Msg("UDPClient reconnect dial fail")
				if !c.sleepOrClosed(c.reconnectDelay) {
					return
				}
				continue
			}
			log.Info().Str("server_addr", c.remoteAddr).Msg("UDPClient reconnected")
			// 新 socket 源地址已变，立即补发一次 ping 让服务端尽快更新路由，
			// 避免下行包继续发往旧地址直到下一次周期 ping（最多 1s）。
			c.triggerImmediatePing()
			break
		}
	}
}

// triggerImmediatePing 非阻塞地请求 pingLoop 立即补发一次 ping。pingNow 为 buffer=1，
// 即使 pingLoop 未运行也不会阻塞（多余请求被丢弃即可）。
func (c *UDPClient) triggerImmediatePing() {
	select {
	case c.pingNow <- struct{}{}:
	default:
	}
}

func (c *UDPClient) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *UDPClient) sleepOrClosed(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.closed:
		return false
	case <-timer.C:
		return true
	}
}

func shouldReconnectUDP(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, syscall.ENETDOWN) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can't assign requested address") ||
		strings.Contains(msg, "cannot assign requested address") ||
		strings.Contains(msg, "network is down") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host")
}

func (c *UDPClient) Stop() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	if conn := c.getConn(); conn != nil {
		conn.Close()
	}
}

// ============================ UDP 服务端 ============================

// udpRoute 记录某 VIP 客户端最近一次被观测到的 UDP 源地址与 ping 时间戳（UnixNano）。
type udpRoute struct {
	addr     *net.UDPAddr
	lastPing int64 // atomic
}

type UDPServer struct {
	listenAddr string
	key        string
	sink       PacketSink
	aesgcm     cipher.AEAD

	conn   *net.UDPConn
	routes sync.Map // vip(string) -> *udpRoute
	closed chan struct{}
}

func NewUDPServer(listenAddr, key string, sink PacketSink) *UDPServer {
	return &UDPServer{
		listenAddr: listenAddr,
		key:        key,
		sink:       sink,
		closed:     make(chan struct{}),
	}
}

func (s *UDPServer) Start() error {
	if s.key != "" {
		aesgcm, err := makeAES128GCM(s.key)
		if err != nil {
			return err
		}
		s.aesgcm = aesgcm
	}

	laddr, err := net.ResolveUDPAddr("udp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("UDPServer resolve %s: %w", s.listenAddr, err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("UDPServer listen %s: %w", s.listenAddr, err)
	}
	_ = conn.SetReadBuffer(udpSocketBuffer)
	_ = conn.SetWriteBuffer(udpSocketBuffer)
	s.conn = conn

	log.Info().Str("addr", s.listenAddr).Msg("UDPServer listening")
	go s.readLoop()
	go s.pruneRoutes()
	return nil
}

// udpRoutePruneInterval / udpRouteEvictAfter：每分钟扫一次路由表，删除超过 1 分钟没有刷新
// ping 的 VIP 条目（远大于发送选路用的 udpRouteStaleTimeout=3s，避免短暂抖动后能恢复的客户端
// 被误删）。与 QUIC 路径的 1 分钟 CleanRoute 对齐，防止 VIP 长期变动时 sync.Map 无界增长。
const (
	udpRoutePruneInterval = time.Minute
	udpRouteEvictAfter    = time.Minute
)

func (s *UDPServer) pruneRoutes() {
	ticker := time.NewTicker(udpRoutePruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			now := time.Now().UnixNano()
			s.routes.Range(func(k, v interface{}) bool {
				r := v.(*udpRoute)
				if now-atomic.LoadInt64(&r.lastPing) > int64(udpRouteEvictAfter) {
					s.routes.Delete(k)
					log.Info().Str("vip", k.(string)).Msg("UDPServer prune stale route")
				}
				return true
			})
		}
	}
}

func (s *UDPServer) readLoop() {
	buf := make([]byte, udpReadBufSize)
	for {
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			// 不因偶发读错误永久退出（否则整个服务端入向失效）；歇一下继续。
			log.Error().Err(err).Msg("UDPServer read err, continue")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		msg, err := decodeDatagram(s.aesgcm, buf[:n])
		if err != nil {
			// key 不对/报文损坏：解不开直接丢（与 QUIC 路径的 ErrCiperNotMatch 等价）
			log.Debug().Err(err).Msg("UDPServer decode fail, drop")
			continue
		}

		ep := protocol.Envelope{}
		if err := proto.Unmarshal(msg, &ep); err != nil {
			log.Debug().Err(err).Msg("UDPServer unmarshal fail, drop")
			continue
		}
		switch ep.Type.(type) {
		case *protocol.Envelope_Ping:
			// 用 UDP 源地址（而非 ping 里的 LocalAddr）作为回包目标：NAT 后只有源地址可达。
			s.updateRoute(ep.GetPing().GetIP(), src)
		case *protocol.Envelope_Packet:
			pkt := iface.PacketIP(ep.GetPacket().GetPayload())
			if s.sink != nil {
				s.sink.WriteToTun(pkt)
			}
		}
	}
}

func (s *UDPServer) updateRoute(vip string, src *net.UDPAddr) {
	if vip == "" || src == nil {
		return
	}
	now := time.Now().UnixNano()
	// 常见路径：地址不变，仅原子刷新时间戳。udpRoute.addr 视为不可变（地址变化时整体替换
	// 条目），避免与 SendPacket 并发读 r.addr 形成数据竞争。
	if v, ok := s.routes.Load(vip); ok {
		r := v.(*udpRoute)
		if r.addr.String() == src.String() {
			atomic.StoreInt64(&r.lastPing, now)
			return
		}
	}
	// 新客户端或源地址因 NAT 重绑定而变化：存入一条全新的不可变条目。
	s.routes.Store(vip, &udpRoute{addr: src, lastPing: now})
}

// SendPacket 内层 IP 报文出向（TUN → 客户端）：按目的 VIP 查路由，仅在「新鲜」连接上发送
// （routeStaleTimeout 内有 ping），避免把下行包发往已离线/陈旧的地址。可被多 worker 并发调用。
func (s *UDPServer) SendPacket(pkt iface.PacketIP) {
	dst := pkt.GetDestinationIP().String()
	v, ok := s.routes.Load(dst)
	if !ok {
		log.Debug().Str("dst", dst).Msg("UDPServer no route, drop")
		return
	}
	r := v.(*udpRoute)
	if !udpRouteFresh(time.Now().UnixNano(), atomic.LoadInt64(&r.lastPing)) {
		log.Debug().Str("dst", dst).Msg("UDPServer route stale, drop")
		return
	}
	data := marshalPacketEnvelope(pkt)
	framed := frameDatagram(s.aesgcm, data)
	if framed == nil {
		return
	}
	if _, err := s.conn.WriteToUDP(framed, r.addr); err != nil {
		warnDatagramDrop(err, len(framed))
	}
}

func (s *UDPServer) Stop() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	if s.conn != nil {
		s.conn.Close()
	}
}

// dispatchToSink 解包 Envelope，数据面包交给 sink 写 TUN；ping 在客户端侧无需处理。
func dispatchToSink(msg []byte, sink PacketSink) {
	ep := protocol.Envelope{}
	if err := proto.Unmarshal(msg, &ep); err != nil {
		log.Debug().Err(err).Msg("UDP dispatch unmarshal fail, drop")
		return
	}
	if pktMsg := ep.GetPacket(); pktMsg != nil {
		if sink != nil {
			sink.WriteToTun(iface.PacketIP(pktMsg.GetPayload()))
		}
	}
}
