package qtun

import (
	"math/rand"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/transport"
	"github.com/matthewgao/qtun/utils/timer"
	"github.com/rs/zerolog/log"
)

type App struct {
	config *config.Config
	client *transport.Client
	routes map[string]map[string]struct{}
	mutex  sync.RWMutex // Already RWMutex, good!
	server *transport.Server
	iface  *iface.Iface
	tm     timer.Timer
}

func NewApp() *App {
	return &App{
		config: config.GetInstance(),
		routes: make(map[string]map[string]struct{}),
		tm:     timer.NewTimer(),
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
		for dst, conns := range this.routes {
			for c := range conns {
				conn := this.server.GetConnsByAddr(c)
				if conn == nil || conn.IsClosed() {
					log.Info().Str("conn", c).
						Str("dst", dst).
						Msg("remove dead conns from route")
					delete(this.routes[dst], c)
					this.server.DeleteDeadConn(c)
				}
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

				if len(conns) == 0 {
					this.mutex.RUnlock()
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::has route but no connection, packet dropped")
					break
				}

				// Copy keys inside RLock to minimize lock time
				keys := make([]string, 0, len(conns))
				for k := range conns {
					keys = append(keys, k)
				}
				this.mutex.RUnlock()

				idx := rand.Intn(len(keys))
				conn := this.server.GetConnsByAddr(keys[idx])

				if conn == nil || conn.IsClosed() {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no connection, packet dropped")
					this.mutex.Lock()
					delete(this.routes[dst], keys[idx])
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
		this.mutex.Lock()
		if _, ok := this.routes[ping.GetIP()]; ok {
			this.routes[ping.GetIP()][ping.GetLocalAddr()] = struct{}{}
		} else {
			this.routes[ping.GetIP()] = map[string]struct{}{
				ping.GetLocalAddr(): struct{}{},
			}
		}
		this.mutex.Unlock()

		log.Debug().Str("local", ping.GetLocalAddr()).Str("ip", ping.GetIP()).
			Msg("Proto Ping")

		log.Info().Interface("route", this.routes).Msg("Route Table")

		this.server.SetConns(ping.GetLocalAddr(), conn)
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())

		log.Debug().Int("pkt_len", len(pkt)).IPAddr("src", pkt.GetSourceIP()).
			IPAddr("dst", pkt.GetDestinationIP()).
			Msg("received protobuf packet")

		this.iface.Write(pkt)
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

		this.iface.Write(pkt)
	}
}

func (this *App) SetProxy() {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("networksetup", "-setautoproxyurl", "Wi-Fi", "http://127.0.0.1:6061/proxy.pac")
		log.Info().Str("cmd", cmd.String()).Msg("set system proxy")
	case "linux":
		log.Info().Msg("set system proxy not support please set it manually")
		return
	case "windows":
		log.Info().Msg("set system proxy not support please set it manually")
		return
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("cmd_output", string(output)).
			Msg("set system proxy fail")
	}
}
