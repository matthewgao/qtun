package transport

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	remoteAddr string
	key        string
	threads    int
	conns      []*ClientConn
	mutex      sync.RWMutex
	serial     int64
	wg         sync.WaitGroup
	handler    ServerHandler
}

func NewClient(remoteAddr string, key string, threads int) *Client {
	return &Client{
		remoteAddr: remoteAddr,
		key:        key,
		threads:    threads,
	}
}

func (c *Client) SetHandler(handler ServerHandler) {
	c.handler = handler
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
		time.Sleep(time.Second) // FIXME:
		go c.conns[connIndex].runRead()
	}
	c.mutex.Unlock()
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
