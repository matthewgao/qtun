package qtun

import (
	"log"
	"math/rand"
	"net"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/transport"
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
		// err := this.InitClient()
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
			log.Printf("FetchAndProcessTunPkt::read ip pkt error: %v", err)
			return err
		}
		src := pkt.GetSourceIP().String()
		dst := pkt.GetDestinationIP().String()
		if config.GetInstance().Verbose {
			log.Printf("FetchAndProcessTunPkt::got tun packet: worker=%d, src=%s dst=%s len=%d", workerNum, src, dst, n)
		}

		if config.GetInstance().ServerMode {
			log.Printf("FetchAndProcessTunPkt::receiver tun packet dst address, worker=%d, dst=%s, route_local_addr=%s",
				workerNum, dst, this.routes[dst])

			conns, ok := this.routes[dst]
			if !ok {
				log.Printf("FetchAndProcessTunPkt::no route, packet dropped  worker=%d, src=%s,dst=%s", workerNum, src, dst)
				continue
			}

			keys := []string{}
			for k := range conns {
				keys = append(keys, k)
			}

			if len(keys) == 0 {
				log.Printf("FetchAndProcessTunPkt::no conns, packet dropped  worker=%d, src=%s,dst=%s", workerNum, src, dst)
				continue
			}

			idx := rand.Intn(len(keys))

			conn := this.server.GetConnsByAddr(keys[idx])
			if conn == nil {
				log.Printf("FetchAndProcessTunPkt::unknown destination, packet dropped  worker=%d, src=%s,dst=%s", workerNum, src, dst)
			} else {
				conn.SendPacket(pkt)
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
		log.Printf("OnData::proto unmarshal err: %s", err)
		return
	}
	switch ep.Type.(type) {
	case *protocol.Envelope_Ping:
		ping := ep.GetPing()
		//log.Printf("received ping: %s", ping.String())
		//根据Client发来的Ping包信息来添加路由
		this.mutex.Lock()

		if _, ok := this.routes[ping.GetIP()]; ok {
			this.routes[ping.GetIP()][ping.GetLocalAddr()] = struct{}{}
		} else {
			this.routes[ping.GetIP()] = map[string]struct{}{
				ping.GetLocalAddr(): struct{}{},
			}
		}

		// this.routes[ping.GetIP()] = Route{
		// 	ClientAddrWithPort: ping.GetLocalAddr(),
		// 	ClientIP:           ping.GetIP(),
		// }

		log.Printf("Proto Ping local=%s, ip=%s", ping.GetLocalAddr(), ping.GetIP())
		log.Printf("route=%v", this.routes)

		this.server.SetConns(ping.GetLocalAddr(), conn)
		if config.GetInstance().Verbose {
			log.Printf("OnData::routes %s", this.routes)
		}
		this.mutex.Unlock()
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())
		if config.GetInstance().Verbose {
			log.Printf("OnData::received packet: src=%s dst=%s len=%d",
				pkt.GetSourceIP(), pkt.GetDestinationIP(), len(pkt))
		}
		this.iface.Write(pkt)
	}
}
