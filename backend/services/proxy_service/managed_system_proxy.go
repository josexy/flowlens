package proxyservice

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/josexy/flowlens/backend/pkg/systemproxy"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

const systemProxyFallbackHost = "127.0.0.1"

type systemProxyController interface {
	State() systemproxy.ControllerState
	Supports(systemproxy.Mode) bool
	Apply(systemproxy.Endpoint, string) error
	Restore() error
}

func (s *ProxyService) GetSystemProxyStatus() SystemProxyStatus {
	if s.systemProxy == nil {
		return SystemProxyStatus{}
	}
	state := s.systemProxy.State()
	mode := settingservice.ProxyMode(state.Endpoint.Mode)
	if !state.Active && s.settingService != nil {
		if cfg, err := s.settingService.GetProxyConfig(); err == nil && cfg != nil {
			mode = cfg.Mode
		}
	}
	mode = normalizeSystemProxyMode(mode)
	return systemProxyStatusFromController(state, mode, s.systemProxy.Supports(systemproxy.Mode(mode)))
}

func (s *ProxyService) SetSystemProxyEnabled(enabled bool) (SystemProxyStatus, error) {
	s.systemProxyOpsMu.Lock()
	defer s.systemProxyOpsMu.Unlock()
	if s.systemProxy == nil {
		return SystemProxyStatus{}, systemproxy.ErrUnsupported
	}
	if s.systemProxyShuttingDown {
		return s.GetSystemProxyStatus(), errors.New("system proxy cannot be changed while FlowLens is shutting down")
	}

	var err error
	if enabled {
		cfg, cfgErr := s.getProxyConfig()
		if cfgErr != nil {
			return s.GetSystemProxyStatus(), cfgErr
		}
		endpoint, endpointErr := systemProxyEndpointFromConfig(cfg)
		if endpointErr != nil {
			return s.GetSystemProxyStatus(), endpointErr
		}
		originalUpstreamProxy := systemproxy.Get()
		if proxyURLMatchesEndpoint(originalUpstreamProxy, endpoint) {
			originalUpstreamProxy = ""
		}
		err = s.systemProxy.Apply(endpoint, originalUpstreamProxy)
	} else {
		err = s.systemProxy.Restore()
	}

	status := s.GetSystemProxyStatus()
	s.emitSystemProxyStatus(status)
	return status, err
}

func (s *ProxyService) syncManagedSystemProxy(cfg *settingservice.ProxyConfig) error {
	s.systemProxyOpsMu.Lock()
	defer s.systemProxyOpsMu.Unlock()
	if s.systemProxy == nil {
		return nil
	}
	if s.systemProxyShuttingDown {
		return nil
	}
	state := s.systemProxy.State()
	if !state.Active {
		s.emitSystemProxyStatus(s.GetSystemProxyStatus())
		return nil
	}
	endpoint, err := systemProxyEndpointFromConfig(cfg)
	if err != nil {
		return err
	}
	if !s.systemProxy.Supports(endpoint.Mode) {
		err = s.systemProxy.Restore()
		s.emitSystemProxyStatus(s.GetSystemProxyStatus())
		return err
	}
	err = s.systemProxy.Apply(endpoint, state.OriginalUpstreamProxy)
	s.emitSystemProxyStatus(s.GetSystemProxyStatus())
	return err
}

func (s *ProxyService) restoreManagedSystemProxy() error {
	s.systemProxyOpsMu.Lock()
	defer s.systemProxyOpsMu.Unlock()
	s.systemProxyShuttingDown = true
	if s.systemProxy == nil {
		return nil
	}
	err := s.systemProxy.Restore()
	if s.app != nil {
		s.emitSystemProxyStatus(s.GetSystemProxyStatus())
	}
	return err
}

func (s *ProxyService) emitSystemProxyStatus(status SystemProxyStatus) {
	if s.app != nil {
		_ = s.app.Event.Emit(systemProxyStatusEventName, status)
	}
}

func (s *ProxyService) resolveMITMUpstreamProxy(cfg *settingservice.ProxyConfig) string {
	if s.systemProxy != nil {
		state := s.systemProxy.State()
		if state.Active {
			return resolveEffectiveUpstreamProxyWithSystem(cfg, state.OriginalUpstreamProxy)
		}
	}
	return resolveEffectiveUpstreamProxy(cfg)
}

func systemProxyEndpointFromConfig(cfg *settingservice.ProxyConfig) (systemproxy.Endpoint, error) {
	if cfg == nil {
		return systemproxy.Endpoint{}, errors.New("proxy config cannot be nil")
	}
	host := strings.TrimSpace(cfg.Host)
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = systemProxyFallbackHost
	}
	if host == "" {
		host = systemProxyFallbackHost
	}
	return systemproxy.Endpoint{
		Mode: systemproxy.Mode(cfg.Mode),
		Host: host,
		Port: cfg.Port,
	}, nil
}

func systemProxyStatusFromController(
	state systemproxy.ControllerState,
	mode settingservice.ProxyMode,
	modeSupported bool,
) SystemProxyStatus {
	mode = normalizeSystemProxyMode(mode)
	return SystemProxyStatus{
		Supported:     state.Supported,
		ModeSupported: modeSupported,
		Active:        state.Active,
		Address:       state.Endpoint.Address(),
		Mode:          mode,
	}
}

func normalizeSystemProxyMode(mode settingservice.ProxyMode) settingservice.ProxyMode {
	if mode != settingservice.ProxyModeHTTP && mode != settingservice.ProxyModeSOCKS5 {
		return settingservice.ProxyModeHTTP
	}
	return mode
}

func proxyURLMatchesEndpoint(rawProxy string, endpoint systemproxy.Endpoint) bool {
	rawProxy = strings.TrimSpace(rawProxy)
	if rawProxy == "" {
		return false
	}
	parsed, err := url.Parse(rawProxy)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), endpoint.Host) && port == endpoint.Port
}
