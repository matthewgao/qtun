package qtun

import (
	"math/rand"
	"net"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/transport"
	"github.com/rs/zerolog/log"
)

type App struct {
	config *config.Config
	client *transport.Client
	routes map[string]map[string]struct{}
	mutex  sync.RWMutex
	server *transport.Server
	iface  *iface.Iface
}

func NewApp() *App {
	return &App{
		config: config.GetInstance(),
		routes: make(map[string]map[string]struct{}),
	}
}

func (this *App) Run() error {
	if config.GetInstance().ServerMode {
		this.server = transport.NewServer(this.config.Listen, this, this.config.Key)
		go this.server.Start()
	} else {
		this.client = transport.NewClient(this.config.RemoteAddrs, this.config.Key, this.config.TransportThreads, this)
		this.client.Start()
	}

	return this.StartFetchTunInterface()
}

func (this *App) StartFetchTunInterface() error {
	this.iface = iface.New("", this.config.Ip, this.config.Mtu)
	err := this.iface.Start()
	if err != nil {
		return err
	}

	for i := 0; i < 10; i++ {
		go this.FetchAndProcessTunPkt(i)
	}

	return this.FetchAndProcessTunPkt(255)
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
			for {
				conns, ok := this.routes[dst]
				if !ok {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no route, packet dropped")
					break
				}

				keys := []string{}
				for k := range conns {
					keys = append(keys, k)
				}

				if len(keys) == 0 {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no conns, packet dropped")
					break
				}

				idx := rand.Intn(len(keys))

				conn := this.server.GetConnsByAddr(keys[idx])
				if conn == nil || conn.IsClosed() {
					log.Info().Int("workder", workerNum).Str("src", src).
						Str("dst", dst).
						Msg("FetchAndProcessTunPkt::no connection, packet dropped")
					delete(conns, keys[idx])
					this.routes[dst] = conns
					this.server.DeleteDeadConn(dst)
				} else {
					conn.SendPacket(pkt)
					break
				}
			}
		} else {
			//client send packet
			this.client.SendPacket(pkt)
		}
	}
}

func (this *App) OnData(buf []byte, conn *net.TCPConn) {
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
		this.mutex.Lock()

		if _, ok := this.routes[ping.GetIP()]; ok {
			this.routes[ping.GetIP()][ping.GetLocalAddr()] = struct{}{}
		} else {
			this.routes[ping.GetIP()] = map[string]struct{}{
				ping.GetLocalAddr(): struct{}{},
			}
		}

		log.Debug().Str("local", ping.GetLocalAddr()).Str("ip", ping.GetIP()).
			Msg("Proto Ping")

		log.Info().Interface("route", this.routes).
			Msg("Route Table")

		this.server.SetConns(ping.GetLocalAddr(), conn)
		this.mutex.Unlock()
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())

		log.Debug().Int("pkt_len", len(pkt)).IPAddr("src", pkt.GetSourceIP()).
			IPAddr("dst", pkt.GetDestinationIP()).
			Msg("received protobuf packet")

		this.iface.Write(pkt)
	}
}
