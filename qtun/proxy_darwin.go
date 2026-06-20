//go:build darwin

package qtun

import (
	"os/exec"

	"github.com/rs/zerolog/log"
)

// setSystemProxy 在 macOS 上把 Wi-Fi 的自动代理 URL 指向 PAC 文件。
func setSystemProxy(pacURL string) {
	cmd := exec.Command("networksetup", "-setautoproxyurl", "Wi-Fi", pacURL)
	log.Info().Str("cmd", cmd.String()).Msg("set system proxy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("cmd_output", string(output)).
			Msg("set system proxy fail")
	}
}

// unsetSystemProxy 关闭 macOS Wi-Fi 的自动代理。
func unsetSystemProxy() {
	cmd := exec.Command("networksetup", "-setautoproxystate", "Wi-Fi", "off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("cmd_output", string(output)).
			Msg("unset system proxy fail")
	}
}
