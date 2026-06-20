//go:build linux || darwin

package iface

import (
	"fmt"
	// "log"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/songgao/water"
)

// unixDevice 是 Linux / macOS 上的 Device 实现，基于 songgao/water 创建 TUN 设备，
// 用 ifconfig / route 命令配置 IP、MTU 与路由。
type unixDevice struct {
	name string
	ip   string
	mtu  int
	ifce *water.Interface
}

func newDevice(name, ip string, mtu int) Device {
	return &unixDevice{
		name: name,
		ip:   ip,
		mtu:  mtu,
	}
}

func (i *unixDevice) Start() error {
	ip, netIP, err := net.ParseCIDR(i.ip)
	if err != nil {
		return err
	}
	config := water.Config{
		DeviceType: water.TUN,
	}

	i.ifce, err = water.New(config)
	if err != nil {
		return err
	}

	log.Info().Str("tun_name", i.ifce.Name()).
		Msg("tun interface")

	mask := netIP.Mask
	netmask := fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("ifconfig", i.Name(),
			ip.String(), ip.String(), "netmask", netmask,
			"mtu", strconv.Itoa(i.mtu), "up")
	} else {
		cmd = exec.Command("ifconfig", i.Name(),
			ip.String(), "netmask", netmask,
			"mtu", strconv.Itoa(i.mtu), "up")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("cmd_output", string(output)).
			Msg("run ifconfig fail")

		return fmt.Errorf("err: %s %s", err, string(output))
	}

	if runtime.GOOS == "darwin" {
		i.AddSysRoute(&ip)
	}

	return nil
}

func (i *unixDevice) AddSysRoute(ip *net.IP) {
	ipdot := strings.Split(ip.String(), ".")
	subnet := strings.Join(ipdot[:len(ipdot)-1], ".") + ".0"
	// log.Printf(subnet)
	log.Debug().Str("subnet", subnet).
		Msg("subnet")

	cmd := exec.Command("route", "add", "-net",
		subnet, ip.String())

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("cmd_output", string(output)).
			Msg("add system route fail")
		panic(fmt.Sprintf("err: %s %s", err, string(output)))
	}
}

func (i *unixDevice) Name() string {
	return i.ifce.Name()
}

func (i *unixDevice) Read(pkt PacketIP) (int, error) {
	return i.ifce.Read(pkt)
}

func (i *unixDevice) Write(pkt PacketIP) (int, error) {
	return i.ifce.Write(pkt)
}

func (i *unixDevice) Close() error {
	if i.ifce != nil {
		return i.ifce.Close()
	}
	return nil
}
