// Package hosttest provides test doubles for package host.
package hosttest

import (
	"sync"

	"github.com/josexy/flowlens/backend/host"
)

// RecordingBus is a host.EventBus that records every emitted event and also
// delivers it to subscribers.
type RecordingBus struct {
	bus *host.Bus

	mu     sync.Mutex
	events []host.Event
}

// NewRecordingBus returns an empty RecordingBus.
func NewRecordingBus() *RecordingBus { return &RecordingBus{bus: host.NewBus()} }

// Emit implements host.EventBus.
func (r *RecordingBus) Emit(name string, data ...any) bool {
	r.mu.Lock()
	r.events = append(r.events, host.Event{Name: name, Data: host.EventData(data)})
	r.mu.Unlock()
	return r.bus.Emit(name, data...)
}

// On implements host.EventBus.
func (r *RecordingBus) On(name string, fn func(host.Event)) (off func()) {
	return r.bus.On(name, fn)
}

// Events returns a copy of everything emitted so far.
func (r *RecordingBus) Events() []host.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]host.Event(nil), r.events...)
}

// Named returns the events emitted under name, in order.
func (r *RecordingBus) Named(name string) []host.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []host.Event
	for _, event := range r.events {
		if event.Name == name {
			matched = append(matched, event)
		}
	}
	return matched
}
