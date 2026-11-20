package processattribution

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
)

const appKitQueueCapacity = 128
const appKitCallTimeout = 2 * time.Second

type appKitResult struct {
	value any
	err   error
}
type appKitRequest struct {
	ctx    context.Context
	call   func() (any, error)
	result chan appKitResult
}

// Native dispatch retains only an integer token. Canceled entries retain no Go
// request, closure or output; their tombstones count against capacity until the
// native callback drains them. Thus an absent run loop cannot grow either queue.
// All native objects are created, copied and released inside call on one thread.
type appKitQueue struct {
	mu       sync.Mutex
	next     uintptr
	pending  map[uintptr]*appKitRequest
	isMain   func() bool
	dispatch func(uintptr)
	scope    func(func() (any, error)) (any, error)
}

func (q *appKitQueue) invoke(parent context.Context, call func() (any, error)) (any, error) {
	ctx, cancel := context.WithTimeout(parent, appKitCallTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Checking main-thread identity and executing directly must be atomic with
	// respect to Go's OS-thread scheduling, including in headless tests.
	runtime.LockOSThread()
	if q.isMain() {
		defer runtime.UnlockOSThread()
		value, err := q.scope(call)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return value, err
	}
	runtime.UnlockOSThread()
	request := &appKitRequest{ctx: ctx, call: call, result: make(chan appKitResult, 1)}
	q.mu.Lock()
	if len(q.pending) >= appKitQueueCapacity || q.next == ^uintptr(0) {
		q.mu.Unlock()
		return nil, errors.New("AppKit request queue is full")
	}
	if q.pending == nil {
		q.pending = make(map[uintptr]*appKitRequest)
	}
	q.next++
	id := q.next
	q.pending[id] = request
	q.mu.Unlock()
	q.dispatch(id)
	select {
	case result := <-request.result:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return result.value, result.err
	case <-ctx.Done():
		q.mu.Lock()
		if _, exists := q.pending[id]; exists {
			q.pending[id] = nil
		}
		q.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (q *appKitQueue) run(id uintptr) {
	q.mu.Lock()
	request := q.pending[id]
	delete(q.pending, id)
	q.mu.Unlock()
	if request == nil || request.ctx.Err() != nil {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	value, err := q.scope(request.call)
	if request.ctx.Err() == nil {
		request.result <- appKitResult{value, err}
	}
}
