package transport

import (
	"io"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/matthewgao/qtun/iface"
)

type fakeUDPConn struct {
	mu       sync.Mutex
	writeErr error
	closed   bool
}

func (f *fakeUDPConn) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (f *fakeUDPConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeUDPConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeUDPConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func TestUDPClientReconnectsAfterLocalAddrWriteFailure(t *testing.T) {
	oldConn := &fakeUDPConn{writeErr: syscall.EADDRNOTAVAIL}
	newConn := &fakeUDPConn{}
	dialed := make(chan struct{}, 1)

	client := &UDPClient{
		conn:           oldConn,
		closed:         make(chan struct{}),
		reconnectCh:    make(chan struct{}, 1),
		reconnectDelay: time.Millisecond,
		dialUDP: func() (udpPacketConn, error) {
			dialed <- struct{}{}
			return newConn, nil
		},
	}
	go client.reconnectLoop()
	t.Cleanup(client.Stop)

	client.SendPacket(testIPv4Packet())

	select {
	case <-dialed:
	case <-time.After(time.Second):
		t.Fatal("expected EADDRNOTAVAIL write failure to trigger a reconnect")
	}

	waitFor(t, time.Second, func() bool {
		return client.getConn() == newConn && oldConn.isClosed()
	})
}

func TestUDPClientDoesNotReconnectForRemotePortUnreachable(t *testing.T) {
	client := &UDPClient{
		closed:         make(chan struct{}),
		reconnectCh:    make(chan struct{}, 1),
		reconnectDelay: time.Millisecond,
		dialUDP: func() (udpPacketConn, error) {
			t.Fatal("ECONNREFUSED should not force a UDP socket reconnect")
			return nil, nil
		},
	}
	client.requestReconnect(syscall.ECONNREFUSED)

	select {
	case <-client.reconnectCh:
		t.Fatal("ECONNREFUSED should be tolerated so a restarted server can recover on the same socket")
	default:
	}
}

func testIPv4Packet() iface.PacketIP {
	pkt := make(iface.PacketIP, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], []byte{10, 4, 4, 3})
	copy(pkt[16:20], []byte{10, 4, 4, 2})
	return pkt
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("condition was not met before timeout")
	}
}

var _ udpPacketConn = (*fakeUDPConn)(nil)
