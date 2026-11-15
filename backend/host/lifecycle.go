package host

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Startable is a service with startup work.
type Startable interface {
	ServiceStartup(ctx context.Context) error
}

// Stoppable is a service with shutdown work.
type Stoppable interface {
	ServiceShutdown() error
}

// Lifecycle starts services in registration order and stops them in reverse.
// Stop cancels the context given to services before it stops any of them.
type Lifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	services []any
	started  []any
	stopped  bool
}

// NewLifecycle returns a Lifecycle whose service context derives from parent.
func NewLifecycle(parent context.Context) *Lifecycle {
	ctx, cancel := context.WithCancel(parent)
	return &Lifecycle{ctx: ctx, cancel: cancel}
}

// Context returns the context given to services.
func (l *Lifecycle) Context() context.Context { return l.ctx }

// Register appends services. A service may implement Startable, Stoppable,
// both, or neither.
func (l *Lifecycle) Register(services ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.services = append(l.services, services...)
}

// Start runs each service's startup in registration order. When one fails,
// the services already started are stopped and its error is returned.
func (l *Lifecycle) Start() error {
	l.mu.Lock()
	services := append([]any(nil), l.services...)
	l.mu.Unlock()
	for index, service := range services {
		l.mu.Lock()
		l.started = append(l.started, service)
		l.mu.Unlock()
		startable, ok := service.(Startable)
		if !ok {
			continue
		}
		if err := startable.ServiceStartup(l.ctx); err != nil {
			l.mu.Lock()
			l.started = l.started[:len(l.started)-1]
			l.mu.Unlock()
			startErr := fmt.Errorf("start service %d (%T): %w", index, service, err)
			return errors.Join(startErr, l.Stop())
		}
	}
	return nil
}

// Stop cancels the service context and then shuts down the started services
// in reverse order. Only the first call does any work.
func (l *Lifecycle) Stop() error {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return nil
	}
	l.stopped = true
	started := l.started
	l.started = nil
	l.mu.Unlock()

	l.cancel()
	var errs []error
	for _, s := range slices.Backward(started) {
		stoppable, ok := s.(Stoppable)
		if !ok {
			continue
		}
		if err := stoppable.ServiceShutdown(); err != nil {
			errs = append(errs, fmt.Errorf("stop service %T: %w", s, err))
		}
	}
	return errors.Join(errs...)
}
