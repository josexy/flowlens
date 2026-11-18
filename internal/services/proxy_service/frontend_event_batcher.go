package proxyservice

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/josexy/flowlens/internal/serviceruntime"
)

// BatchedAppEventName is the event that carries FrontendEventBatch values to
// the interface.
const BatchedAppEventName = "app:event-batch"

const (
	// frontendEventClaimTimeout bounds how long the batcher waits for a claimed
	// batch to be released, in case its consumer drops it without releasing.
	frontendEventClaimTimeout = 2 * time.Second
	frontendEventOverhead     = 32
	// frontendEventMaxSizeDepth bounds the payload walk. Event payloads are
	// shallow, so anything deeper is counted as a small constant.
	frontendEventMaxSizeDepth = 16
)

var (
	errFrontendEventBatcherClosed = errors.New("frontend event batcher closed")
	errFrontendEventDropped       = errors.New("frontend event dropped because the pending batch is full")
)

// FrontendEvent is one event in a batch. Data is the published value, which
// publishers must not change afterwards.
type FrontendEvent struct {
	Name string
	Data any
}

// FrontendEventBatch holds events in publication order and how many events of
// each name were dropped since the previous batch.
type FrontendEventBatch struct {
	Events  []FrontendEvent
	Dropped map[string]uint64

	claim *frontendBatchClaim
}

// Claim tells the batcher that the batch is still being applied: no further
// batch is emitted until every claim is released, the batcher stops, or a
// safety timeout passes. Events published meanwhile stay within the pending
// limits or are dropped and counted. Claim must be called while the batch is
// being emitted. It is safe on a batch the batcher did not emit, and release
// is safe to call more than once.
func (b FrontendEventBatch) Claim() (release func()) {
	claim := b.claim
	if claim == nil {
		return func() {}
	}
	claim.mu.Lock()
	claim.active++
	claim.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			claim.mu.Lock()
			claim.active--
			idle := claim.active == 0
			claim.mu.Unlock()
			if idle {
				select {
				case claim.idle <- struct{}{}:
				default:
				}
			}
		})
	}
}

type frontendBatchClaim struct {
	mu     sync.Mutex
	active int
	// idle is signaled when active drops to zero.
	idle chan struct{}
}

func (c *frontendBatchClaim) isActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active > 0
}

type frontendEventBatcherOptions struct {
	FlushInterval    time.Duration
	MaxPendingEvents int
	MaxPendingBytes  int
	// ClaimTimeout bounds the wait for a claimed batch to be released.
	ClaimTimeout time.Duration
}

type frontendEventBatcher struct {
	options frontendEventBatcherOptions
	emit    func(FrontendEventBatch)

	mu           sync.Mutex
	pending      []FrontendEvent
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
	emit func(FrontendEventBatch),
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
	if options.ClaimTimeout <= 0 {
		options.ClaimTimeout = frontendEventClaimTimeout
	}
	if emit == nil {
		emit = func(FrontendEventBatch) {}
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

// FrontendEventPublisher publishes high-frequency events to the interface in
// bounded batches.
type FrontendEventPublisher interface {
	Publish(name string, data any) error
	Close()
}

// NewFrontendEventPublisher starts a batcher with the limits ProxyService uses
// and emits its batches on events as BatchedAppEventName. Interface tests use
// it to exercise real batching and back-pressure.
func NewFrontendEventPublisher(events serviceruntime.EventBus) FrontendEventPublisher {
	return newBusFrontendEventBatcher(events, defaultFrontendEventBatcherOptions())
}

func defaultFrontendEventBatcherOptions() frontendEventBatcherOptions {
	return frontendEventBatcherOptions{
		FlushInterval:    frontendEventFlushInterval,
		MaxPendingEvents: frontendEventMaxPending,
		MaxPendingBytes:  frontendEventMaxBytes,
	}
}

func newBusFrontendEventBatcher(
	events serviceruntime.EventBus,
	options frontendEventBatcherOptions,
) *frontendEventBatcher {
	return newFrontendEventBatcher(options, func(batch FrontendEventBatch) {
		if events != nil {
			events.Emit(BatchedAppEventName, batch)
		}
	})
}

func (b *frontendEventBatcher) Publish(name string, data any) error {
	return b.publish(name, data, true)
}

// PublishUnbounded preserves batching and publication order without applying
// the pending event count or byte limits.
func (b *frontendEventBatcher) PublishUnbounded(name string, data any) error {
	return b.publish(name, data, false)
}

func (b *frontendEventBatcher) publish(name string, data any, enforceLimits bool) error {
	if name == "" {
		return fmt.Errorf("frontend event name is empty")
	}
	minimumEventSize := len(name) + frontendEventOverhead + 1
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

	// Sized outside the lock: a large payload must not hold up other publishers.
	eventSize := len(name) + estimateFrontendEventSize(data) + frontendEventOverhead
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
	b.pending = append(b.pending, FrontendEvent{Name: name, Data: data})
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
				if !b.waitForRelease(b.emitPending()) {
					b.emitPending()
					return
				}
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

// emitPending emits the pending events and returns the batch's claim, or nil
// when nothing was pending.
func (b *frontendEventBatcher) emitPending() *frontendBatchClaim {
	b.mu.Lock()
	if len(b.pending) == 0 && len(b.dropped) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := FrontendEventBatch{
		Events:  b.pending,
		Dropped: b.dropped,
		claim:   &frontendBatchClaim{idle: make(chan struct{}, 1)},
	}
	b.pending = nil
	b.pendingBytes = 0
	b.dropped = nil
	b.mu.Unlock()

	b.emit(batch)
	return batch.claim
}

// waitForRelease waits until a claimed batch is released or the claim timeout
// passes. It returns false when the batcher is stopping.
func (b *frontendEventBatcher) waitForRelease(claim *frontendBatchClaim) bool {
	if claim == nil || !claim.isActive() {
		return true
	}
	timer := time.NewTimer(b.options.ClaimTimeout)
	defer timer.Stop()
	for {
		select {
		case <-claim.idle:
			if !claim.isActive() {
				return true
			}
		case <-timer.C:
			return true
		case <-b.stop:
			return false
		}
	}
}

// estimateFrontendEventSize approximates the memory a pending payload keeps
// alive, so the byte limit bounds bursts of large chunks and bodies.
func estimateFrontendEventSize(data any) int {
	return estimateValueSize(reflect.ValueOf(data), 0)
}

func estimateValueSize(value reflect.Value, depth int) int {
	const scalarSize = 8
	if !value.IsValid() || depth > frontendEventMaxSizeDepth {
		return scalarSize
	}
	switch value.Kind() {
	case reflect.String:
		return value.Len() + 2
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value.Len() + 2
		}
		size := 2
		for index := range value.Len() {
			size += estimateValueSize(value.Index(index), depth+1) + 1
		}
		return size
	case reflect.Map:
		size := 2
		for iterator := value.MapRange(); iterator.Next(); {
			size += estimateValueSize(iterator.Key(), depth+1)
			size += estimateValueSize(iterator.Value(), depth+1) + 1
		}
		return size
	case reflect.Struct:
		size := 2
		valueType := value.Type()
		for index := range value.NumField() {
			if valueType.Field(index).IsExported() {
				size += estimateValueSize(value.Field(index), depth+1) + 1
			}
		}
		return size
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return scalarSize
		}
		return estimateValueSize(value.Elem(), depth+1)
	default:
		return scalarSize
	}
}
