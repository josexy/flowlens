package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGracefulShutdownCoordinatorWaitsForUIReadyAndRunsOnce(t *testing.T) {
	var showCalls atomic.Int32
	var prepareCalls atomic.Int32
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	quitCalled := make(chan struct{}, 1)

	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		showLoading: func() {
			showCalls.Add(1)
		},
		prepare: func() error {
			prepareCalls.Add(1)
			close(prepareStarted)
			<-releasePrepare
			return nil
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	const callers = 16
	var requestWinners atomic.Int32
	var requests sync.WaitGroup
	for range callers {
		requests.Go(func() {
			if coordinator.Request() {
				requestWinners.Add(1)
			}
		})
	}
	requests.Wait()

	if got := requestWinners.Load(); got != 1 {
		t.Fatalf("Request winners = %d, want 1", got)
	}
	if got := showCalls.Load(); got != 1 {
		t.Fatalf("show loading calls = %d, want 1", got)
	}
	if !coordinator.InProgress() {
		t.Fatal("coordinator should report shutdown in progress after Request")
	}
	if coordinator.CanQuit() {
		t.Fatal("coordinator must not allow Wails to quit before preparation")
	}

	select {
	case <-prepareStarted:
		t.Fatal("preparation started before the UI acknowledged the loading state")
	case <-time.After(20 * time.Millisecond):
	}

	var readyWinners atomic.Int32
	var readyCalls sync.WaitGroup
	for range callers {
		readyCalls.Go(func() {
			if coordinator.UIReady() {
				readyWinners.Add(1)
			}
		})
	}
	readyCalls.Wait()
	if got := readyWinners.Load(); got != 1 {
		t.Fatalf("UIReady winners = %d, want 1", got)
	}
	waitForShutdownSignal(t, prepareStarted)
	close(releasePrepare)
	waitForShutdownSignal(t, quitCalled)

	if got := prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls = %d, want 1", got)
	}
	if !coordinator.CanQuit() {
		t.Fatal("coordinator should allow Wails to quit after preparation")
	}
	if coordinator.InProgress() {
		t.Fatal("coordinator should no longer report shutdown in progress")
	}
}

func TestGracefulShutdownCoordinatorFallsBackWithoutUIReady(t *testing.T) {
	prepareCalled := make(chan struct{}, 1)
	quitCalled := make(chan struct{}, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		uiReadyTimeout: 20 * time.Millisecond,
		prepare: func() error {
			prepareCalled <- struct{}{}
			return nil
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	coordinator.Request()
	waitForShutdownSignal(t, prepareCalled)
	waitForShutdownSignal(t, quitCalled)
	if !coordinator.CanQuit() {
		t.Fatal("fallback preparation should allow Wails to quit")
	}
	if coordinator.UIReady() {
		t.Fatal("late UI acknowledgement must not start preparation again")
	}
}

func TestGracefulShutdownCoordinatorHonoursMinimumVisibleDuration(t *testing.T) {
	const minimumVisible = 60 * time.Millisecond
	quitCalled := make(chan time.Time, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		minimumVisible: minimumVisible,
		prepare: func() error {
			return nil
		},
		quit: func() {
			quitCalled <- time.Now()
		},
	})

	coordinator.Request()
	readyAt := time.Now()
	coordinator.UIReady()
	quitAt := waitForShutdownValue(t, quitCalled)
	if elapsed := quitAt.Sub(readyAt); elapsed < minimumVisible-10*time.Millisecond {
		t.Fatalf("loading visible for %s, want at least %s", elapsed, minimumVisible)
	}
}

func TestGracefulShutdownCoordinatorReportsErrorAndStillQuits(t *testing.T) {
	wantErr := errors.New("save settings")
	errorReported := make(chan error, 1)
	quitCalled := make(chan struct{}, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		prepare: func() error {
			return wantErr
		},
		onPrepareError: func(err error) {
			errorReported <- err
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	coordinator.Request()
	coordinator.UIReady()
	if got := waitForShutdownValue(t, errorReported); !errors.Is(got, wantErr) {
		t.Fatalf("reported error = %v, want %v", got, wantErr)
	}
	waitForShutdownSignal(t, quitCalled)
	if !coordinator.CanQuit() {
		t.Fatal("preparation errors must not prevent the selected best-effort exit policy")
	}
}

func TestGracefulShutdownCoordinatorPrepareAndWaitIsIdempotent(t *testing.T) {
	wantErr := errors.New("restore system proxy")
	var prepareCalls atomic.Int32
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		prepare: func() error {
			prepareCalls.Add(1)
			return wantErr
		},
	})

	for range 2 {
		if got := coordinator.PrepareAndWait(); !errors.Is(got, wantErr) {
			t.Fatalf("PrepareAndWait error = %v, want %v", got, wantErr)
		}
	}
	if got := prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls = %d, want 1", got)
	}
	if coordinator.CanQuit() {
		t.Fatal("synchronous fallback preparation alone must not re-enter app.Quit")
	}
}

func waitForShutdownSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	waitForShutdownValue(t, signal)
}

func waitForShutdownValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown coordinator")
		var zero T
		return zero
	}
}
