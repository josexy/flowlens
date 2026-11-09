// Package host defines what backend services need from the application that
// runs them, so that services stay independent of any UI toolkit.
package host

import "sync"

// Event is a named notification published by a service.
type Event struct {
	Name string
	Data any
}

// EventBus delivers named events between services and the interface.
type EventBus interface {
	// Emit delivers an event to the current listeners of name, in the order
	// they subscribed, and reports whether any received it. No data gives a
	// nil Event.Data, one value is that value, and several are a []any.
	Emit(name string, data ...any) bool
	// On subscribes fn to name and returns a function that unsubscribes it.
	On(name string, fn func(Event)) (off func())
}

type listener struct {
	id uint64
	fn func(Event)
}

// Bus is the in-process EventBus. Emit runs listeners synchronously on the
// caller's goroutine, so one publisher's events reach a listener in order.
// Listeners must not block; the interface queues events for its main thread.
type Bus struct {
	mu        sync.RWMutex
	next      uint64
	listeners map[string][]listener
}

// NewBus returns an empty Bus.
func NewBus() *Bus {
	return &Bus{listeners: make(map[string][]listener)}
}

// Emit implements EventBus.
func (b *Bus) Emit(name string, data ...any) bool {
	b.mu.RLock()
	subscribed := b.listeners[name]
	b.mu.RUnlock()
	if len(subscribed) == 0 {
		return false
	}
	event := Event{Name: name, Data: EventData(data)}
	for _, l := range subscribed {
		l.fn(event)
	}
	return true
}

// On implements EventBus.
func (b *Bus) On(name string, fn func(Event)) (off func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	// Copy on write: Emit iterates a snapshot without holding the lock.
	current := b.listeners[name]
	updated := make([]listener, len(current), len(current)+1)
	copy(updated, current)
	b.listeners[name] = append(updated, listener{id: id, fn: fn})
	var once sync.Once
	return func() {
		once.Do(func() { b.remove(name, id) })
	}
}

func (b *Bus) remove(name string, id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.listeners[name]
	updated := make([]listener, 0, len(current))
	for _, l := range current {
		if l.id != id {
			updated = append(updated, l)
		}
	}
	if len(updated) == 0 {
		delete(b.listeners, name)
		return
	}
	b.listeners[name] = updated
}

// EventData folds the variadic arguments of Emit into Event.Data.
func EventData(data []any) any {
	switch len(data) {
	case 0:
		return nil
	case 1:
		return data[0]
	default:
		return append([]any(nil), data...)
	}
}
