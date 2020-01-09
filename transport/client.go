package transport

import (
	"fmt"
	// "log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/iface"
	"github.com/matthewgao/qtun/protocol"
	"github.com/matthewgao/qtun/utils"

	"github.com/golang/protobuf/proto"
	"github.com/rs/zerolog/log"
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
		conn := NewClientConn(c.remoteAddr, c.key, connIndex, &c.wg, config.GetInstance().NoDelay)
		conn.SetHander(c.handler)

		go conn.run()
		time.Sleep(time.Millisecond * 200)

		connected := false
		for index := 0; index < 10; index++ {
			if conn.IsConnected() {
				connected = true
				break
			}
			time.Sleep(time.Millisecond * 500)
		}

		if connected {
			c.conns[connIndex] = conn
			go c.conns[connIndex].runRead()
		} else {
			log.Warn().Str("server_addr", c.remoteAddr).
				Msg("fail to connect to the server after 10 times retry")

			c.wg.Done()
		}
	}

	log.Info().Str("server_addr", c.remoteAddr).Int("conn_num", len(c.conns)).
		Msg("connections has been establlished")

	c.mutex.Unlock()

	go func() {
		for {
			c.ping()
			time.Sleep(time.Second)
			log.Warn().Str("server_addr", c.remoteAddr).Int("conn_num", len(c.conns)).
				Msg("client ping exit, restart")
		}
	}()
}

func (c *Client) Stop() {
	defer log.Info().Str("server_addr", c.remoteAddr).Int("conn_num", len(c.conns)).
		Msg("client stop")

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
// func (c *Client) GetRemotePortRandom() string {
// 	serial := atomic.AddInt64(&c.serial, 1)
// 	next := int(serial) % c.threads
// 	conn := c.conns[next]
// 	return conn.GetConnPort()
// }

func (c *Client) ping() {
	defer func() {
		if err := recover(); err != nil {
			// log.Printf("peer ping panic: %s", err)
			log.Error().Interface("err", err).Str("server_addr", c.remoteAddr).Int("conn_num", len(c.conns)).
				Msg("peer ping panic")
		}
	}()

	tickerPing := time.NewTicker(time.Second * 2)
	defer tickerPing.Stop()
	for range tickerPing.C {
		c.SendAllPing()
	}
}

// func (c *Client) GetTunLocalAddrWithPort() string {
// 	return fmt.Sprintf("%s:%s", config.GetInstance().Ip, c.GetRemotePortRandom())
// }

func (c *Client) GetTunLocalAddrWithPortOnConn(conn *ClientConn) string {
	return fmt.Sprintf("%s:%s", config.GetInstance().Ip, conn.GetConnPort())
}

func (c *Client) SendAllPing() {
	for _, v := range c.conns {
		if v == nil {
			//FIXME: should delete conn
			continue
		}

		c.SendPing(v)
	}
}

func (c *Client) SendPing(conn *ClientConn) {
	ip, _, err := net.ParseCIDR(config.GetInstance().Ip)
	utils.POE(err)

	localAddr := c.GetTunLocalAddrWithPortOnConn(conn)
	env := &protocol.Envelope{
		Type: &protocol.Envelope_Ping{
			Ping: &protocol.MessagePing{
				Timestamp:        time.Now().UnixNano(),
				LocalAddr:        localAddr, //唯一的表示一个CLINET端的一个连接
				LocalPrivateAddr: "not_use",
				DC:               "client",
				IP:               ip.String(),
			},
		},
	}

	log.Debug().Str("local_addr", localAddr).Int("conn_num", len(c.conns)).IPAddr("client_vip", ip).
		Msg("send ping")
	data, err := proto.Marshal(env)
	utils.POE(err)

	conn.WriteNow(data)
}

func (c *Client) SendPacket(pkt iface.PacketIP) {
	data, _ := proto.Marshal(&protocol.Envelope{
		Type: &protocol.Envelope_Packet{
			Packet: &protocol.MessagePacket{Payload: pkt},
		},
	})
	c.Write(data)
}
