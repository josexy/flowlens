// Package runtime connects background work to the interface. Application state
// changes only on the main thread, so everything a goroutine produces reaches
// the state through a Dispatcher.
package runtime

import "sync"

// Window is the part of a native window that the Dispatcher uses.
type Window interface {
	// Update runs fn on the main thread, then redraws the window.
	Update(fn func())
	// Invalidate redraws the window.
	Invalidate()
}

// Dispatcher runs functions on the main thread and redraws every open window
// afterwards, because all windows show the same application state.
type Dispatcher struct {
	mu      sync.Mutex
	main    Window
	windows map[string]Window
	queued  []func()
	closed  bool
}

// NewDispatcher returns a Dispatcher without a main window. Functions posted
// before SetMain are kept and run in order once it is set.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{windows: make(map[string]Window)}
}

// SetMain sets the window that carries work onto the main thread. The main
// window stays open for the whole process.
func (d *Dispatcher) SetMain(window Window) {
	d.mu.Lock()
	d.main = window
	queued := d.queued
	d.queued = nil
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return
	}
	for _, fn := range queued {
		d.deliver(window, fn)
	}
}

// Post runs fn on the main thread and redraws every open window. It is safe
// from any goroutine and returns without waiting. After Close it does nothing.
func (d *Dispatcher) Post(fn func()) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	main := d.main
	if main == nil {
		d.queued = append(d.queued, fn)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.deliver(main, fn)
}

func (d *Dispatcher) deliver(main Window, fn func()) {
	main.Update(func() {
		if d.isClosed() {
			return
		}
		fn()
		d.invalidateSecondary()
	})
}

func (d *Dispatcher) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func (d *Dispatcher) invalidateSecondary() {
	for _, window := range d.secondaryWindows() {
		window.Invalidate()
	}
}

func (d *Dispatcher) secondaryWindows() []Window {
	d.mu.Lock()
	defer d.mu.Unlock()
	windows := make([]Window, 0, len(d.windows))
	for _, window := range d.windows {
		windows = append(windows, window)
	}
	return windows
}

// InvalidateAll redraws every open window.
func (d *Dispatcher) InvalidateAll() {
	d.mu.Lock()
	main, closed := d.main, d.closed
	d.mu.Unlock()
	if closed {
		return
	}
	if main != nil {
		main.Invalidate()
	}
	d.invalidateSecondary()
}

// Register records an open secondary window under name.
func (d *Dispatcher) Register(name string, window Window) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.windows[name] = window
}

// Unregister forgets a closed secondary window, so it is no longer redrawn.
func (d *Dispatcher) Unregister(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.windows, name)
}

// Window returns the open secondary window registered under name.
func (d *Dispatcher) Window(name string) (Window, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	window, ok := d.windows[name]
	return window, ok
}

// Close stops delivery: functions already posted but not yet run are dropped,
// and later posts do nothing.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.queued = nil
	d.windows = make(map[string]Window)
}
