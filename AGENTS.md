# AGENTS.md

## Cursor Cloud specific instructions

### Project overview

Qtun is a Go 1.24 secure network tunneling/VPN tool using QUIC protocol. Single binary, no Docker, no containers. See `README.md` for usage examples.

### Build / Test / Lint

- **Build**: `make build` (output: `bin/qtun`)
- **Test**: `go test ./...` (tests live in `socks5/` package only)
- **Lint**: `go vet ./...` (pre-existing warnings in `socks5/request_test.go` and `socks5/socks5_test.go` about `t.Fatalf` from goroutines, plus unreachable code in `socks5.go` — these are not regressions)

### Running the application

- **Full mode** requires a TUN device (`/dev/net/tun`) and root. In Cloud Agent VMs the kernel does not support TUN, so full server/client mode will fail with "no such device".
- **Proxy-only mode** works without TUN: `./bin/qtun qt --proxyonly --socks5_port 2080`
- The binary also starts a **statsviz** monitoring dashboard at `http://localhost:6060/debug/statsviz/`.
- To create `/dev/net/tun` if missing: `sudo mkdir -p /dev/net && sudo mknod /dev/net/tun c 10 200 && sudo chmod 666 /dev/net/tun`

### Gotchas

- The SOCKS5 restart loop (`socks5 server exit, restart`) is a known behavior in the code — it retries immediately on error with no backoff. It is noisy but does not prevent the proxy from working once it binds successfully.
- `go.mod` requires Go 1.24.0; the Cloud Agent VM ships with this version pre-installed.
