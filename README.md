# Qtun on Quic v1.0

## About

a C-S safe tunnel, support multi-ip pipeline
* `master` branch is a tcp implementation edtion.
* `quic` branch is a quic implementation edition. RECOMMAND!

## How to run

* Go 1.19 needed

### Server
会自动启动两个服务一个Tun, 一个Socks5 Server 监听在127.0.0.1:2080
```
Server:
cd bin/
sudo ./qtun qt --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --server_mode
```

### Client:
启动后配置自动代理 http://127.0.0.1:8082/proxy.pac， 如果不是在工程bin目录中启动，需要自己设置http file server地址
```
cd bin/
sudo ./qtun qt --key "hahaha" --remote_addrs "8.8.8.80:8080" --ip "10.4.4.3/24"
```

## Help

```
Qtun (Version: 1.0.0)
Usage:
  ./qtun [Global Options...] {command} [--option ...] [argument ...]

Global Options:
      --verbose     Set error reporting level(quiet 0 - 4 debug)
      --no-color    Disable color when outputting message
  -h, --help        Display the help information
  -V, --version     Display app version information

Available Commands:
  qtun        qiuqiu-tun (alias: qt)
gen
  gen:ac       Generate auto complete scripts for current application (alias: genac,gen-ac)

  help         Display help information

Use "./qtun {COMMAND} -h" for more information about a command
```

```
Q-tun

Name: qtun (alias: qt)
Usage: ./qtun [Global Options...] qtun [--option ...] [argument ...]

Global Options:
      --verbose     Set error reporting level(quiet 0 - 4 debug)
      --no-color    Disable color when outputting message
  -h, --help        Display this help information

Options:
      --file_dir string
        Http file server directory (default ../static)
      --file_svr_port int
        Http file server port (default 8082)
      --ip string
        Vpn vip (default 10.237.0.1/16)
      --key string
        Encrpyt key (default hello-world)
      --listen string
        Server listen address, only for server (default 0.0.0.0:8080)
      --log_level string
        Log level (default info)
      --mtu int
        MTU size (default 1500)
      --remote_addrs string
        Remote server address, only for client (default 2.2.2.2:8080)
      --server_mode
        If running in server mode
      --socks5_port int
        Socks5 server port (default 2080)
      --transport_threads int
        Concurrent threads num only for client (default 1) 
Examples:
  Server: sudo ./qtun qt --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --server_mode
  Client: sudo ./qtun qt --key "hahaha" --remote_addrs "8.8.8.80:8080" --ip "10.4.4.3/24"
```

## Misc
* You can set goproxy to accelerate module download progress `go env -w GOPROXY=https://goproxy.io,direct`
* Git with proxy `git config --global http.proxy 'socks5://10.4.4.2:2080'`

## Env

* MacOS 支持
* Linux 支持
* Windows 支持（基于 Wintun，需管理员权限）

### Windows 说明

* 隧道模式基于 [Wintun](https://www.wintun.net) 驱动（Layer 3 TUN）。运行前需从
  wintun.net 下载对应架构的 `wintun.dll`，放到 `qtun-win.exe` 同目录。
* 创建网卡、配置 IP/路由需要**管理员权限**（请以管理员身份运行）。
* 客户端模式会通过注册表把系统代理设为自动配置（PAC，`http://127.0.0.1:6061/proxy.pac`），
  进程退出（Ctrl+C / 终止）时自动还原。
* `--proxyonly` 模式仅启动 SOCKS5/HTTP 代理，不需要 TUN、不需要管理员权限。
