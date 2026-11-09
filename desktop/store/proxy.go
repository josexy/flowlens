package store

import (
	"context"

	"github.com/josexy/flowlens/backend/host"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/runtime"
)

const (
	eventProxyStatus      = "proxy:status"
	eventProxyError       = "proxy:error"
	eventSystemProxy      = "proxy:system-proxy-status"
	eventDNSCache         = "proxy:dns-cache-status"
	jobProxyToggle        = "proxy:toggle"
	jobSystemProxy        = "proxy:system"
	jobDNSRefresh         = "proxy:dns-refresh"
	jobDNSClear           = "proxy:dns-clear"
	jobRestartCapture     = "proxy:restart-capture"
	jobUpdateHistoryAlias = "proxy:alias"
)

// ProxyBackend is the capture service as the proxy store uses it.
type ProxyBackend interface {
	Start() (proxyservice.ProxyStatus, error)
	Stop() (proxyservice.ProxyStatus, error)
	GetStatus() proxyservice.ProxyStatus
	GetSystemProxyStatus() proxyservice.SystemProxyStatus
	SetSystemProxyEnabled(enabled bool) (proxyservice.SystemProxyStatus, error)
	GetDNSCacheStatus() proxyservice.DNSCacheStatus
	ClearDNSCache() proxyservice.DNSCacheStatus
	RestartCapture(saveCurrent bool) error
	UpdateHistoryAlias(alias string)
}

// ProxyStore holds the state of the proxy listener.
type ProxyStore struct {
	rt      Runtime
	backend ProxyBackend

	Status      proxyservice.ProxyStatus
	SystemProxy proxyservice.SystemProxyStatus
	DNSCache    proxyservice.DNSCacheStatus
	LastError   string
	// Toggling is true while the proxy is starting or stopping.
	Toggling bool
	// SystemProxyApplying is true while the system proxy is changing.
	SystemProxyApplying bool
	// Restarting is true while a capture restart runs.
	Restarting bool

	offs []func()
}

// NewProxyStore returns a store with no status.
func NewProxyStore(rt Runtime, backend ProxyBackend) *ProxyStore {
	return &ProxyStore{rt: rt, backend: backend}
}

// Initialize reads the current state and follows later changes.
func (s *ProxyStore) Initialize(bus host.EventBus) {
	s.Status = s.backend.GetStatus()
	s.SystemProxy = s.backend.GetSystemProxyStatus()
	s.DNSCache = s.backend.GetDNSCacheStatus()
	s.Cleanup()
	s.offs = []func(){
		bus.On(eventProxyStatus, func(event host.Event) {
			if status, ok := event.Data.(proxyservice.ProxyStatus); ok {
				s.rt.Dispatcher.Post(func() { s.Status = status })
			}
		}),
		bus.On(eventProxyError, func(event host.Event) {
			message, _ := event.Data.(string)
			s.rt.Dispatcher.Post(func() { s.LastError = message })
		}),
		bus.On(eventSystemProxy, func(event host.Event) {
			if status, ok := event.Data.(proxyservice.SystemProxyStatus); ok {
				s.rt.Dispatcher.Post(func() { s.SystemProxy = status })
			}
		}),
		bus.On(eventDNSCache, func(event host.Event) {
			if status, ok := event.Data.(proxyservice.DNSCacheStatus); ok {
				s.rt.Dispatcher.Post(func() { s.DNSCache = status })
			}
		}),
	}
}

// Cleanup stops following changes.
func (s *ProxyStore) Cleanup() {
	for _, off := range s.offs {
		off()
	}
	s.offs = nil
}

// Toggle starts a stopped proxy or stops a running one. It reports false while
// another toggle runs. done receives whether the proxy is now running.
func (s *ProxyStore) Toggle(done func(running bool, err error)) bool {
	if s.Toggling {
		return false
	}
	s.Toggling = true
	s.LastError = ""
	stopping := s.Status.Running
	runtime.Run(s.rt.Jobs, jobProxyToggle, func(context.Context) (proxyservice.ProxyStatus, error) {
		if stopping {
			return s.backend.Stop()
		}
		return s.backend.Start()
	}, func(status proxyservice.ProxyStatus, err error) {
		s.Toggling = false
		if err != nil {
			s.LastError = err.Error()
			if done != nil {
				done(s.Status.Running, err)
			}
			return
		}
		s.Status = status
		if done != nil {
			done(status.Running, nil)
		}
	})
	return true
}

// Restart stops and starts the proxy so a changed configuration takes effect.
func (s *ProxyStore) Restart(done func(error)) {
	runtime.Run(s.rt.Jobs, jobProxyToggle, func(context.Context) (proxyservice.ProxyStatus, error) {
		if _, err := s.backend.Stop(); err != nil {
			return proxyservice.ProxyStatus{}, err
		}
		return s.backend.Start()
	}, func(status proxyservice.ProxyStatus, err error) {
		if err == nil {
			s.Status = status
		} else {
			s.LastError = err.Error()
		}
		if done != nil {
			done(err)
		}
	})
}

// SetSystemProxyEnabled turns the managed system proxy on or off. It reports
// false while another change runs.
func (s *ProxyStore) SetSystemProxyEnabled(enabled bool, done func(proxyservice.SystemProxyStatus, error)) bool {
	if s.SystemProxyApplying {
		return false
	}
	s.SystemProxyApplying = true
	runtime.Run(s.rt.Jobs, jobSystemProxy, func(context.Context) (proxyservice.SystemProxyStatus, error) {
		return s.backend.SetSystemProxyEnabled(enabled)
	}, func(status proxyservice.SystemProxyStatus, err error) {
		s.SystemProxyApplying = false
		if err == nil {
			s.SystemProxy = status
		}
		if done != nil {
			done(status, err)
		}
	})
	return true
}

// RefreshDNSCache reads the DNS cache status.
func (s *ProxyStore) RefreshDNSCache(done func(proxyservice.DNSCacheStatus)) {
	runtime.Run(s.rt.Jobs, jobDNSRefresh, func(context.Context) (proxyservice.DNSCacheStatus, error) {
		return s.backend.GetDNSCacheStatus(), nil
	}, func(status proxyservice.DNSCacheStatus, err error) {
		if err != nil {
			return
		}
		s.DNSCache = status
		if done != nil {
			done(status)
		}
	})
}

// ClearDNSCache removes cached records.
func (s *ProxyStore) ClearDNSCache(done func()) {
	runtime.Run(s.rt.Jobs, jobDNSClear, func(context.Context) (proxyservice.DNSCacheStatus, error) {
		return s.backend.ClearDNSCache(), nil
	}, func(status proxyservice.DNSCacheStatus, err error) {
		if err == nil {
			s.DNSCache = status
		}
		if done != nil {
			done()
		}
	})
}

// RestartCapture starts a new session, saving or discarding the current one.
func (s *ProxyStore) RestartCapture(saveCurrent bool, done func(error)) bool {
	if s.Restarting {
		return false
	}
	s.Restarting = true
	runtime.Run(s.rt.Jobs, jobRestartCapture, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.RestartCapture(saveCurrent)
	}, func(_ struct{}, err error) {
		s.Restarting = false
		if done != nil {
			done(err)
		}
	})
	return true
}

// UpdateHistoryAlias names the current session.
func (s *ProxyStore) UpdateHistoryAlias(alias string, done func(error)) {
	runtime.Run(s.rt.Jobs, jobUpdateHistoryAlias, func(context.Context) (struct{}, error) {
		s.backend.UpdateHistoryAlias(alias)
		return struct{}{}, nil
	}, func(_ struct{}, err error) {
		if err == nil {
			s.Status.CaptureAlias = alias
		}
		if done != nil {
			done(err)
		}
	})
}
