package fileserver

import (
	"github.com/rs/zerolog/log"
	"net/http"
)

func Start(dir string, port string) {
	go func() {
		for {
			log.Info().Str("dir", dir).Msg("start file http server")
			http.ListenAndServe("127.0.0.1:"+port, http.FileServer(http.Dir(dir)))
		}
	}()
}
