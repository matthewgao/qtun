# qtun 使用手册

qtun 是一个基于 **QUIC / 裸 UDP** 的加密网络隧道（VPN）。单个二进制可作 **client** 或
**server**，把本机 TUN 设备与远端对接，并自带 SOCKS5 / HTTP 代理与 PAC 文件服务，便于把流量
导进隧道。本手册覆盖从编译、运行到「手机经树莓派走 qtun」的完整配置。

> 默认数据面走**裸 UDP + AES-GCM**（类 WireGuard，单流吞吐好）。用 `--udp=false` 可回退 QUIC。
> 原理与排查见 `PERFORMANCE_SINGLE_FLOW_DEBUGGING.md`。

---

## 目录
1. [快速开始](#1-快速开始)
2. [编译](#2-编译)
3. [命令行参数](#3-命令行参数)
4. [运行模式](#4-运行模式)
5. [传输与性能开关](#5-传输与性能开关)
6. [场景：手机经树莓派走 qtun](#6-场景手机经树莓派走-qtun)
7. [验证](#7-验证)
8. [开机自启（systemd）](#8-开机自启systemd)
9. [故障排查](#9-故障排查)
10. [安全说明](#10-安全说明)

---

## 1. 快速开始

最小可用：一台公网 server + 一台 client，两端用**相同 `--key` 和相同传输模式**。

```bash
# 服务端（公网机器，需 root + TUN）
sudo ./bin/qtun qt --key "your-secret" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --mtu 1280 --server_mode

# 客户端
sudo ./bin/qtun qt --key "your-secret" --remote_addrs "<server公网IP>:8080" --ip "10.4.4.3/24" --mtu 1280
```

- 默认走 UDP，确保 server 防火墙/安全组放行 **`8080/udp`**。
- 两端 `--ip` 必须在同一网段、地址不同（如 `10.4.4.2/24` 与 `10.4.4.3/24`）。
- `--proxyonly` 模式不需要 root / TUN（仅起本地 SOCKS5 + HTTP 代理）。

---

## 2. 编译

需要 Go 1.24。

```bash
make build          # 本机 -> bin/qtun
make linux          # 64位 Linux  -> bin/qtun-linux
make arm            # 32位树莓派系统 -> bin/qtun-arm
make linux-arm64    # 64位树莓派系统(Pi 3/4/5 + 64-bit OS) -> bin/qtun-arm64
make windows        # Windows x64 -> bin/qtun-win.exe（需 wintun.dll 同目录）
make m4             # Apple Silicon mac -> bin/qtun-m4
```

**树莓派选哪个**：`uname -m` 看架构——`aarch64` 用 `make linux-arm64`，`armv7l` 用 `make arm`。

---

## 3. 命令行参数

子命令为 `qt`（即 `qtun qt ...`）。

| 参数 | 默认 | 说明 |
|---|---|---|
| `--key` | `hello-world` | 共享密钥，**两端必须一致**；既是认证也是 AES-128-GCM 加密密钥 |
| `--server_mode` | false | 以服务端模式运行 |
| `--listen` | `0.0.0.0:8080` | 服务端监听地址（server） |
| `--remote_addrs` | — | 服务端地址 `IP:PORT`（client） |
| `--ip` | `10.237.0.1/16` | 本端隧道内网 VIP/掩码，两端同网段不同址 |
| `--mtu` | `1400` | 隧道 MTU；需为封装留余量（见下） |
| `--udp` | **true** | 数据面走裸 UDP；`=false` 回退 QUIC |
| `--egress_workers` | 0(自动) | 读 TUN 并发数；`1` 可保单流顺序，排查乱序用 |
| `--transport_threads` | 1 | QUIC 并发连接数（**仅 QUIC**） |
| `--flow_hash` | false | QUIC 流亲和（**仅 QUIC**，多连接聚合↑、单流↓） |
| `--proxyonly` | false | 只起本地 SOCKS5+HTTP 代理，不建隧道、不需 root |
| `--socks5_port` | 2080 | SOCKS5 端口（server / proxyonly） |
| `--http_proxy_port` | 2081 | HTTP/HTTPS 代理端口（server / proxyonly） |
| `--file_svr_port` | 6061 | PAC 文件服务端口（client） |
| `--nodelay` | false | TCP_NODELAY（历史项） |
| `--log_level` | `info` | 日志级别 |

固定端口：statsviz 运行面板 `http://localhost:6060/debug/statsviz/`。

**MTU 提示**：内层 MTU + 封装开销(~63B) 若超过路径 MTU(常见 1500) 会分片（一片丢=整包丢）。
默认 1400 安全；公网链路保守用 `--mtu 1280`。启动时若 MTU 偏大会打告警。

---

## 4. 运行模式

### 服务端
```bash
sudo ./bin/qtun qt --key "secret" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --mtu 1280 --server_mode
```
自动起 SOCKS5(2080) + HTTP 代理(2081)，对外（含隧道内 VIP `10.4.4.2`）提供出网代理。

### 客户端
```bash
sudo ./bin/qtun qt --key "secret" --remote_addrs "1.2.3.4:8080" --ip "10.4.4.3/24" --mtu 1280
```
起 PAC 文件服务(6061)；macOS 上会自动设置系统 Wi-Fi 代理，Linux 需手动配置（见场景章节）。

### 仅代理（无隧道、无需 root）
```bash
./bin/qtun qt --proxyonly --socks5_port 2080 --http_proxy_port 2081
```

---

## 5. 传输与性能开关

- **UDP（默认）**：隧道是「哑管道」，拥塞控制交还内层 TCP，**单流吞吐好**。推荐。
- **QUIC（`--udp=false`）**：保留旧路径做 A/B。配 `--transport_threads N` 多连接、
  `--flow_hash` 流亲和——多连接聚合更高但单流受限。
- **`--egress_workers`**：默认自动(2×CPU)。多 worker 会让单条流出向乱序（轻微乱序压低内层
  TCP 窗口却不显示为重传）。单流吞吐异常时设 `--egress_workers 1` 对比。

---

## 6. 场景：手机经树莓派走 qtun

目标：手机网关指向树莓派，流量经 qtun 在远端 server 出网。树莓派作 **client**，远端 VPS 作
**server（出口）**。

> qtun 自身不做 NAT/转发，需用脚本 `scripts/qtun-gw.sh` 在 OS 层补转发/NAT/策略路由。
> 该脚本幂等、可 `down` 回滚、`status` 查看。两种方式：

### 方式 A：HTTP 代理（最简单，不用动 server）
只代理 HTTP/HTTPS，出网由 server 的 goproxy 完成。

```bash
# 树莓派（client）
sudo ./scripts/qtun-gw.sh up --mode proxy --role client
```
**手机**：Wi-Fi → 网关设为树莓派局域网 IP；手动 HTTP 代理 = `10.4.4.2`，端口 `2081`。

### 方式 B：全局透明（所有流量，含 UDP/DNS）
手机不用设代理，但 **server 端也要配**。

```bash
# 树莓派（client）：--server-ip 填 qtun 远端公网 IP（防隧道底层回环，必填）
sudo ./scripts/qtun-gw.sh up --mode transparent --role client --server-ip 1.2.3.4
# 远端 server
sudo ./scripts/qtun-gw.sh up --mode transparent --role server
```
**手机**：Wi-Fi → 网关设为树莓派局域网 IP，**无需设代理**。

### 脚本动作对照
| role/mode | 做了什么 |
|---|---|
| client/proxy | `ip_forward` + `MASQUERADE -o tun`(回包找路) + `MASQUERADE -o wan`(普通流量本地出网) + FORWARD 放行 |
| client/transparent | 上述 + 策略路由表(本地 LAN 直连、其余 `default dev tun`) + 3 条 `ip rule`(到 server 直连防回环 / 树莓派自身不进隧道 / LAN→隧道) + rp_filter 放宽 |
| server/transparent | `ip_forward` + `MASQUERADE -s 10.4.4.0/24 -o wan` + FORWARD 放行 |
| server/proxy | 无需配置 |

参数可自动探测；不准时用 `--tun/--wan/--lan-subnet/--tun-subnet/--table` 覆盖。回滚：把 `up`
换 `down`、参数照填。查看：`sudo ./scripts/qtun-gw.sh status`。

### 手机端设置位置
- **iOS**：设置 → Wi-Fi → ⓘ → 配置代理 → 手动（仅支持 HTTP 代理，方式 A 用）。
- **Android**：Wi-Fi → 修改网络 → 高级 → 代理 → 手动。
- 网关/DHCP：把手机所在网络的网关指向树莓派（或树莓派发 DHCP 把网关设成自己）。

---

## 7. 验证

```bash
# 树莓派/服务端：看隧道有无流量
sudo tcpdump -ni tun0
# 方式 A 还可盯代理端口
sudo tcpdump -ni tun0 host 10.4.4.2 and port 2081

# 隧道连通性（client 上 ping server VIP）
ping 10.4.4.2

# 看 OS 层规则是否就位
sudo ./scripts/qtun-gw.sh status
```
最终确认：**手机访问 `ip.sb` / `whatismyip`，公网 IP 应显示为 server 的 IP**。

---

## 8. 开机自启（systemd）

`/etc/systemd/system/qtun.service`（树莓派 client + 全局透明为例）：
```ini
[Unit]
Description=qtun client
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/qtun/bin/qtun-arm64 qt --key secret --remote_addrs 1.2.3.4:8080 --ip 10.4.4.3/24 --mtu 1280
# 隧道起来后再配 OS 规则（延时确保 tun 已创建）
ExecStartPost=/bin/sh -c 'sleep 3; /opt/qtun/scripts/qtun-gw.sh up --mode transparent --role client --server-ip 1.2.3.4'
ExecStopPost=/opt/qtun/scripts/qtun-gw.sh down --mode transparent --role client --server-ip 1.2.3.4
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```
```bash
sudo systemctl daemon-reload && sudo systemctl enable --now qtun
journalctl -u qtun -f      # 看日志
```
iptables 持久化（可选，配合上面更省心）：`sudo apt install iptables-persistent`。

---

## 9. 故障排查

| 现象 | 排查 |
|---|---|
| 连不上、客户端反复重连 | server 是否放行 `8080/udp`；两端 `--key` 是否一致；两端是否都 `--udp`（一端 UDP 一端 QUIC 必失败） |
| 隧道通但手机不能上网 | `--server-ip` 是否填对（透明模式）；server 端是否执行了 `--role server`；`status` 看规则与 `ip_forward=1` |
| 网速慢/单流上不去 | 用 `--mtu 1280`；`--egress_workers 1` 对比；参见 `PERFORMANCE_SINGLE_FLOW_DEBUGGING.md` |
| 启动告警 MTU 偏大 | 调小 `--mtu`（如 1280） |
| `SendDatagram 丢弃` 告警 | 报文超 MTU，调小 `--mtu` |
| 透明模式 `default dev tun` 不通 | 把脚本里该行改成 `default via 10.4.4.2 dev $TUN` |
| 改了规则想还原 | `sudo ./scripts/qtun-gw.sh down ...`（参数同 up） |

日志调详细：加 `--log_level debug`。运行面板：`http://localhost:6060/debug/statsviz/`。

---

## 10. 安全说明

- 真正的认证与加密是**共享 `--key`**（AES-128-GCM，密钥=口令的 MD5）。密钥错误在 server 侧
  表现为解密失败并丢弃。**请用足够强的 key。**
- QUIC 的 TLS 层不是安全边界（自签证书 + `InsecureSkipVerify`），仅作传输载体。
- 方式 A（HTTP 代理）中，手机↔树莓派那段是局域网内明文代理；树莓派↔server 由 qtun 加密。
- 隧道外那段（server→目标站点）是普通出网，不被 qtun 加密——与任何 VPN/代理一致。
