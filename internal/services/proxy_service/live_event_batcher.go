package proxyservice

import (
	"errors"
	"sync"
	"time"

	"github.com/josexy/flowlens/internal/serviceruntime"
)

var (
	errLiveBatcherClosed = errors.New("live event batcher closed")
	errLiveEventDropped  = errors.New("live event dropped because the pending batch is full")
)

// LiveBatch holds live events in publication order and how many events of
// each kind were dropped since the previous batch.
type LiveBatch struct {
	Events  []LiveEvent
	Dropped map[LiveEventKind]uint64

	claim *liveBatchClaim
}

// Claim tells the batcher that the batch is still being applied: no further
// batch is emitted until every claim is released. Shutdown ends the wait
// without delivering another batch. Events published meanwhile stay within the pending
// limits or are dropped and counted. Claim must be called while the batch is
// being emitted. It is safe on a batch the batcher did not emit, and release
// is safe to call more than once.
func (b LiveBatch) Claim() (release func()) {
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

type liveBatchClaim struct {
	mu     sync.Mutex
	active int
	// idle is signaled when active drops to zero.
	idle chan struct{}
}

func (c *liveBatchClaim) isActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active > 0
}

type liveEventBatcherOptions struct {
	FlushInterval    time.Duration
	MaxPendingEvents int
	MaxPendingBytes  int
}

type liveEventBatcher struct {
	options liveEventBatcherOptions
	emit    func(LiveBatch)

	mu           sync.Mutex
	pending      []LiveEvent
	pendingBytes int
	dropped      map[LiveEventKind]uint64
	closed       bool

	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newLiveEventBatcher(
	options liveEventBatcherOptions,
	emit func(LiveBatch),
) *liveEventBatcher {
	if options.FlushInterval <= 0 {
		options.FlushInterval = liveEventFlushInterval
	}
	if options.MaxPendingEvents <= 0 {
		options.MaxPendingEvents = liveEventMaxPending
	}
	if options.MaxPendingBytes <= 0 {
		options.MaxPendingBytes = liveEventMaxBytes
	}
	if emit == nil {
		emit = func(LiveBatch) {}
	}
	batcher := &liveEventBatcher{
		options: options,
		emit:    emit,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go batcher.run()
	return batcher
}

// LivePublisher publishes high-frequency events to the interface in bounded
// batches.
type LivePublisher interface {
	Publish(event LiveEvent) error
	Close()
}

// NewLivePublisher starts a batcher with the limits ProxyService uses and
// publishes its batches on live. Interface tests use it to exercise real
// batching and back-pressure.
func NewLivePublisher(live *serviceruntime.Topic[LiveBatch]) LivePublisher {
	return newLiveEventBatcher(defaultLiveEventBatcherOptions(), live.Publish)
}

func defaultLiveEventBatcherOptions() liveEventBatcherOptions {
	return liveEventBatcherOptions{
		FlushInterval:    liveEventFlushInterval,
		MaxPendingEvents: liveEventMaxPending,
		MaxPendingBytes:  liveEventMaxBytes,
	}
}

// Publish queues event within the pending event and byte limits; beyond
// them the event is dropped and counted for its kind.
func (b *liveEventBatcher) Publish(event LiveEvent) error {
	return b.publish(event, true)
}

// PublishUnbounded preserves batching and publication order without applying
// the pending event count or byte limits.
func (b *liveEventBatcher) PublishUnbounded(event LiveEvent) error {
	return b.publish(event, false)
}

func (b *liveEventBatcher) publish(event LiveEvent, enforceLimits bool) error {
	if event == nil {
		return errors.New("live event is nil")
	}
	// Sized outside the lock: a large entry must not hold up other publishers.
	size := event.liveSize()
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errLiveBatcherClosed
	}
	if enforceLimits && (len(b.pending) >= b.options.MaxPendingEvents ||
		size > b.options.MaxPendingBytes ||
		b.pendingBytes > b.options.MaxPendingBytes-size) {
		b.recordDropLocked(event.liveKind())
		b.mu.Unlock()
		b.signal()
		return errLiveEventDropped
	}
	b.pending = append(b.pending, event)
	b.pendingBytes += size
	b.mu.Unlock()
	b.signal()
	return nil
}

func (b *liveEventBatcher) recordDropLocked(kind LiveEventKind) {
	if b.dropped == nil {
		b.dropped = make(map[LiveEventKind]uint64)
	}
	b.dropped[kind]++
}
func (b *liveEventBatcher) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		close(b.stop)
		<-b.done
		// An abandoned UI claim must not keep pending payloads alive after
		// shutdown, or cause one more batch to enter the stalled UI queue.
		b.mu.Lock()
		b.pending, b.dropped, b.pendingBytes = nil, nil, 0
		b.mu.Unlock()
	})
}

func (b *liveEventBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *liveEventBatcher) run() {
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

func (b *liveEventBatcher) hasPending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) != 0 || len(b.dropped) != 0
}

// emitPending emits the pending events and returns the batch's claim, or nil
// when nothing was pending.
func (b *liveEventBatcher) emitPending() *liveBatchClaim {
	b.mu.Lock()
	if len(b.pending) == 0 && len(b.dropped) == 0 {
		b.mu.Unlock()
		return nil
	}
	batch := LiveBatch{
		Events:  b.pending,
		Dropped: b.dropped,
		claim:   &liveBatchClaim{idle: make(chan struct{}, 1)},
	}
	b.pending = nil
	b.pendingBytes = 0
	b.dropped = nil
	b.mu.Unlock()

	b.emit(batch)
	return batch.claim
}

// waitForRelease waits until every consumer releases the batch. Only shutdown
// may abandon a claim; elapsed time does not mean the UI consumed its payload.
func (b *liveEventBatcher) waitForRelease(claim *liveBatchClaim) bool {
	if claim == nil || !claim.isActive() {
		return true
	}
	for {
		select {
		case <-claim.idle:
			if !claim.isActive() {
				return true
			}
		case <-b.stop:
			return false
		}
	}
}
