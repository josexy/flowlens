package store

import (
	"sync/atomic"
	"time"

	"github.com/josexy/flowlens/backend/host"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/runtime"
)

// Clock schedules work on the main thread.
type Clock interface {
	// After runs fn on the main thread once d has passed, unless stop is called
	// first.
	After(d time.Duration, fn func()) (stop func())
}

type dispatcherClock struct{ dispatcher *runtime.Dispatcher }

func (c dispatcherClock) After(d time.Duration, fn func()) func() {
	var stopped atomic.Bool
	timer := time.AfterFunc(d, func() {
		c.dispatcher.Post(func() {
			if !stopped.Load() {
				fn()
			}
		})
	})
	return func() {
		stopped.Store(true)
		timer.Stop()
	}
}

type eventListener struct {
	onData    func(any)
	onDropped func(uint64)
}

// EventRouter delivers batched backend events to their listeners on the main
// thread, keeping publication order.
type EventRouter struct {
	listeners map[string][]*eventListener
}

// NewEventRouter returns a router without listeners.
func NewEventRouter() *EventRouter {
	return &EventRouter{listeners: make(map[string][]*eventListener)}
}

// Subscribe adds a listener for name. onDropped, if set, hears how many events
// of that name were lost, so the listener can recover from a snapshot.
func (r *EventRouter) Subscribe(name string, onData func(any), onDropped func(uint64)) (off func()) {
	listener := &eventListener{onData: onData, onDropped: onDropped}
	r.listeners[name] = append(r.listeners[name], listener)
	active := true
	return func() {
		if !active {
			return
		}
		active = false
		current := r.listeners[name]
		next := make([]*eventListener, 0, len(current))
		for _, candidate := range current {
			if candidate != listener {
				next = append(next, candidate)
			}
		}
		if len(next) == 0 {
			delete(r.listeners, name)
			return
		}
		r.listeners[name] = next
	}
}

// ListenerCount returns the number of subscribed listeners.
func (r *EventRouter) ListenerCount() int {
	count := 0
	for _, listeners := range r.listeners {
		count += len(listeners)
	}
	return count
}

// Dispatch delivers a batch: events in order, then drop counts.
func (r *EventRouter) Dispatch(batch proxyservice.FrontendEventBatch) {
	for _, event := range batch.Events {
		for _, listener := range r.listeners[event.Name] {
			listener.onData(event.Data)
		}
	}
	for name, count := range batch.Dropped {
		if count == 0 {
			continue
		}
		for _, listener := range r.listeners[name] {
			if listener.onDropped != nil {
				listener.onDropped(count)
			}
		}
	}
}

// Connect feeds batches from bus through post, which must run its function on
// the main thread. One batch causes one redraw.
func (r *EventRouter) Connect(bus host.EventBus, post func(func())) (off func()) {
	return bus.On(proxyservice.BatchedAppEventName, func(event host.Event) {
		batch, ok := event.Data.(proxyservice.FrontendEventBatch)
		if !ok {
			return
		}
		post(func() { r.Dispatch(batch) })
	})
}
