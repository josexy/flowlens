// Package topictest records the values published on service topics in tests.
package topictest

import (
	"sync"

	"github.com/josexy/flowlens/internal/serviceruntime"
)

// Recorder keeps every value published on one topic since Record.
type Recorder[T any] struct {
	mu     sync.Mutex
	values []T
}

// Record subscribes a new Recorder to topic for the rest of the test.
func Record[T any](t interface{ Cleanup(func()) }, topic *serviceruntime.Topic[T]) *Recorder[T] {
	recorder := &Recorder[T]{}
	off := topic.Subscribe(func(value T) {
		recorder.mu.Lock()
		recorder.values = append(recorder.values, value)
		recorder.mu.Unlock()
	})
	t.Cleanup(off)
	return recorder
}

// Values returns a copy of the values published so far, in order.
func (r *Recorder[T]) Values() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.values...)
}

// Len returns how many values were published.
func (r *Recorder[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.values)
}

// Last returns the latest value, or false when none was published.
func (r *Recorder[T]) Last() (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var zero T
	if len(r.values) == 0 {
		return zero, false
	}
	return r.values[len(r.values)-1], true
}
