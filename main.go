package main

import (
	"log"
	"os"

	_ "net/http/pprof"

	"github.com/matthewgao/qtun/config"
	"github.com/matthewgao/qtun/qtun"
	"github.com/miolini/cliconfig"
	"github.com/urfave/cli"
)

func main() {
	var cfg config.Config
	app := cli.NewApp()
	app.Name = "qtun"
	app.Flags = cliconfig.Fill(&cfg, "QTUN_")
	app.Action = func(ctx *cli.Context) error {
		log.Printf("config: %#v", cfg)
		qtunApp := qtun.NewApp(cfg)
		return qtunApp.Run()
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Printf("app run err: %s", err)
	}
}
