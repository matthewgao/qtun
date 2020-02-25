package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"strconv"

	"github.com/gookit/color"
	"github.com/gookit/gcli/v2"
	"github.com/gookit/gcli/v2/builtin"
	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/fileserver"
	"github.com/matthewgao/qtun/qtun"
	"github.com/matthewgao/qtun/socks5"
	"github.com/matthewgao/qtun/utils/log"
)

type CmdOpts struct {
	Key              string `default:"hello-world"`
	RemoteAddrs      string `default:"0.0.0.0:8080"`
	Listen           string `default:"0.0.0.0:8080"`
	TransportThreads int    `default:"1"`
	Ip               string `default:"10.237.0.1/16"`
	Mtu              int    `default:"1500"`
	LogLevel         string `default:"0"`
	ServerMode       bool   `default:"0"`
	Socks5Port       int
	FileServerPort   int
	FileDir          string
	NoDelay          bool
	ProxyOnly        bool
}

// options for the command
var cmdOpts = CmdOpts{}

// Command command definition
func Command() *gcli.Command {
	cmd := &gcli.Command{
		Func:    command,
		Name:    "qtun",
		Aliases: []string{"qt"},
		UseFor:  "q-tun",
		Examples: `
  Server: sudo {$binName} qt --key "hahaha" --listen "0.0.0.0:8080" --ip "10.4.4.2/24" --server_mode
  Client: sudo {$binName} qt --key "hahaha" --remote_addrs "8.8.8.80:8080" --ip "10.4.4.3/24"`,
	}

	cmd.StrOpt(&cmdOpts.Key, "key", "", "hello-world", "encrpyt key")
	cmd.StrOpt(&cmdOpts.RemoteAddrs, "remote_addrs", "", "2.2.2.2:8080", "remote server address, only for client")
	cmd.StrOpt(&cmdOpts.Listen, "listen", "", "0.0.0.0:8080", "server listen address, only for server")
	cmd.StrOpt(&cmdOpts.Ip, "ip", "", "10.237.0.1/16", "vpn vip")
	cmd.StrOpt(&cmdOpts.LogLevel, "log_level", "", "info", "log level")
	cmd.StrOpt(&cmdOpts.FileDir, "file_dir", "", "../static", "http file server directory")
	cmd.IntOpt(&cmdOpts.TransportThreads, "transport_threads", "", 1, "concurrent threads num only for client")
	cmd.IntOpt(&cmdOpts.Mtu, "mtu", "", 1500, "MTU size")
	cmd.IntOpt(&cmdOpts.Socks5Port, "socks5_port", "", 2080, "socks5 server port")
	cmd.IntOpt(&cmdOpts.FileServerPort, "file_svr_port", "", 8082, "http file server port")
	cmd.BoolOpt(&cmdOpts.ServerMode, "server_mode", "", false, "if running in server mode")
	cmd.BoolOpt(&cmdOpts.NoDelay, "nodelay", "", false, "tcp no delay")
	cmd.BoolOpt(&cmdOpts.ProxyOnly, "proxyonly", "", false, "only enable proxy")

	return cmd
}

// command running
func command(c *gcli.Command, args []string) error {
	// magentaln("dump params:")
	color.Cyan.Printf("%+v\n", cmdOpts)

	config.InitConfig(config.Config{
		Key:              cmdOpts.Key,
		RemoteAddrs:      cmdOpts.RemoteAddrs,
		Listen:           cmdOpts.Listen,
		TransportThreads: cmdOpts.TransportThreads,
		Ip:               cmdOpts.Ip,
		Mtu:              cmdOpts.Mtu,
		ServerMode:       cmdOpts.ServerMode,
		NoDelay:          cmdOpts.NoDelay,
	})

	log.InitLog(cmdOpts.LogLevel)

	if cmdOpts.ProxyOnly {
		qtunApp := qtun.NewApp()
		qtunApp.SetProxy()
		socks5.StartSocks5(fmt.Sprintf("%d", cmdOpts.Socks5Port))
		return nil
	}

	if cmdOpts.ServerMode {
		go socks5.StartSocks5(fmt.Sprintf("%d", cmdOpts.Socks5Port))
	} else {
		fileserver.Start(cmdOpts.FileDir, strconv.FormatInt(int64(cmdOpts.FileServerPort), 10))
	}

	qtunApp := qtun.NewApp()
	return qtunApp.Run()
}

func Init() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	app := gcli.NewApp()
	app.Add(builtin.GenAutoCompleteScript())
	app.Version = "1.0.0"
	app.Description = "qtun"
	// app.SetVerbose(gcli.VerbDebug)

	app.Add(Command())
	app.Run()
}

func pprof() {
	go func() {
		fmt.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

func main() {
	pprof()
	Init()
}
