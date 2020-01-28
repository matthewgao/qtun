package transport

type GrpcHandler interface {
	ClientOnData([]byte)
	ServerOnData([]byte, *ServerConn)
}
