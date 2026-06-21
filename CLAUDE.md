# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Qtun is a Go (1.24) secure network tunnel / VPN built on **QUIC**. A single binary runs in
either client or server mode, bridges a TUN device to the remote peer over an encrypted QUIC
stream, and bundles SOCKS5 + HTTP proxies and a PAC file server so traffic can be steered through
the tunnel. The `quic` branch (current) is the recommended implementation; `master` is an older
TCP edition.

## Commands

```bash
make build                 # -> bin/qtun (native)
make linux | windows | arm | m4   # cross-compile (output bin/qtun-<os>)

go test ./...                          # tests live in socks5/ AND qtun/ (not socks5-only)
go test ./socks5/ -run TestSOCKS5_Connect   # run a single test by name
go vet ./...                           # lint; pre-existing warnings in socks5 are not regressions

cd protocol && make build  # regenerate protocol.pb.go from protocol.proto (needs protoc + protoc-gen-go)
```

Running (needs root + a TUN device, except `--proxyonly`):

```bash
# server: TUN + SOCKS5(2080) + HTTP proxy(2081)
sudo ./bin/qtun qt --key "secret" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --server_mode
# client: TUN + PAC file server(6061), auto-sets macOS Wi-Fi proxy
sudo ./bin/qtun qt --key "secret" --remote_addrs "1.2.3.4:8080" --ip "10.4.4.3/24"
# proxy-only (no TUN, no root): just SOCKS5 + HTTP proxy
./bin/qtun qt --proxyonly --socks5_port 2080
```

`main.go` also serves a **statsviz** runtime dashboard at `http://localhost:6060/debug/statsviz/`.
See `AGENTS.md` for Cloud/TUN-less environment notes and `PERFORMANCE_*.md` for tuning rationale.
Note: README port examples are stale — trust the code defaults (socks5 2080, http proxy 2081,
PAC file server 6061, statsviz 6060).

## Transport modes — UDP (default) vs QUIC

There are **two** transports, picked by `--udp` (default `true`):

- **`--udp` (default): raw UDP + AES-GCM, WireGuard-style** (`transport/udp.go`, `UDPClient` /
  `UDPServer`). One UDP socket per side. **No transport-layer congestion control** — the tunnel is
  a dumb pipe and the inner TCP owns congestion control. This exists because QUIC (even via
  datagram) rate-limits a single flow to ~20Mbps *without loss* via its CC/pacer/32-deep send
  queue (pprof: only ~35% CPU, RTT flat → not CPU, not loss → the QUIC layer itself was throttling).
  Server keeps `routes[vip] = {udpAddr, lastPing}` learned from pings (keyed by the **UDP source
  addr**, since NAT hides the client's real addr); freshness via `udpRouteFresh` (3s), same idea as
  the QUIC path. App delegates wire+routing to the UDP transport and only implements
  `PacketSink.WriteToTun` (→ the single `tunWriter`).
- **`--udp=false`: QUIC** — the older path described below. Kept for A/B comparison. `--flow_hash`
  and `--transport_threads` only apply here.

Both reuse the same `frameDatagram`/`decodeDatagram` framing, the same protobuf `Envelope`
(`oneof ping/packet`), and the same AES-128-GCM (`--key`). The sections below describe the **QUIC**
path; the UDP path mirrors its packet semantics minus QUIC's streams/CC.

## Architecture — the cross-file data flow

The interesting logic is spread across `qtun/`, `transport/`, and `iface/`; reconstruct it from
the end-to-end packet path rather than per-file.

**Layers (Linux / macOS / Windows):**
`iface/` (TUN device) → `qtun/app.go` (orchestration + routing) → `transport/` (QUIC + framing +
crypto) ← `protocol/` (protobuf `Envelope`).

`iface/` is a platform-split abstraction: `iface.Device` interface (`iface.go`) with a `New`
factory; `iface_unix.go` (`//go:build linux || darwin`) is the songgao/water + `ifconfig`/`route`
impl, `iface_windows.go` (`//go:build windows`) is the Wintun impl (`netsh` for IP/MTU). Likewise
system-proxy setup is split into `qtun/proxy_{darwin,linux,windows}.go`. Windows needs admin rights
+ `wintun.dll` next to the exe; the registry proxy is auto-restored on exit (SIGINT/SIGTERM).
Any Windows-only dependency (wintun, x/sys/windows/registry) **must** stay inside `_windows.go`
files so Linux/macOS builds don't break.

**Egress (TUN → peer):** `App.FetchAndProcessTunPkt` (N=2×CPU worker goroutines) reads IP packets
from the TUN device. On the **client** it just calls `client.SendPacket` (round-robins across
`transport_threads` QUIC streams). On the **server** it looks up the destination VIP in the route
table and sends to a matching connection.

**Ingress (peer → TUN):** QUIC read loop → `ServerOnData` / `ClientOnData` (in `app.go`) →
protobuf unmarshal → `enqueueTunWrite` → a buffered channel consumed by a **single** `tunWriter`
goroutine. Single writer is deliberate: it preserves packet ordering (so the inner TCP doesn't see
reordering as loss) and keeps the blocking TUN syscall off the QUIC read loop.

**Routing & liveness (server side, the subtlest part):** the server keeps
`routes[clientVIP][localAddr] = lastPing` (UnixNano). The client sends a **ping every second** per
connection; each ping refreshes that timestamp. When forwarding a downstream packet the server
picks a **random connection among only the *fresh* ones** (`isRouteFresh`, window
`routeStaleTimeout = 3s`). This is how a client restart is handled without waiting for QUIC's idle
timeout: the dead connection stops pinging, goes stale within 3s, and is excluded from selection —
avoiding silent drops onto a dead stream. A 1-minute `CleanRoute` task prunes stale/closed entries.

**Wire framing (inside the QUIC stream):** every message is
`[secure uint8][dataLen uint16][payload][nonce(12B) if secure]`. The `payload` is a marshaled
protobuf `Envelope` (`oneof { MessagePing, MessagePacket }`). See `transport/{client_conn,server_conn}.go`.

**Crypto / auth:** the real authentication and confidentiality is the **shared `--key`**, used as
AES-128-GCM (`makeAES128GCM`, key = MD5 of the passphrase) in `transport/crypto.go`. A wrong key
surfaces as `ErrCiperNotMatch` on the server and drops the connection. The QUIC TLS layer is *not*
security here: the server uses a throwaway self-signed cert and the client sets
`InsecureSkipVerify` with a fixed ALPN `"quic-echo-example"`. (`makeAES256GCM` exists but is unused.)

## Gotchas

- **`config.GetInstance()` is a global singleton** returning `nil` until `config.InitConfig` runs
  (done in `main.go`'s `command`). Anything touching config depends on that init order.
- **`GrpcHandler` (transport/grpc_handler.go) has nothing to do with gRPC** — it's just the
  QUIC data callback interface (`ClientOnData` / `ServerOnData`). There is no gRPC layer.
- **QUIC tuning must stay synced between `transport/server.go` and `transport/client_conn.go`** —
  receive-window sizes, 5s `MaxIdleTimeout`, 2s `KeepAlivePeriod`. The two ends negotiate the min,
  so editing one without the other changes behavior silently. Rationale is in the in-code comments.
- The SOCKS5 / HTTP-proxy / file-server loops **restart immediately on error with no backoff**
  (noisy logs like `socks5 server exit, restart`); this is existing behavior, not a bug to "fix".
- Object pools in `transport/pool.go` (nonce, protobuf messages, buffers) are reused per packet —
  return pooled objects only after the payload is fully marshaled/copied.
