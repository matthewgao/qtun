package fileserver

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

func Start(dir string, port string) {
	go func() {
		for {
			log.Info().Str("dir", dir).Str("listen_port", port).Msg("start file http server")
			http.ListenAndServe("0.0.0.0:"+port, http.FileServer(http.Dir(dir)))
		}
	}()
}
