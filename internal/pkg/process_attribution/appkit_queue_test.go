package processattribution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testAppKitQueue() (*appKitQueue, chan uintptr) {
	ids := make(chan uintptr, appKitQueueCapacity)
	q := &appKitQueue{isMain: func() bool { return false }, dispatch: func(id uintptr) { ids <- id }, scope: func(call func() (any, error)) (any, error) { return call() }}
	return q, ids
}

func TestAppKitQueueCancellationAndLateCallback(t *testing.T) {
	q, ids := testAppKitQueue()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var called atomic.Bool
	go func() {
		_, err := q.invoke(ctx, func() (any, error) { called.Store(true); return nil, nil })
		done <- err
	}()
	id := <-ids
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	q.mu.Lock()
	request := q.pending[id]
	q.mu.Unlock()
	if request != nil {
		t.Fatal("canceled closure retained")
	}
	q.run(id)
	q.run(id) // duplicate/late callbacks must not publish or consume another ID
	if called.Load() || len(q.pending) != 0 {
		t.Fatal("late callback ran canceled work")
	}
}

func TestAppKitQueueAlreadyMainThreadAndCleanup(t *testing.T) {
	q, ids := testAppKitQueue()
	q.isMain = func() bool { return true }
	closed := 0
	q.scope = func(call func() (any, error)) (any, error) { defer func() { closed++ }(); return call() }
	want := errors.New("native failure")
	_, err := q.invoke(context.Background(), func() (any, error) { return nil, want })
	if !errors.Is(err, want) || closed != 1 || len(ids) != 0 {
		t.Fatalf("direct call: %v, cleanup %d, queued %d", err, closed, len(ids))
	}
}

func TestAppKitQueueAbsentRunLoopIsBounded(t *testing.T) {
	q, ids := testAppKitQueue()
	for i := 0; i < appKitQueueCapacity; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := q.invoke(ctx, func() (any, error) { t.Error("canceled work ran"); return nil, nil })
			done <- err
		}()
		<-ids
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if _, err := q.invoke(context.Background(), func() (any, error) { return nil, nil }); err == nil {
		t.Fatal("unbounded dispatch queue")
	}
	for id, request := range q.pending {
		if request != nil {
			t.Fatal("retained canceled request")
		}
		q.run(id)
	}
	if len(q.pending) != 0 {
		t.Fatal("queue did not drain")
	}
}

func TestAppKitQueueCancelRunningCall(t *testing.T) {
	q, ids := testAppKitQueue()
	ctx, cancel := context.WithCancel(context.Background())
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := q.invoke(ctx, func() (any, error) { close(started); <-release; return "stale", nil })
		done <- err
	}()
	id := <-ids
	go func() { q.run(id); close(finished) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for main thread")
	}
	close(release)
	<-finished
	if len(q.pending) != 0 {
		t.Fatal("retained running request")
	}
}

func TestAppKitQueueDeadlineAndResult(t *testing.T) {
	q, ids := testAppKitQueue()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := q.invoke(ctx, func() (any, error) { return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	q.run(<-ids)
	done := make(chan any, 1)
	go func() {
		value, _ := q.invoke(context.Background(), func() (any, error) { return "ready", nil })
		done <- value
	}()
	q.run(<-ids)
	if value := <-done; value != "ready" {
		t.Fatalf("result: %v", value)
	}
}

func TestAppKitQueueManagerCloseAndReplacement(t *testing.T) {
	q, ids := testAppKitQueue()
	returned := make(chan struct{}, 2)
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		defer func() { returned <- struct{}{} }()
		value, err := q.invoke(ctx, func() (any, error) { return resolvedResult(42, "start", "current"), nil })
		if err != nil {
			return Result{Status: StatusNotFound}
		}
		return value.(Result)
	}}
	oldManager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Minute})
	oldLookup := oldManager.Start(context.Background(), testTuple(45001))
	defer oldLookup.Release()
	oldID := <-ids
	closed := make(chan struct{})
	go func() { oldManager.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Manager close waited for AppKit")
	}
	<-returned
	newManager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Minute})
	newLookup := newManager.Start(context.Background(), testTuple(45001))
	defer newLookup.Release()
	newID := <-ids
	if newID == oldID {
		t.Fatal("native callback ID reused across Manager rotation")
	}
	q.run(oldID)
	if oldLookup.Snapshot().Status == StatusResolved || newLookup.Snapshot().Status != StatusPending {
		t.Fatal("late callback published across Manager rotation")
	}
	q.run(newID)
	<-returned
	if result := waitForStatus(t, newLookup, StatusResolved); result.PID != 42 {
		t.Fatalf("new Manager lookup: %+v", result)
	}
}
