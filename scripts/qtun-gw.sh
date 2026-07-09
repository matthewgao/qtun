#!/usr/bin/env bash
#
# qtun-gw.sh —— qtun 网关 OS 层配置脚本（转发 / NAT / 策略路由）
#
# qtun 本身只在两个 TUN（VIP 10.4.4.x）之间搬运 IP 包，不做 NAT/转发。把树莓派当网关、
# 让手机流量走 qtun，需要在 OS 层补这些规则。本脚本封装两种模式 × 两个角色，全部幂等、
# 可一键回滚（down）。
#
# 两种模式：
#   proxy        手机设 HTTP 代理 = <server-vip>:2081，只代理 HTTP/HTTPS。出网由 server 上
#                的 goproxy 完成，无需改 server。最简单、最稳。
#   transparent  真·全局：手机所有流量（含 UDP/DNS）透明经隧道在 server 出网。需要 client
#                和 server 两端都配置（策略路由 + 双端 NAT）。gfwlist/PAC 不再起作用。
#
# 用法：
#   sudo ./qtun-gw.sh up   --mode proxy       --role client
#   sudo ./qtun-gw.sh up   --mode transparent --role client --server-ip <server公网IP>
#   sudo ./qtun-gw.sh up   --mode transparent --role server
#   sudo ./qtun-gw.sh down --mode <m> --role <r> [...同 up 的参数]
#   sudo ./qtun-gw.sh status
#
# 可选参数（不填则自动探测）：
#   --tun <dev>           qtun 的 TUN 设备名（默认自动取第一个 tun*）
#   --wan <dev>           上网网卡（默认取默认路由的出口网卡）
#   --lan-subnet <cidr>   手机所在子网，transparent/client 用（默认取 WAN 网卡的直连子网）
#   --tun-subnet <cidr>   隧道内网段（默认 10.4.4.0/24）
#   --server-ip <ip>      server 公网 IP，transparent/client 必填（用于排除隧道底层流量回环）
#   --table <id>          策略路由表号（默认 100）
#
set -euo pipefail

# ---------------------------------------------------------------------------
# 参数解析与默认值
# ---------------------------------------------------------------------------
ACTION="${1:-up}"
[[ "$ACTION" =~ ^(up|down|status)$ ]] || { echo "首个参数须为 up|down|status"; exit 1; }
shift || true

MODE=""; ROLE=""; TUN=""; WAN=""; LAN_SUBNET=""; TUN_SUBNET="10.4.4.0/24"
SERVER_IP=""; TABLE="100"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)       MODE="$2"; shift 2;;
    --role)       ROLE="$2"; shift 2;;
    --tun)        TUN="$2"; shift 2;;
    --wan)        WAN="$2"; shift 2;;
    --lan-subnet) LAN_SUBNET="$2"; shift 2;;
    --tun-subnet) TUN_SUBNET="$2"; shift 2;;
    --server-ip)  SERVER_IP="$2"; shift 2;;
    --table)      TABLE="$2"; shift 2;;
    *) echo "未知参数: $1"; exit 1;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "请用 root（sudo）运行"; exit 1; }

# 自动探测
[[ -n "$WAN" ]] || WAN="$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')"
[[ -n "$TUN" ]] || TUN="$(ip -o link show 2>/dev/null | awk -F': ' '$2 ~ /^tun/{print $2; exit}')"
if [[ -z "$LAN_SUBNET" && -n "$WAN" ]]; then
  LAN_SUBNET="$(ip route show dev "$WAN" scope link proto kernel 2>/dev/null | awk '{print $1; exit}')"
fi

# ---------------------------------------------------------------------------
# 幂等 iptables / ip 规则封装
# ---------------------------------------------------------------------------
ipt_ensure() { # <table> <chain> <rule...>
  local t="$1" c="$2"; shift 2
  iptables -t "$t" -C "$c" "$@" 2>/dev/null || iptables -t "$t" -A "$c" "$@"
}
ipt_remove() { # <table> <chain> <rule...>
  local t="$1" c="$2"; shift 2
  while iptables -t "$t" -C "$c" "$@" 2>/dev/null; do iptables -t "$t" -D "$c" "$@"; done
}
# ip rule 幂等：pref 与 selector 分开传。`ip rule list` 的输出里 pref 显示为行首 "N:"，
# 而 selector（from/to ... lookup ...）原样出现，故按 selector 子串判重，按 pref 增删。
rule_ensure() { # <pref> <selector...>
  local pref="$1"; shift; local sel="$*"
  ip rule list | grep -q "$sel" || ip rule add $sel pref "$pref"
}
rule_remove() { # <pref> <selector...>
  local pref="$1"; shift; local sel="$*"
  while ip rule list | grep -q "$sel"; do
    ip rule del $sel pref "$pref" 2>/dev/null || ip rule del pref "$pref" 2>/dev/null || break
  done
}

set_forward() { # <0|1>
  sysctl -wq net.ipv4.ip_forward="$1"
}

require() { # <var> <name>
  [[ -n "${!1}" ]] || { echo "缺少 $2（自动探测失败，请用对应参数指定）"; exit 1; }
}

# ===========================================================================
# client + proxy ：手机设 HTTP 代理，仅需转发 + 双 MASQUERADE
# ===========================================================================
client_proxy() { # <up|down>
  require TUN "--tun"; require WAN "--wan"
  if [[ "$1" == up ]]; then
    set_forward 1
    ipt_ensure nat POSTROUTING -o "$TUN" -j MASQUERADE        # 手机流量进隧道 → src 改成 10.4.4.3
    ipt_ensure nat POSTROUTING -o "$WAN" -j MASQUERADE        # 手机普通流量经本地出口上网（不走 qtun）
    ipt_ensure filter FORWARD -o "$TUN" -j ACCEPT
    ipt_ensure filter FORWARD -i "$TUN" -j ACCEPT
    echo "[proxy/client] 完成。手机：网关=本机，HTTP 代理=<server-vip>:2081（如 10.4.4.2:2081）"
  else
    ipt_remove nat POSTROUTING -o "$TUN" -j MASQUERADE
    ipt_remove nat POSTROUTING -o "$WAN" -j MASQUERADE
    ipt_remove filter FORWARD -o "$TUN" -j ACCEPT
    ipt_remove filter FORWARD -i "$TUN" -j ACCEPT
    echo "[proxy/client] 规则已清除（ip_forward 未改回，如需关闭：sysctl -w net.ipv4.ip_forward=0）"
  fi
}

# ===========================================================================
# client + transparent ：策略路由把手机流量整体导进 TUN + MASQUERADE
#   关键：必须把"隧道底层(到 server 公网IP)"和"树莓派自身流量"排除出策略表，否则回环。
# ===========================================================================
client_transparent() { # <up|down>
  require TUN "--tun"; require WAN "--wan"; require LAN_SUBNET "--lan-subnet"; require SERVER_IP "--server-ip"
  local PI_IP; PI_IP="$(ip -o -4 addr show dev "$WAN" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)"
  require PI_IP "--wan 上的 IP"

  if [[ "$1" == up ]]; then
    set_forward 1
    sysctl -wq net.ipv4.conf.all.rp_filter=2                  # 非对称路由，rp_filter 设宽松避免误丢
    ipt_ensure nat POSTROUTING -o "$TUN" -j MASQUERADE        # 手机 → 10.4.4.3，server 只见单客户端
    ipt_ensure filter FORWARD -o "$TUN" -j ACCEPT
    ipt_ensure filter FORWARD -i "$TUN" -j ACCEPT

    # 策略路由表：本地 LAN 直连，其余默认走 TUN
    ip route replace "$LAN_SUBNET" dev "$WAN" table "$TABLE"
    ip route replace default dev "$TUN" table "$TABLE"

    # 规则按 pref 由小到大匹配：
    rule_ensure 50  "to $SERVER_IP lookup main"               #   隧道底层永远直连（防回环，最关键）
    rule_ensure 60  "from $PI_IP lookup main"                 #   树莓派自身流量不进隧道
    rule_ensure 100 "from $LAN_SUBNET lookup $TABLE"          #   手机等 LAN 主机 → 隧道
    echo "[transparent/client] 完成。手机：网关=本机即可，无需设代理。"
    echo "  注意：server 端必须执行  sudo ./qtun-gw.sh up --mode transparent --role server"
  else
    rule_remove 100 "from $LAN_SUBNET lookup $TABLE"
    rule_remove 60  "from $PI_IP lookup main"
    rule_remove 50  "to $SERVER_IP lookup main"
    ip route flush table "$TABLE" 2>/dev/null || true
    ipt_remove nat POSTROUTING -o "$TUN" -j MASQUERADE
    ipt_remove filter FORWARD -o "$TUN" -j ACCEPT
    ipt_remove filter FORWARD -i "$TUN" -j ACCEPT
    echo "[transparent/client] 规则已清除"
  fi
}

# ===========================================================================
# server + transparent ：qtun server 不出网，靠 OS NAT 把隧道网段 MASQUERADE 到公网
# ===========================================================================
server_transparent() { # <up|down>
  require WAN "--wan"
  if [[ "$1" == up ]]; then
    set_forward 1
    ipt_ensure nat POSTROUTING -s "$TUN_SUBNET" -o "$WAN" -j MASQUERADE
    ipt_ensure filter FORWARD -s "$TUN_SUBNET" -j ACCEPT
    ipt_ensure filter FORWARD -d "$TUN_SUBNET" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
    echo "[transparent/server] 完成。隧道网段 $TUN_SUBNET 已 NAT 出 $WAN"
  else
    ipt_remove nat POSTROUTING -s "$TUN_SUBNET" -o "$WAN" -j MASQUERADE
    ipt_remove filter FORWARD -s "$TUN_SUBNET" -j ACCEPT
    ipt_remove filter FORWARD -d "$TUN_SUBNET" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
    echo "[transparent/server] 规则已清除"
  fi
}

# ===========================================================================
show_status() {
  echo "==== 探测 ===="
  echo "WAN=$WAN  TUN=$TUN  LAN_SUBNET=$LAN_SUBNET  TUN_SUBNET=$TUN_SUBNET"
  echo "ip_forward=$(sysctl -n net.ipv4.ip_forward)"
  echo "==== nat POSTROUTING ===="; iptables -t nat -S POSTROUTING | grep -E 'MASQUERADE' || echo "(无)"
  echo "==== ip rule ===="; ip rule list
  echo "==== route table $TABLE ===="; ip route show table "$TABLE" 2>/dev/null || echo "(空)"
}

# ---------------------------------------------------------------------------
# 分发
# ---------------------------------------------------------------------------
if [[ "$ACTION" == status ]]; then
  show_status; exit 0
fi

[[ -n "$MODE" && -n "$ROLE" ]] || { echo "up/down 需指定 --mode 和 --role"; exit 1; }

case "$ROLE/$MODE" in
  client/proxy)        client_proxy "$ACTION";;
  client/transparent)  client_transparent "$ACTION";;
  server/transparent)  server_transparent "$ACTION";;
  server/proxy)        echo "[proxy/server] 无需 OS 配置：server 上的 goproxy 直接出网即可。";;
  *) echo "不支持的组合: role=$ROLE mode=$MODE"; exit 1;;
esac

if [[ "$ACTION" == up ]]; then
  echo
  echo "提示：规则未持久化。重启保留请： apt install iptables-persistent && netfilter-persistent save"
  echo "      策略路由(transparent)重启需重跑本脚本，或写进 qtun 的 systemd ExecStartPost。"
fi
