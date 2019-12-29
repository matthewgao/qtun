package transport

import "net"

type GrpcHandler interface {
	OnData([]byte, *net.TCPConn)
}
