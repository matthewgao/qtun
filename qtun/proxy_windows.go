//go:build windows

package qtun

import (
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// inetSettings 是当前用户的 Internet 设置注册表路径（系统代理就存在这里）。
const inetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// setSystemProxy 在 Windows 上通过注册表把系统代理设为自动配置（PAC），
// 对标 macOS 的 networksetup -setautoproxyurl。仅影响当前用户（HKCU）。
func setSystemProxy(pacURL string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettings, registry.SET_VALUE)
	if err != nil {
		log.Error().Err(err).Msg("open internet settings registry fail")
		return
	}
	defer k.Close()

	if err := k.SetStringValue("AutoConfigURL", pacURL); err != nil {
		log.Error().Err(err).Msg("set AutoConfigURL fail")
		return
	}
	log.Info().Str("pac", pacURL).Msg("set system proxy (AutoConfigURL)")
	notifyProxyChange()
}

// unsetSystemProxy 删除自动配置 URL 并关闭代理开关，还原系统代理。
func unsetSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettings, registry.SET_VALUE)
	if err != nil {
		log.Error().Err(err).Msg("open internet settings registry fail")
		return
	}
	defer k.Close()

	// 值可能本就不存在，忽略对应错误。
	_ = k.DeleteValue("AutoConfigURL")
	_ = k.SetDWordValue("ProxyEnable", 0)
	notifyProxyChange()
	log.Info().Msg("unset system proxy")
}

// notifyProxyChange 通知 WinINet 代理设置已变更并刷新，使更改无需重启应用即可生效。
// best-effort：失败不影响主流程。
func notifyProxyChange() {
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	wininet := windows.NewLazySystemDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	setOption.Call(0, internetOptionSettingsChanged, 0, 0)
	setOption.Call(0, internetOptionRefresh, 0, 0)
}
