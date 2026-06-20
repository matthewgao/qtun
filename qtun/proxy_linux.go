//go:build linux

package qtun

import "github.com/rs/zerolog/log"

// setSystemProxy 在 Linux 上没有统一的系统代理机制，提示用户手动设置。
func setSystemProxy(pacURL string) {
	log.Info().Str("pac", pacURL).
		Msg("set system proxy not supported on linux, please set it manually")
}

// unsetSystemProxy 在 Linux 上为空操作。
func unsetSystemProxy() {}
