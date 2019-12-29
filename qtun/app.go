package qtun

import (
	"log"
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
	client *Peer
	routes map[string]Route
	mutex  sync.RWMutex
	server *transport.Server
	iface  *iface.Iface
}

func NewApp() *App {
	return &App{
		config: config.GetInstance(),
		routes: make(map[string]Route),
	}
}

func (a *App) Run() error {
	if config.GetInstance().ServerMode {
		a.server = transport.NewServer(a.config.Listen, a, a.config.Key)
		// a.server.SetConfig(a.config)
		go a.server.Start()
	} else {
		err := a.InitClient()
		if err != nil {
			return err
		}
	}

	return a.StartFetchTunInterface()
}

func (a *App) StartFetchTunInterface() error {
	a.iface = iface.New("", a.config.Ip, a.config.Mtu)
	err := a.iface.Start()
	if err != nil {
		return err
	}

	for i := 0; i < 10; i++ {
		go a.FetchAndProcessTunPkt(i)
	}

	return a.FetchAndProcessTunPkt(255)
}

func (a *App) FetchAndProcessTunPkt(workerNum int) error {
	mtu := config.GetInstance().Mtu
	pkt := iface.NewPacketIP(mtu)
	for {
		n, err := a.iface.Read(pkt)
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
				workerNum, dst, a.routes[dst].LocalAddr)
			conn := a.server.GetConnsByAddr(a.routes[dst].LocalAddr)
			if conn == nil {
				log.Printf("FetchAndProcessTunPkt::unknown destination, packet dropped  worker=%d, src=%s,dst=%s", workerNum, src, dst)
			} else {
				conn.SendPacket(pkt)
			}
		} else {
			//client send packet
			a.client.SendPacket(pkt)
		}
	}
}

func (a *App) InitClient() error {
	//For server no need to make connection to client -gs
	// if a.config.ServerMode == 0 {
	peer := NewPeer(a)
	peer.Start()
	a.client = peer
	return nil
}

func (a *App) OnData(buf []byte, conn *net.TCPConn) {
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
		a.mutex.Lock()
		a.routes[ping.GetIP()] = Route{
			LocalAddr: ping.GetLocalAddr(),
			IP:        ping.GetIP(),
		}

		log.Printf("Proto Ping local=%s, ip=%s", ping.GetLocalAddr(), ping.GetIP())

		a.server.SetConns(a.routes[ping.GetIP()].LocalAddr, conn)
		if config.GetInstance().Verbose {
			log.Printf("OnData::routes %s", a.routes)
		}
		a.mutex.Unlock()
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())
		if config.GetInstance().Verbose {
			log.Printf("OnData::received packet: src=%s dst=%s len=%d",
				pkt.GetSourceIP(), pkt.GetDestinationIP(), len(pkt))
		}
		a.iface.Write(pkt)
	}
}
