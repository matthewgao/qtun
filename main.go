package main

import (
	"log"
	"os"

	"github.com/matthewgao/meshbird/config"
	"github.com/matthewgao/meshbird/meshbird"
	"github.com/miolini/cliconfig"
	"github.com/urfave/cli"
)

func main() {
	var cfg config.Config
	app := cli.NewApp()
	app.Name = "meshbird"
	app.Flags = cliconfig.Fill(&cfg, "MESHBIRD_")
	app.Action = func(ctx *cli.Context) error {
		log.Printf("config: %#v", cfg)
		meshbirdApp := meshbird.NewApp(cfg)
		return meshbirdApp.Run()
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Printf("app run err: %s", err)
	}
}
