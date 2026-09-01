package proxyservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const batchedAppEventName = "app:event-batch"

const serializedFrontendEventOverhead = 32

var (
	errFrontendEventBatcherClosed = errors.New("frontend event batcher closed")
	errFrontendEventDropped       = errors.New("frontend event dropped because the pending batch is full")
)

type serializedFrontendEvent struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

type frontendEventBatch struct {
	Events  []serializedFrontendEvent `json:"events"`
	Dropped map[string]uint64         `json:"dropped,omitempty"`
}

type frontendEventBatcherOptions struct {
	FlushInterval    time.Duration
	MaxPendingEvents int
	MaxPendingBytes  int
}

type frontendEventBatcher struct {
	options frontendEventBatcherOptions
	emit    func(frontendEventBatch)

	mu           sync.Mutex
	pending      []serializedFrontendEvent
	pendingBytes int
	dropped      map[string]uint64
	closed       bool

	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newFrontendEventBatcher(
	options frontendEventBatcherOptions,
	emit func(frontendEventBatch),
) *frontendEventBatcher {
	if options.FlushInterval <= 0 {
		options.FlushInterval = 16 * time.Millisecond
	}
	if options.MaxPendingEvents <= 0 {
		options.MaxPendingEvents = 512
	}
	if options.MaxPendingBytes <= 0 {
		options.MaxPendingBytes = 4 << 20
	}
	if emit == nil {
		emit = func(frontendEventBatch) {}
	}

	batcher := &frontendEventBatcher{
		options: options,
		emit:    emit,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go batcher.run()
	return batcher
}

func newWindowFrontendEventBatcher(
	app *application.App,
	windowName string,
	options frontendEventBatcherOptions,
) *frontendEventBatcher {
	return newFrontendEventBatcher(options, func(batch frontendEventBatch) {
		if app == nil {
			return
		}
		window, ok := app.Window.GetByName(windowName)
		if !ok || window == nil {
			return
		}
		window.DispatchWailsEvent(&application.CustomEvent{
			Name: batchedAppEventName,
			Data: batch,
		})
	})
}

func (b *frontendEventBatcher) Publish(name string, data any) error {
	return b.publish(name, data, true)
}

// PublishUnbounded preserves batching and publication order without applying
// the pending event count or serialized byte limits.
func (b *frontendEventBatcher) PublishUnbounded(name string, data any) error {
	return b.publish(name, data, false)
}

func (b *frontendEventBatcher) publish(name string, data any, enforceLimits bool) error {
	if name == "" {
		return fmt.Errorf("frontend event name is empty")
	}
	minimumEventSize := len(name) + serializedFrontendEventOverhead + 1
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errFrontendEventBatcherClosed
	}
	if enforceLimits && (len(b.pending) >= b.options.MaxPendingEvents ||
		minimumEventSize > b.options.MaxPendingBytes ||
		b.pendingBytes > b.options.MaxPendingBytes-minimumEventSize) {
		b.recordDropLocked(name)
		b.mu.Unlock()
		b.signal()
		return errFrontendEventDropped
	}
	b.mu.Unlock()

	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal frontend event %q: %w", name, err)
	}

	eventSize := len(name) + len(encoded) + serializedFrontendEventOverhead
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errFrontendEventBatcherClosed
	}
	if enforceLimits && (len(b.pending) >= b.options.MaxPendingEvents ||
		eventSize > b.options.MaxPendingBytes ||
		b.pendingBytes > b.options.MaxPendingBytes-eventSize) {
		b.recordDropLocked(name)
		b.mu.Unlock()
		b.signal()
		return errFrontendEventDropped
	}
	b.pending = append(b.pending, serializedFrontendEvent{Name: name, Data: encoded})
	b.pendingBytes += eventSize
	b.mu.Unlock()
	b.signal()
	return nil
}

func (b *frontendEventBatcher) recordDropLocked(name string) {
	if b.dropped == nil {
		b.dropped = make(map[string]uint64)
	}
	b.dropped[name]++
}

func (b *frontendEventBatcher) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		close(b.stop)
		<-b.done
	})
}

func (b *frontendEventBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *frontendEventBatcher) run() {
	defer close(b.done)
	for {
		select {
		case <-b.wake:
			if !b.hasPending() {
				continue
			}
			timer := time.NewTimer(b.options.FlushInterval)
			select {
			case <-timer.C:
				b.emitPending()
			case <-b.stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				b.emitPending()
				return
			}
		case <-b.stop:
			b.emitPending()
			return
		}
	}
}

func (b *frontendEventBatcher) hasPending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) != 0 || len(b.dropped) != 0
}

func (b *frontendEventBatcher) emitPending() {
	b.mu.Lock()
	if len(b.pending) == 0 && len(b.dropped) == 0 {
		b.mu.Unlock()
		return
	}
	batch := frontendEventBatch{
		Events:  b.pending,
		Dropped: b.dropped,
	}
	b.pending = nil
	b.pendingBytes = 0
	b.dropped = nil
	b.mu.Unlock()

	b.emit(batch)
}
