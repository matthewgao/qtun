package meshbird

import (
	"log"
	"net"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/meshbird/config"
	"github.com/matthewgao/meshbird/iface"
	"github.com/matthewgao/meshbird/protocol"
	"github.com/matthewgao/meshbird/transport"
)

type App struct {
	config config.Config
	client *Peer
	routes map[string]Route
	mutex  sync.RWMutex
	server *transport.Server
	iface  *iface.Iface
}

func NewApp(config config.Config) *App {
	return &App{
		config: config,
		routes: make(map[string]Route),
	}
}

func (a *App) Run() error {
	if a.config.ServerMode == 1 {
		a.server = transport.NewServer(a.config.Listen, "not_use_private_address", a, a.config.Key)
		a.server.SetConfig(a.config)
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
	pkt := iface.NewPacketIP(a.config.Mtu)
	if a.config.Verbose == 1 {
		log.Printf("interface name: %s", a.iface.Name())
	}
	for {
		n, err := a.iface.Read(pkt)
		if err != nil {
			return err
		}
		src := pkt.GetSourceIP().String()
		dst := pkt.GetDestinationIP().String()
		if a.config.Verbose == 1 {
			log.Printf("tun packet: src=%s dst=%s len=%d", src, dst, n)
		}
		// a.mutex.RLock()
		// peer, ok := a.peers[a.routes[dst].LocalAddr]
		if a.config.ServerMode == 1 {
			log.Printf("receiver tun packet dst address  dst=%s, route_local_addr=%s", dst, a.routes[dst].LocalAddr)
			conn := a.server.GetConnsByAddr(a.routes[dst].LocalAddr)
			if conn == nil {
				if a.config.Verbose == 1 {
					log.Printf("unknown destination, packet dropped")
				}
			} else {
				conn.SendPacket(pkt)
			}
		} else {
			//client send packet
			a.client.SendPacket(pkt)
		}
		// a.mutex.RUnlock()

	}
}

func (a *App) InitClient() error {
	//For server no need to make connection to client -gs
	// if a.config.ServerMode == 0 {
	peer := NewPeer(a.config, a)
	peer.Start()
	a.client = peer
	return nil
}

func (a *App) getRoutes() []Route {
	a.mutex.Lock()
	routes := make([]Route, len(a.routes))
	i := 0
	for _, route := range a.routes {
		routes[i] = route
		i++
	}
	a.mutex.Unlock()
	return routes
}

func (a *App) OnData(buf []byte, conn *net.TCPConn) {
	ep := protocol.Envelope{}
	err := proto.Unmarshal(buf, &ep)
	if err != nil {
		log.Printf("proto unmarshal err: %s", err)
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

		a.server.SetConns(a.routes[ping.GetIP()].LocalAddr, conn)
		if a.config.Verbose == 1 {
			log.Printf("routes %s", a.routes)
		}
		a.mutex.Unlock()
	case *protocol.Envelope_Packet:
		pkt := iface.PacketIP(ep.GetPacket().GetPayload())
		if a.config.Verbose == 1 {
			log.Printf("received packet: src=%s dst=%s len=%d",
				pkt.GetSourceIP(), pkt.GetDestinationIP(), len(pkt))
		}
		a.iface.Write(pkt)
	}
}
