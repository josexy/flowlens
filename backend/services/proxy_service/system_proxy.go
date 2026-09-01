package proxyservice

import (
	"strings"

	"github.com/josexy/flowlens/backend/pkg/systemproxy"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func resolveEffectiveUpstreamProxy(cfg *settingservice.ProxyConfig) string {
	return resolveEffectiveUpstreamProxyWithSystem(cfg, resolveSystemUpstreamProxy())
}

func resolveEffectiveUpstreamProxyWithSystem(cfg *settingservice.ProxyConfig, systemProxy string) string {
	if cfg == nil {
		return ""
	}
	switch cfg.UpstreamMode {
	case settingservice.UpstreamProxyModeNone:
		return ""
	case settingservice.UpstreamProxyModeCustom:
		return strings.TrimSpace(cfg.UpstreamProxy)
	case settingservice.UpstreamProxyModeSystem:
		return strings.TrimSpace(systemProxy)
	default:
		if proxy := strings.TrimSpace(cfg.UpstreamProxy); proxy != "" {
			return proxy
		}
		return strings.TrimSpace(systemProxy)
	}
}

func resolveSystemUpstreamProxy() string {
	return systemproxy.Get()
}
