package store

import (
	"context"
	"errors"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

const (
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
	Restart() (proxyservice.ProxyStatus, error)
	SetRecordingPaused(paused bool) (proxyservice.ProxyStatus, error)
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
	// Toggling guards listener and recording operations as a single job.
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
func (s *ProxyStore) Initialize(events *proxyservice.Events) {
	s.Status = s.backend.GetStatus()
	s.SystemProxy = s.backend.GetSystemProxyStatus()
	s.DNSCache = s.backend.GetDNSCacheStatus()
	s.Cleanup()
	post := s.rt.Dispatcher.Post
	s.offs = []func(){
		events.Status.Subscribe(func(status proxyservice.ProxyStatus) {
			post(func() { s.applyStatus(status) })
		}),
		events.SystemProxy.Subscribe(func(status proxyservice.SystemProxyStatus) {
			post(func() { s.SystemProxy = status })
		}),
		events.DNSCache.Subscribe(func(status proxyservice.DNSCacheStatus) {
			post(func() { s.DNSCache = status })
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
	stopping := s.Status.Running
	return s.runProxyOperation(func() (proxyservice.ProxyStatus, error) {
		if stopping {
			return s.backend.Stop()
		}
		return s.backend.Start()
	}, func(status proxyservice.ProxyStatus, err error) {
		if done != nil {
			done(status.Running, err)
		}
	})
}

func (s *ProxyStore) applyStatus(status proxyservice.ProxyStatus) {
	if status.Revision >= s.Status.Revision {
		s.Status = status
	}
}

func (s *ProxyStore) runProxyOperation(work func() (proxyservice.ProxyStatus, error), done func(proxyservice.ProxyStatus, error)) bool {
	if s.Toggling {
		return false
	}
	s.Toggling = true
	s.LastError = ""
	runtime.Run(s.rt.Jobs, jobProxyToggle, func(context.Context) (proxyservice.ProxyStatus, error) {
		status, err := work()
		if err != nil {
			status = s.backend.GetStatus()
		}
		return status, err
	}, func(status proxyservice.ProxyStatus, err error) {
		s.Toggling = false
		s.applyStatus(status)
		if err != nil {
			s.LastError = err.Error()
		}
		if done != nil {
			done(s.Status, err)
		}
	})
	return true
}

// Restart stops and starts the proxy so a changed configuration takes effect.
func (s *ProxyStore) Restart(done func(error)) {
	if !s.runProxyOperation(s.backend.Restart, func(_ proxyservice.ProxyStatus, err error) {
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("proxy operation already in progress"))
	}
}

// SetRecordingPaused changes admission without touching the listener or session.
func (s *ProxyStore) SetRecordingPaused(paused bool, done func(proxyservice.ProxyStatus, error)) bool {
	if !s.Status.Running {
		return false
	}
	return s.runProxyOperation(func() (proxyservice.ProxyStatus, error) {
		return s.backend.SetRecordingPaused(paused)
	}, done)
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
