package main

import (
	"fmt"
	"runtime"

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
}

// options for the command
var cmdOpts = CmdOpts{}

// Command command definition
func Command() *gcli.Command {
	cmd := &gcli.Command{
		Func:    command,
		Name:    "qtun",
		Aliases: []string{"qt"},
		UseFor:  "run pic evaluation in batch",
		Examples: `
  {$binName} {$cmd} --filename all.data --workernum 20 --cmd "python run.py"`,
	}

	cmd.StrOpt(&cmdOpts.Key, "key", "", "hello-world", "encrpyt key")
	cmd.StrOpt(&cmdOpts.RemoteAddrs, "remote_addrs", "", "2.2.2.2:8080", "remote server address, only for client")
	cmd.StrOpt(&cmdOpts.Listen, "listen", "", "0.0.0.0:8080", "server listen address, only for server")
	cmd.StrOpt(&cmdOpts.Ip, "ip", "", "10.237.0.1/16", "vpn vip")
	cmd.StrOpt(&cmdOpts.LogLevel, "log_level", "", "info", "log level")
	cmd.IntOpt(&cmdOpts.TransportThreads, "transport_threads", "", 1, "concurrent threads num only for client")
	cmd.IntOpt(&cmdOpts.Mtu, "mtu", "", 1500, "MTU size")
	cmd.IntOpt(&cmdOpts.Socks5Port, "socks5_port", "", 2080, "socks5 server port")
	cmd.BoolOpt(&cmdOpts.ServerMode, "server_mode", "", false, "if running in server mode")

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
	})

	log.InitLog(cmdOpts.LogLevel)

	if cmdOpts.ServerMode {
		go socks5.StartSocks5(fmt.Sprintf("%d", cmdOpts.Socks5Port))
	} else {
		fileserver.Start("../static", "8082")
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

func main() {
	Init()
}
