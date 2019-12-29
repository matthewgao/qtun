package transport

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/utils"

	"github.com/golang/protobuf/proto"
)

type Client struct {
	remoteAddr string
	key        string
	threads    int
	conns      []*ClientConn
	mutex      sync.RWMutex
	serial     int64
	wg         sync.WaitGroup
	handler    GrpcHandler
}

func NewClient(remoteAddr string, key string, threads int, handler GrpcHandler) *Client {
	return &Client{
		remoteAddr: remoteAddr,
		key:        key,
		threads:    threads,
		handler:    handler,
	}

}

func (c *Client) Start() {
	c.mutex.Lock()
	c.conns = make([]*ClientConn, c.threads)
	for connIndex := 0; connIndex < c.threads; connIndex++ {
		c.wg.Add(1)
		conn := NewClientConn(c.remoteAddr, c.key, connIndex, &c.wg)
		conn.SetHander(c.handler)
		c.conns[connIndex] = conn

		go c.conns[connIndex].run()
		time.Sleep(time.Millisecond * 1000) // FIXME:
		go c.conns[connIndex].runRead()
	}
	c.mutex.Unlock()
	go c.ping()
}

func (c *Client) Stop() {
	defer log.Printf("client stopped")
	c.mutex.RLock()
	for _, conn := range c.conns {
		conn.Close()
	}
	c.mutex.RUnlock()
	c.wg.Wait()
}

func (c *Client) ConnectWait() {
	for {
		count := 0
		c.mutex.RLock()
		for _, conn := range c.conns {
			if conn.IsConnected() {
				count++
			}
		}
		c.mutex.RUnlock()
		if count == c.threads {
			return
		}
		time.Sleep(time.Second)
	}
}

//随机找一个连接发送请求
func (c *Client) WriteNow(data []byte) {
	if c.threads == 1 {
		conn := c.conns[0]
		if err := conn.WriteNow(data); err != nil {
			conn.Write(data)
		}
		return
	}
	serial := atomic.AddInt64(&c.serial, 1)
	next := int(serial) % c.threads
	conn := c.conns[next]
	if err := conn.WriteNow(data); err != nil {
		conn.Write(data)
	}
}

func (c *Client) Write(data []byte) {
	serial := atomic.AddInt64(&c.serial, 1)
	next := int(serial) % c.threads
	conn := c.conns[next]
	conn.Write(data)
}

//随机找一个可用的连接，为了获取连接地址
func (c *Client) GetRemotePortRandom() string {
	serial := atomic.AddInt64(&c.serial, 1)
	next := int(serial) % c.threads
	conn := c.conns[next]
	return conn.GetConnPort()
}

func (c *Client) ping() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("peer ping panic: %s", err)
		}
	}()
	tickerPing := time.NewTicker(time.Second * 2)
	defer tickerPing.Stop()
	for range tickerPing.C {
		c.SendPing()
	}
}

func (c *Client) GetTunLocalAddrWithPort() string {
	return fmt.Sprintf("%s:%s", config.GetInstance().Ip, c.GetRemotePortRandom())
}

func (c *Client) SendPing() {
	ip, _, err := net.ParseCIDR(config.GetInstance().Ip)
	utils.POE(err)
	env := &protocol.Envelope{
		Type: &protocol.Envelope_Ping{
			Ping: &protocol.MessagePing{
				Timestamp:        time.Now().UnixNano(),
				LocalAddr:        c.GetTunLocalAddrWithPort(), //唯一的表示一个CLINET端
				LocalPrivateAddr: "not_use",
				DC:               "client",
				IP:               ip.String(),
			},
		},
	}

	data, err := proto.Marshal(env)
	utils.POE(err)
	c.Write(data)
}

func (c *Client) SendPacket(pkt iface.PacketIP) {
	data, _ := proto.Marshal(&protocol.Envelope{
		Type: &protocol.Envelope_Packet{
			Packet: &protocol.MessagePacket{Payload: pkt},
		},
	})
	c.Write(data)
}
