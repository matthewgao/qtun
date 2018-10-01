package transport

import "net"

type ServerHandler interface {
	OnData([]byte, *net.TCPConn)
}
