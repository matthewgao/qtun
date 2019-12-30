package log

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func InitLog(loglevel string) {

	// zerolog.TimeFieldFormat = zerolog.TimestampFieldName
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	switch loglevel {
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Debug().Msg("This message appears only when log level set to Debug")
	log.Info().Msg("This message appears when log level set to Debug or Info")
}
