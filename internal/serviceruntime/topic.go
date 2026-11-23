// Package serviceruntime manages service startup and shutdown and provides
// typed event topics, independent of the UI toolkit.
package serviceruntime

import "sync"

// Topic delivers values of one type from the service that owns it to its
// subscribers. The zero value is ready to use, and every method is safe from
// any goroutine.
//
// Publish runs subscribers synchronously on the caller's goroutine, in the
// order they subscribed, so one publisher's values reach a subscriber in
// order. Subscribers must not block: the interface posts each value to its
// main thread or appends it under a short lock.
type Topic[T any] struct {
	mu          sync.Mutex
	next        uint64
	subscribers []topicSubscriber[T]
}

type topicSubscriber[T any] struct {
	id uint64
	fn func(T)
}

// Publish delivers value to the current subscribers. A subscriber that
// unsubscribes during delivery still receives the value being delivered.
func (t *Topic[T]) Publish(value T) {
	t.mu.Lock()
	subscribers := t.subscribers
	t.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber.fn(value)
	}
}

// Subscribe adds fn and returns a function that removes it. Calling off more
// than once is safe.
func (t *Topic[T]) Subscribe(fn func(T)) (off func()) {
	t.mu.Lock()
	t.next++
	id := t.next
	// Copy on write: Publish iterates a snapshot without holding the lock.
	updated := make([]topicSubscriber[T], len(t.subscribers), len(t.subscribers)+1)
	copy(updated, t.subscribers)
	t.subscribers = append(updated, topicSubscriber[T]{id: id, fn: fn})
	t.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { t.remove(id) }) }
}

func (t *Topic[T]) remove(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	updated := make([]topicSubscriber[T], 0, len(t.subscribers))
	for _, subscriber := range t.subscribers {
		if subscriber.id != id {
			updated = append(updated, subscriber)
		}
	}
	if len(updated) == 0 {
		updated = nil
	}
	t.subscribers = updated
}
