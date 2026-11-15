// Package bustest provides a recording event bus for tests.
package bustest

import (
	"sync"

	"github.com/josexy/flowlens/internal/serviceruntime"
)

// RecordingBus is a serviceruntime.EventBus that records every emitted event and also
// delivers it to subscribers.
type RecordingBus struct {
	bus *serviceruntime.Bus

	mu     sync.Mutex
	events []serviceruntime.Event
}

// NewRecordingBus returns an empty RecordingBus.
func NewRecordingBus() *RecordingBus { return &RecordingBus{bus: serviceruntime.NewBus()} }

// Emit implements serviceruntime.EventBus.
func (r *RecordingBus) Emit(name string, data ...any) bool {
	r.mu.Lock()
	r.events = append(r.events, serviceruntime.Event{Name: name, Data: serviceruntime.EventData(data)})
	r.mu.Unlock()
	return r.bus.Emit(name, data...)
}

// On implements serviceruntime.EventBus.
func (r *RecordingBus) On(name string, fn func(serviceruntime.Event)) (off func()) {
	return r.bus.On(name, fn)
}

// Events returns a copy of everything emitted so far.
func (r *RecordingBus) Events() []serviceruntime.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]serviceruntime.Event(nil), r.events...)
}

// Named returns the events emitted under name, in order.
func (r *RecordingBus) Named(name string) []serviceruntime.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []serviceruntime.Event
	for _, event := range r.events {
		if event.Name == name {
			matched = append(matched, event)
		}
	}
	return matched
}
