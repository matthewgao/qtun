//go:build windows

package iface

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

// ringCapacity 是 Wintun 收发环形缓冲大小（4 MiB），必须是 2 的幂、范围
// [128 KiB, 64 MiB]。越大越能吸收突发，代价是内存占用。
const ringCapacity = 0x400000

// defaultAdapterName 在调用方未指定网卡名时使用（app.go 目前传空串）。
const defaultAdapterName = "qtun"

// windowsDevice 是 Windows 上的 Device 实现，基于 WireGuard 的 Wintun 驱动。
// Wintun 是 Layer 3 设备，Read/Write 直接处理裸 IP 包，与 packet_ip.go 的
// IPv4 偏移假设一致，无需像 TAP 那样处理以太网帧 / ARP。
// 网卡 IP/MTU 用 netsh 配置（与 Unix 端用 ifconfig 同思路），避免引入庞大的
// wireguard-windows 依赖。
type windowsDevice struct {
	name     string
	ip       string
	mtu      int
	adapter  *wintun.Adapter
	session  wintun.Session
	readWait windows.Handle
	// readMu 序列化接收侧：Wintun 的 ReceivePacket 是单消费者（SPSC 环），不可被
	// 多个 goroutine 并发调用，否则会破坏环。但 app.go 的 FetchAndProcessTunPkt 起了
	// 2×CPU 个 worker 同时调用 Read（Unix 下内核 read() 天然序列化，Windows 没有这层
	// 保证）。这里全程加锁串行化 Read；发送侧 Write 由单个 tunWriter 调用，本就单
	// 消费者，与接收可并发，无需此锁。
	readMu sync.Mutex
}

func newDevice(name, ip string, mtu int) Device {
	if name == "" {
		name = defaultAdapterName
	}
	return &windowsDevice{
		name: name,
		ip:   ip,
		mtu:  mtu,
	}
}

func (i *windowsDevice) Start() error {
	// 解析 --ip（形如 10.4.4.3/24）为地址与子网掩码。
	ip, ipNet, err := net.ParseCIDR(i.ip)
	if err != nil {
		return fmt.Errorf("parse cidr %q: %w", i.ip, err)
	}

	// 创建 Wintun 适配器（需要管理员权限；wintun.dll 必须可被加载）。
	adapter, err := wintun.CreateAdapter(i.name, "Wintun", nil)
	if err != nil {
		return fmt.Errorf("create wintun adapter (需要管理员权限且 wintun.dll 在 exe 同目录): %w", err)
	}
	i.adapter = adapter

	session, err := adapter.StartSession(ringCapacity)
	if err != nil {
		adapter.Close()
		i.adapter = nil
		return fmt.Errorf("start wintun session: %w", err)
	}
	i.session = session
	i.readWait = session.ReadWaitEvent()

	log.Info().Str("tun_name", i.name).Msg("wintun adapter created")

	// 配置 IP（netsh 的 set address static 会顺带建立 on-link 子网路由）。
	mask := ipNet.Mask
	netmask := fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
	if out, err := exec.Command("netsh", "interface", "ip", "set", "address",
		"name="+i.name, "static", ip.String(), netmask).CombinedOutput(); err != nil {
		i.Close()
		return fmt.Errorf("netsh set address fail: %s %s", err, string(out))
	}

	// 配置 MTU（失败不致命，仅告警）。
	if out, err := exec.Command("netsh", "interface", "ipv4", "set", "subinterface",
		i.name, fmt.Sprintf("mtu=%d", i.mtu), "store=persistent").CombinedOutput(); err != nil {
		log.Warn().Err(err).Str("cmd_output", string(out)).Msg("netsh set mtu fail")
	}

	log.Info().Str("tun_name", i.name).Str("ip", i.ip).Int("mtu", i.mtu).
		Msg("wintun interface up")
	return nil
}

func (i *windowsDevice) Name() string {
	return i.name
}

// Read 阻塞读取一个 IP 包。Wintun 无包时 ReceivePacket 返回 ERROR_NO_MORE_ITEMS，
// 必须用 ReadWaitEvent 等待新包，否则会变成 busy-loop 跑满 CPU。
func (i *windowsDevice) Read(pkt PacketIP) (int, error) {
	i.readMu.Lock()
	defer i.readMu.Unlock()
	for {
		packet, err := i.session.ReceivePacket()
		if err == nil {
			n := copy(pkt, packet)
			i.session.ReleaseReceivePacket(packet)
			return n, nil
		}
		switch err {
		case windows.ERROR_NO_MORE_ITEMS:
			windows.WaitForSingleObject(i.readWait, windows.INFINITE)
			continue
		case windows.ERROR_HANDLE_EOF:
			return 0, os.ErrClosed
		default:
			return 0, err
		}
	}
}

func (i *windowsDevice) Write(pkt PacketIP) (int, error) {
	packet, err := i.session.AllocateSendPacket(len(pkt))
	if err != nil {
		// ERROR_BUFFER_OVERFLOW：发送环已满，丢弃该包（上层会重传内层 TCP）。
		if err == windows.ERROR_BUFFER_OVERFLOW {
			return 0, nil
		}
		return 0, err
	}
	copy(packet, pkt)
	i.session.SendPacket(packet)
	return len(pkt), nil
}

func (i *windowsDevice) Close() error {
	if i.session != (wintun.Session{}) {
		i.session.End()
		i.session = wintun.Session{}
	}
	if i.adapter != nil {
		err := i.adapter.Close()
		i.adapter = nil
		return err
	}
	return nil
}
