package qtun

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"

	"github.com/golang/protobuf/proto"
	"github.com/matthewgao/qtun/transport"
	"github.com/matthewgao/qtun/utils"
)

type Peer struct {
	// remoteDC   string
	remoteAddr string
	config     config.Config
	client     *transport.Client
}

func NewPeer(cfg config.Config, handler transport.ServerHandler) *Peer {
	peer := &Peer{
		remoteAddr: cfg.RemoteAddrs,
		config:     cfg,
		client:     transport.NewClient(cfg.RemoteAddrs, cfg.Key, cfg.TransportThreads),
	}

	peer.client.SetHandler(handler)
	return peer
}

func (p *Peer) Start() {
	p.client.Start()
	go p.process()
}

func (p *Peer) process() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("peer process panic: %s", err)
		}
	}()
	tickerPing := time.NewTicker(time.Second * 2)
	defer tickerPing.Stop()
	for range tickerPing.C {
		p.SendPing()
	}
}

func (p *Peer) GetTunLocalAddrWithPort() string {
	return fmt.Sprintf("%s:%s", p.config.Ip, p.client.GetRemotePortRandom())
}

func (p *Peer) SendPing() {
	ip, _, err := net.ParseCIDR(p.config.Ip)
	utils.POE(err)
	env := &protocol.Envelope{
		Type: &protocol.Envelope_Ping{
			Ping: &protocol.MessagePing{
				Timestamp:        time.Now().UnixNano(),
				LocalAddr:        p.GetTunLocalAddrWithPort(), //唯一的表示一个CLINET端
				LocalPrivateAddr: "not_use",
				DC:               "client",
				IP:               ip.String(),
			},
		},
	}
	data, err := proto.Marshal(env)
	utils.POE(err)
	p.client.Write(data)
}

func (p *Peer) SendPacket(pkt iface.PacketIP) {
	data, _ := proto.Marshal(&protocol.Envelope{
		Type: &protocol.Envelope_Packet{
			Packet: &protocol.MessagePacket{Payload: pkt},
		},
	})
	p.client.Write(data)
}
