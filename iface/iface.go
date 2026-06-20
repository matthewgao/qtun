package iface

// Device 是平台无关的 TUN 设备抽象。各平台（Unix 用 songgao/water，Windows 用
// Wintun）各自实现，业务层（qtun/app.go）只依赖此接口，从而把平台相关代码及其
// 专属依赖隔离在 iface_<os>.go 中，互不影响编译。
type Device interface {
	// Start 创建并配置底层 TUN 设备（IP、MTU、必要的路由）。
	Start() error
	// Read 从设备读取一个裸 IP 包到 pkt，返回字节数。
	Read(pkt PacketIP) (int, error)
	// Write 把一个裸 IP 包写入设备。
	Write(pkt PacketIP) (int, error)
	// Name 返回底层网卡名。
	Name() string
	// Close 释放设备资源。
	Close() error
}

// New 按当前编译平台构造一个 Device。具体实现见 iface_unix.go / iface_windows.go
// 中的 newDevice。
func New(name, ip string, mtu int) Device {
	return newDevice(name, ip, mtu)
}
