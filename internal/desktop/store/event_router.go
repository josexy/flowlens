package store

import (
	"maps"
	"slices"
	"sync/atomic"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
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

// listeners holds the main-thread listeners of one kind of value. Removing a
// listener replaces the slice, so delivery can iterate a snapshot while a
// listener unsubscribes.
type listeners[T any] struct {
	next  uint64
	items []listenerEntry[T]
}

type listenerEntry[T any] struct {
	id uint64
	fn func(T)
}

func (l *listeners[T]) add(fn func(T)) (off func()) {
	l.next++
	id := l.next
	l.items = append(l.items[:len(l.items):len(l.items)], listenerEntry[T]{id: id, fn: fn})
	active := true
	return func() {
		if !active {
			return
		}
		active = false
		next := make([]listenerEntry[T], 0, len(l.items))
		for _, item := range l.items {
			if item.id != id {
				next = append(next, item)
			}
		}
		l.items = next
	}
}

func (l *listeners[T]) call(value T) {
	for _, item := range l.items {
		item.fn(value)
	}
}

// EventRouter delivers live batches to typed listeners on the main thread,
// keeping publication order. All methods run on the main thread.
type EventRouter struct {
	entries     listeners[*logic.TrafficEntry]
	patches     listeners[*logic.TrafficPatch]
	liveUpdates listeners[proxyservice.TrafficLiveUpdate]
	resets      listeners[uint64]
	dropped     map[proxyservice.LiveEventKind]*listeners[uint64]
}

// NewEventRouter returns a router without listeners.
func NewEventRouter() *EventRouter {
	return &EventRouter{dropped: make(map[proxyservice.LiveEventKind]*listeners[uint64])}
}

// OnTrafficEntry hears complete live capture entries.
func (r *EventRouter) OnTrafficEntry(fn func(*logic.TrafficEntry)) (off func()) {
	return r.entries.add(fn)
}

// OnTrafficPatch hears updates of live capture entries.
func (r *EventRouter) OnTrafficPatch(fn func(*logic.TrafficPatch)) (off func()) {
	return r.patches.add(fn)
}

// OnTrafficLiveUpdate hears SSE and WebSocket data of the entry in the detail
// panel.
func (r *EventRouter) OnTrafficLiveUpdate(fn func(proxyservice.TrafficLiveUpdate)) (off func()) {
	return r.liveUpdates.add(fn)
}

// OnTrafficReset hears the start of a new capture generation.
func (r *EventRouter) OnTrafficReset(fn func(captureGeneration uint64)) (off func()) {
	return r.resets.add(fn)
}

// OnDropped hears how many events of kind were lost, so the listener can
// recover from a snapshot.
func (r *EventRouter) OnDropped(kind proxyservice.LiveEventKind, fn func(count uint64)) (off func()) {
	drops := r.dropped[kind]
	if drops == nil {
		drops = &listeners[uint64]{}
		r.dropped[kind] = drops
	}
	return drops.add(fn)
}

// ListenerCount returns the number of subscribed listeners.
func (r *EventRouter) ListenerCount() int {
	count := len(r.entries.items) + len(r.patches.items) + len(r.liveUpdates.items) + len(r.resets.items)
	for _, drops := range r.dropped {
		count += len(drops.items)
	}
	return count
}

// Dispatch delivers a batch: events in order, then drop counts by kind.
// Request-editor stream and session events go to the sink of the operation
// that started them.
func (r *EventRouter) Dispatch(batch proxyservice.LiveBatch) {
	for _, event := range batch.Events {
		switch event := event.(type) {
		case proxyservice.TrafficEntryEvent:
			if event.Entry != nil {
				r.entries.call(event.Entry)
			}
		case proxyservice.TrafficPatchEvent:
			r.patches.call(&event.Patch)
		case proxyservice.TrafficLiveUpdateEvent:
			r.liveUpdates.call(event.Update)
		case proxyservice.TrafficResetEvent:
			r.resets.call(event.CaptureGeneration)
		case proxyservice.HTTPStreamEvent:
			event.Deliver()
		case proxyservice.WebSocketEvent:
			event.Deliver()
		}
	}
	kinds := slices.Sorted(maps.Keys(batch.Dropped))
	for _, kind := range kinds {
		if count := batch.Dropped[kind]; count != 0 {
			if drops := r.dropped[kind]; drops != nil {
				drops.call(count)
			}
		}
	}
}

// Connect feeds batches from live through post, which must run its function
// on the main thread. One batch causes one redraw. Each batch is claimed until
// it has been dispatched, so at most one batch waits for the main thread and
// events published meanwhile stay within the backend's pending bound.
func (r *EventRouter) Connect(live Topic[proxyservice.LiveBatch], post func(func())) (off func()) {
	return live.Subscribe(func(batch proxyservice.LiveBatch) {
		release := batch.Claim()
		post(func() {
			defer release()
			r.Dispatch(batch)
		})
	})
}
