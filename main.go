package main

import (
	"log"
	"net/http"
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

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	err := app.Run(os.Args)
	if err != nil {
		log.Printf("app run err: %s", err)
	}
}
