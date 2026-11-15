// Package runtimetest provides a stand-in for native windows in tests.
package runtimetest

import "sync"

// Window records Update and Invalidate calls. Update functions run when Drain
// is called, on the caller's goroutine, as the main thread would run them.
type Window struct {
	mu          sync.Mutex
	queue       []func()
	invalidated int
	updated     int
}

// Update implements runtime.Window.
func (w *Window) Update(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.queue = append(w.queue, fn)
	w.updated++
}

// Invalidate implements runtime.Window.
func (w *Window) Invalidate() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.invalidated++
}

// Drain runs queued functions in order until none remain.
func (w *Window) Drain() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.mu.Unlock()
			return
		}
		fn := w.queue[0]
		w.queue = w.queue[1:]
		w.mu.Unlock()
		fn()
	}
}

// Invalidated returns how many times the window was asked to redraw.
func (w *Window) Invalidated() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.invalidated
}

// Updated returns how many Update calls the window received.
func (w *Window) Updated() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.updated
}
