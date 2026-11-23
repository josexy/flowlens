package runtime

import (
	"sync"
	"time"
)

// CoalesceInterval is how long a Coalescer gathers values before it hands
// them to the main thread.
const CoalesceInterval = 16 * time.Millisecond

// Coalescer gathers values that producers add from any goroutine and hands
// them to the main thread in ordered batches, one dispatcher post per batch,
// so a burst causes one interface update per interval rather than one per
// value. While a batch waits for the main thread, new values wait in the
// coalescer, bounded by its bound function, and no further batch is posted.
type Coalescer[T any] struct {
	post     func(func())
	deliver  func(batch []T)
	bound    func(pending []T) []T
	interval time.Duration

	mu       sync.Mutex
	pending  []T
	timer    *time.Timer
	inFlight bool
	closed   bool
}

// NewCoalescer returns a coalescer that delivers batches on the main thread
// through dispatcher. bound, when set, runs after each value is added and
// returns the values to keep, so that pending values stay bounded while the
// main thread is busy.
func NewCoalescer[T any](dispatcher *Dispatcher, deliver func(batch []T), bound func(pending []T) []T) *Coalescer[T] {
	return &Coalescer[T]{post: dispatcher.Post, deliver: deliver, bound: bound, interval: CoalesceInterval}
}

// Add queues value for the next batch. After Close it does nothing.
func (c *Coalescer[T]) Add(value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.pending = append(c.pending, value)
	if c.bound != nil {
		c.pending = c.bound(c.pending)
	}
	c.armLocked()
}

// Discard drops the values that have not been posted yet.
func (c *Coalescer[T]) Discard() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = nil
}

// Close stops delivery and drops pending values without waiting for the main
// thread. A batch already posted still runs if the dispatcher runs it.
func (c *Coalescer[T]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (c *Coalescer[T]) armLocked() {
	if c.timer != nil || c.inFlight || len(c.pending) == 0 {
		return
	}
	c.timer = time.AfterFunc(c.interval, c.flush)
}

func (c *Coalescer[T]) flush() {
	c.mu.Lock()
	c.timer = nil
	if c.closed || len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	batch := c.pending
	c.pending = nil
	c.inFlight = true
	c.mu.Unlock()
	c.post(func() {
		c.mu.Lock()
		c.inFlight = false
		closed := c.closed
		c.armLocked()
		c.mu.Unlock()
		if !closed {
			c.deliver(batch)
		}
	})
}
