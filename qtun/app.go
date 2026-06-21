package qtun

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/transport"
	"github.com/matthewgao/qtun/utils/timer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// routeStaleTimeout：路由表中某连接超过此时长没有刷新 ping，即视为陈旧。
// client 每秒对每条连接发一次 ping（见 client.go），重启后旧连接（旧进程已退出）
// 不再发 ping，其时间戳停止刷新；分发下行包时只在「新鲜」连接里随机选，于是旧连接
// 在此窗口内即被排除，无需等 QUIC idle timeout。给 3 个 ping 周期容错以抗抖动。
const routeStaleTimeout = 3 * time.Second

// isRouteFresh 判断某连接最近一次 ping（lastPing，UnixNano）相对 now 是否仍在新鲜窗口内。
// FetchAndProcessTunPkt 选连接与 CleanRoute 清理共用此判断，保证两处口径一致。
func isRouteFresh(now, lastPing int64) bool {
	return now-lastPing <= int64(routeStaleTimeout)
}

type App struct {
	config *config.Config
	client *transport.Client
	// routes[clientVIP][localAddr] = 该连接最近一次 ping 的 UnixNano 时间戳
	routes map[string]map[string]int64
	mutex  sync.RWMutex // Already RWMutex, good!
	server *transport.Server
	iface  iface.Device
	tm     timer.Timer
	// tunWriteChan 把「收到的 IP 包」从各 QUIC 读 goroutine 解耦到单个写 TUN 的 goroutine。
	// 阻塞式的 iface.Write(syscall) 不再卡在 QUIC 读循环里拖慢流控；用单 writer（而非池）
	// 保证写 TUN 不乱序，避免内层 TCP 把乱序误判成丢包。
	tunWriteChan chan iface.PacketIP
	// tunChanWarnAt 记录上次「channel 接近满」告警的纳秒时间戳，用于节流（每秒最多一条）。
	tunChanWarnAt int64
}

func NewApp() *App {
	return &App{
		config:       config.GetInstance(),
		routes:       make(map[string]map[string]int64),
		tm:           timer.NewTimer(),
		tunWriteChan: make(chan iface.PacketIP, 2048),
	}
}

func (this *App) Run() error {
	if config.GetInstance().ServerMode {
		this.server = transport.NewServer(this.config.Listen, this, this.config.Key)
		go this.server.Start()
		this.CleanRoute()
	} else {
		this.client = transport.NewClient(this.config.RemoteAddrs, this.config.Key, this.config.TransportThreads, this)
		this.client.Start()
		this.SetProxy()
	}

	return this.StartFetchTunInterface()
}

func (this *App) CleanRoute() {
	this.tm.RegisterTask(func() {
		log.Info().Msg("start to clean route")

		// 先持读锁对路由表做一次快照，避免无锁遍历与其他 goroutine 的写并发
		// （否则会触发 fatal error: concurrent map iteration and map write）
		type routeEntry struct {
			dst, conn string
			lastPing  int64
		}
		var entries []routeEntry
		now := time.Now().UnixNano()
		this.mutex.RLock()
		for dst, conns := range this.routes {
			for c, lastPing := range conns {
				entries = append(entries, routeEntry{dst: dst, conn: c, lastPing: lastPing})
			}
		}
		this.mutex.RUnlock()

		// 锁外做耗时的连接探活，发现死连接（或长时间未刷新 ping 的陈旧连接）再持写锁删除
		for _, e := range entries {
			conn := this.server.GetConnsByAddr(e.conn)
			stale := !isRouteFresh(now, e.lastPing)
			if conn == nil || conn.IsClosed() || stale {
				log.Info().Str("conn", e.conn).
					Str("dst", e.dst).
					Bool("stale", stale).
					Msg("remove dead conns from route")
				this.mutex.Lock()
				if m, ok := this.routes[e.dst]; ok {
					delete(m, e.conn)
					if len(m) == 0 {
						delete(this.routes, e.dst)
					}
				}
				this.mutex.Unlock()
				this.server.DeleteDeadConn(e.conn)
			}
		}
	}, time.Minute)
	this.tm.Start()
}

func (this *App) StartFetchTunInterface() error {
	this.iface = iface.New("", this.config.Ip, this.config.Mtu)
	err := this.iface.Start()
	if err != nil {
		return err
	}

	// 单个写 TUN 的 goroutine，消费 tunWriteChan（接收端流水线解耦）
	go this.tunWriter()

	// Optimized: Dynamic worker count based on CPU cores
	// Use 2x CPU cores for better I/O parallelism, with min 4 and max 32
	numWorkers := runtime.NumCPU() * 2
	if numWorkers < 4 {
		numWorkers = 4
	}
	if numWorkers > 32 {
		numWorkers = 32
	}

	log.Info().Int("num_workers", numWorkers).Int("num_cpu", runtime.NumCPU()).
		Msg("Starting TUN packet workers")

	for i := 0; i < numWorkers-1; i++ {
		go this.FetchAndProcessTunPkt(i)
	}

	return this.FetchAndProcessTunPkt(numWorkers - 1)
}

// enqueueTunWrite 把收到的 IP 包交给单个 tunWriter。
// 当 channel 占用 ≥90% 时，说明 TUN writer 跟不上、接收侧成为瓶颈，节流打印一次告警
// （每秒最多一条）。平时仅做一次 len 比较，零额外开销。
func (this *App) enqueueTunWrite(pkt iface.PacketIP) {
	if l, c := len(this.tunWriteChan), cap(this.tunWriteChan); l*10 >= c*9 {
		now := time.Now().UnixNano()
		last := atomic.LoadInt64(&this.tunChanWarnAt)
		if now-last > int64(time.Second) && atomic.CompareAndSwapInt64(&this.tunChanWarnAt, last, now) {
			log.Warn().Int("len", l).Int("cap", c).
				Msg("tunWriteChan 接近满：TUN writer 跟不上，接收侧可能受限（可考虑多队列 TUN）")
		}
	}
	this.tunWriteChan <- pkt
}

// tunWriter 是唯一向 TUN 设备写入的 goroutine，保证写入顺序、避免阻塞 QUIC 读循环。
func (this *App) tunWriter() {
	defer func() {
		if err := recover(); err != nil {
			log.Error().Interface("err", err).Msg("tunWriter panic")
		}
	}()
	for pkt := range this.tunWriteChan {
		if _, err := this.iface.Write(pkt); err != nil {
			log.Error().Err(err).Msg("tunWriter::write to tun fail")
		}
	}
}

func (this *App) FetchAndProcessTunPkt(workerNum int) error {
	mtu := config.GetInstance().Mtu
	pkt := iface.NewPacketIP(mtu)
	for {
		n, err := this.iface.Read(pkt)
		if err != nil {
			log.Error().Err(err).Msg("FetchAndProcessTunPkt read ip pkt error")
			return err
		}
		src := pkt.GetSourceIP().String()
		dst := pkt.GetDestinationIP().String()

		log.Debug().Int("workder", workerNum).Str("src", src).Str("dst", dst).
			Int("len", n).Msg("FetchAndProcessTunPkt::got tun packet")

		if config.GetInstance().ServerMode {
			// 一个包的处理很快，循环外取一次「现在」用于新鲜度判断即可
			now := time.Now().UnixNano()
			// Optimized: Use RLock for read operations and minimize critical section
			for {
				this.mutex.RLock()
				conns, ok := this.routes[dst]
				if !ok {
					this.mutex.RUnlock()
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no route, packet dropped")
					break
				}

				// 只把「近期仍在发 ping」的连接作为候选：client 重启后旧（已死）连接
				// 不再刷新 lastPing，routeStaleTimeout 内即被排除，无需等 QUIC idle
				// timeout，从根上避免把下行包随机分发到陈旧连接造成静默丢包。
				// Copy keys inside RLock to minimize lock time
				keys := make([]string, 0, len(conns))
				for k, lastPing := range conns {
					if isRouteFresh(now, lastPing) {
						keys = append(keys, k)
					}
				}
				this.mutex.RUnlock()

				if len(keys) == 0 {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::has route but no fresh connection, packet dropped")
					break
				}

				// 选连接：默认随机分发（单流友好）；开启 FlowHash 时按五元组哈希固定到
				// 同一条连接（流亲和，多连接聚合吞吐更高、单流受单连接上限）。
				// 注意：keys 来自 map 遍历、顺序每次随机，流亲和前必须 sort 成稳定顺序，
				// 否则同一哈希每包仍指向不同连接，亲和形同虚设。
				var idx int
				if config.GetInstance().FlowHash {
					sort.Strings(keys)
					idx = int(pkt.FlowHash() % uint32(len(keys)))
				} else {
					idx = rand.Intn(len(keys))
				}
				conn := this.server.GetConnsByAddr(keys[idx])

				if conn == nil || conn.IsClosed() {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no connection, packet dropped")
					this.mutex.Lock()
					if m, ok := this.routes[dst]; ok {
						delete(m, keys[idx])
						if len(m) == 0 {
							delete(this.routes, dst)
						}
					}
					this.mutex.Unlock()
					this.server.DeleteDeadConn(keys[idx])
				} else {
					log.Debug().Int("workder", workerNum).Str("src", src).Str("dst", dst).
						Int("len", n).Msg("FetchAndProcessTunPkt::send packet")
					conn.SendPacket(pkt)
					log.Debug().Int("workder", workerNum).Str("src", src).Str("dst", dst).
						Int("len", n).Msg("FetchAndProcessTunPkt::send packet done")
					break
				}
			}
		} else {
			//client send packet
			this.client.SendPacket(pkt)
		}
	}
}

func (this *App) ServerOnData(buf []byte, conn *transport.ServerConn) {
	ep := protocol.Envelope{}
	err := proto.Unmarshal(buf, &ep)
	if err != nil {
		log.Error().Err(err).Msg("OnData::proto unmarshal err")
		return
	}

	switch ep.Type.(type) {
	case *protocol.Envelope_Ping:
		ping := ep.GetPing()
		//根据Client发来的Ping包信息来添加路由
		// Optimized: Minimize critical section
		// 每次收到 ping 都刷新该连接的时间戳，作为「连接仍然活着」的应用层信号；
		// 分发时据此挑掉陈旧连接（见 routeStaleTimeout / FetchAndProcessTunPkt）。
		now := time.Now().UnixNano()
		this.mutex.Lock()
		if _, ok := this.routes[ping.GetIP()]; ok {
			this.routes[ping.GetIP()][ping.GetLocalAddr()] = now
		} else {
			this.routes[ping.GetIP()] = map[string]int64{
				ping.GetLocalAddr(): now,
			}
		}
		// 仅在 Debug 级别真正启用时，才在持锁状态下把路由表拷成字符串快照，
		// 避免锁外把活的共享 map 交给日志库反射遍历而与其他 goroutine 的写并发，
		// 同时在非 Debug 级别下省去无谓的序列化开销
		routeSnapshot := ""
		if zerolog.GlobalLevel() <= zerolog.DebugLevel {
			routeSnapshot = fmt.Sprintf("%v", this.routes)
		}
		this.mutex.Unlock()

		log.Debug().Str("local", ping.GetLocalAddr()).Str("ip", ping.GetIP()).
			Msg("Proto Ping")

		log.Debug().Str("route", routeSnapshot).Msg("Route Table")

		this.server.SetConns(ping.GetLocalAddr(), conn)
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())

		log.Debug().Int("pkt_len", len(pkt)).IPAddr("src", pkt.GetSourceIP()).
			IPAddr("dst", pkt.GetDestinationIP()).
			Msg("received protobuf packet")

		// 交给单个 tunWriter goroutine，避免阻塞式写 TUN 卡住 QUIC 读循环
		this.enqueueTunWrite(pkt)
	}
}

func (this *App) ClientOnData(buf []byte) {
	ep := protocol.Envelope{}
	err := proto.Unmarshal(buf, &ep)
	if err != nil {
		log.Error().Err(err).Msg("OnData::proto unmarshal err")
		return
	}

	switch ep.Type.(type) {
	case *protocol.Envelope_Ping:
		// ping := ep.GetPing()
		// //根据Client发来的Ping包信息来添加路由
		// this.mutex.Lock()

		// if _, ok := this.routes[ping.GetIP()]; ok {
		// 	this.routes[ping.GetIP()][ping.GetLocalAddr()] = struct{}{}
		// } else {
		// 	this.routes[ping.GetIP()] = map[string]struct{}{
		// 		ping.GetLocalAddr(): struct{}{},
		// 	}
		// }

		// log.Debug().Str("local", ping.GetLocalAddr()).Str("ip", ping.GetIP()).
		// 	Msg("Proto Ping")

		// log.Info().Interface("route", this.routes).
		// 	Msg("Route Table")

		// this.server.SetConns(ping.GetLocalAddr(), conn)
		// this.mutex.Unlock()
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())

		log.Debug().Int("pkt_len", len(pkt)).IPAddr("src", pkt.GetSourceIP()).
			IPAddr("dst", pkt.GetDestinationIP()).
			Msg("received protobuf packet")

		// 交给单个 tunWriter goroutine，避免阻塞式写 TUN 卡住 QUIC 读循环
		this.enqueueTunWrite(pkt)
	}
}

// pacURL 是 client 模式下供系统自动代理使用的 PAC 文件地址，由本机的文件服务器
// （fileserver，默认 6061 端口）提供。各平台的实际设置逻辑见 proxy_<os>.go。
const pacURL = "http://127.0.0.1:6061/proxy.pac"

func (this *App) SetProxy() {
	setSystemProxy(pacURL)
	// 只有真正设置过系统代理的路径（client / proxyonly）才注册退出还原；
	// server 模式不调 SetProxy，退出时不会动用户的代理设置。
	this.registerProxyCleanup()
}

// registerProxyCleanup 监听中断/终止信号，退出前 best-effort 还原系统代理，
// 避免进程结束后把用户流量继续指向已失效的本地代理。
func (this *App) registerProxyCleanup() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info().Msg("received signal, restoring system proxy")
		unsetSystemProxy()
		os.Exit(0)
	}()
}
