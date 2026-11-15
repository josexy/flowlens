package runtime_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
)

func setup() (*runtime.Dispatcher, *runtime.Jobs, *runtimetest.Window) {
	dispatcher := runtime.NewDispatcher()
	window := &runtimetest.Window{}
	dispatcher.SetMain(window)
	return dispatcher, runtime.NewJobs(dispatcher), window
}

func TestPostRunsInOrderOnTheMainWindow(t *testing.T) {
	dispatcher, _, window := setup()
	var got []int
	for i := range 3 {
		dispatcher.Post(func() { got = append(got, i) })
	}
	if len(got) != 0 {
		t.Fatal("Post ran before the main thread drained")
	}
	window.Drain()
	if want := []int{0, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPostBeforeMainWindowIsKeptInOrder(t *testing.T) {
	dispatcher := runtime.NewDispatcher()
	var got []int
	dispatcher.Post(func() { got = append(got, 1) })
	dispatcher.Post(func() { got = append(got, 2) })
	window := &runtimetest.Window{}
	dispatcher.SetMain(window)
	dispatcher.Post(func() { got = append(got, 3) })
	window.Drain()
	if want := []int{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPostRedrawsSecondaryWindows(t *testing.T) {
	dispatcher, _, window := setup()
	settings := &runtimetest.Window{}
	dispatcher.Register("settings", settings)
	dispatcher.Post(func() {})
	window.Drain()
	if settings.Invalidated() != 1 {
		t.Fatalf("settings invalidated %d times", settings.Invalidated())
	}
	dispatcher.Unregister("settings")
	dispatcher.Post(func() {})
	window.Drain()
	if settings.Invalidated() != 1 {
		t.Fatal("closed window was redrawn")
	}
}

func TestWindowRegistry(t *testing.T) {
	dispatcher, _, _ := setup()
	if _, ok := dispatcher.Window("settings"); ok {
		t.Fatal("unexpected window")
	}
	settings := &runtimetest.Window{}
	dispatcher.Register("settings", settings)
	if got, ok := dispatcher.Window("settings"); !ok || got != settings {
		t.Fatalf("Window = %v, %v", got, ok)
	}
}

func TestInvalidateAll(t *testing.T) {
	dispatcher, _, window := setup()
	settings := &runtimetest.Window{}
	dispatcher.Register("settings", settings)
	dispatcher.InvalidateAll()
	if window.Invalidated() != 1 || settings.Invalidated() != 1 {
		t.Fatalf("main %d, settings %d", window.Invalidated(), settings.Invalidated())
	}
}

func TestCloseDropsPendingAndLaterPosts(t *testing.T) {
	dispatcher, _, window := setup()
	ran := 0
	dispatcher.Post(func() { ran++ })
	dispatcher.Close()
	dispatcher.Post(func() { ran++ })
	window.Drain()
	if ran != 0 {
		t.Fatalf("ran %d after Close", ran)
	}
}

func deliver(jobs *runtime.Jobs, window *runtimetest.Window) {
	jobs.Wait()
	window.Drain()
}

func TestRunDeliversResultOnMainThread(t *testing.T) {
	_, jobs, window := setup()
	var got string
	runtime.Run(jobs, "a", func(context.Context) (string, error) { return "value", nil },
		func(value string, err error) {
			if err != nil {
				t.Error(err)
			}
			got = value
		})
	jobs.Wait()
	if got != "" {
		t.Fatal("result delivered before the main thread drained")
	}
	window.Drain()
	if got != "value" {
		t.Fatalf("got %q", got)
	}
	if jobs.Active("a") {
		t.Fatal("finished job still active")
	}
}

func TestLaterRunSupersedesEarlierResult(t *testing.T) {
	_, jobs, window := setup()
	release := make(chan struct{})
	var canceled atomic.Bool
	var got []string
	runtime.Run(jobs, "detail", func(ctx context.Context) (string, error) {
		<-release
		canceled.Store(ctx.Err() != nil)
		return "A", nil
	}, func(value string, _ error) { got = append(got, value) })
	runtime.Run(jobs, "detail", func(context.Context) (string, error) {
		return "B", nil
	}, func(value string, _ error) { got = append(got, value) })
	close(release)
	deliver(jobs, window)
	if want := []string{"B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !canceled.Load() {
		t.Fatal("superseded job's context was not canceled")
	}
}

func TestSupersededResultWaitingOnMainThreadIsDiscarded(t *testing.T) {
	_, jobs, window := setup()
	var got []string
	runtime.Run(jobs, "detail", func(context.Context) (string, error) { return "A", nil },
		func(value string, _ error) { got = append(got, value) })
	jobs.Wait() // A's result is now queued for the main thread.
	runtime.Run(jobs, "detail", func(context.Context) (string, error) { return "B", nil },
		func(value string, _ error) { got = append(got, value) })
	deliver(jobs, window)
	if want := []string{"B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDifferentKeysDoNotInterfere(t *testing.T) {
	_, jobs, window := setup()
	got := map[string]bool{}
	var mu sync.Mutex
	for _, key := range []string{"a", "b"} {
		runtime.Run(jobs, key, func(context.Context) (string, error) { return key, nil },
			func(value string, _ error) {
				mu.Lock()
				got[value] = true
				mu.Unlock()
			})
	}
	deliver(jobs, window)
	if !got["a"] || !got["b"] {
		t.Fatalf("got %v", got)
	}
}

func TestCancelDiscardsResult(t *testing.T) {
	_, jobs, window := setup()
	called := false
	runtime.Run(jobs, "a", func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}, func(int, error) { called = true })
	jobs.Cancel("a")
	deliver(jobs, window)
	if called {
		t.Fatal("canceled job delivered a result")
	}
}

func TestClosedWindowJobsAreDiscardedWithoutError(t *testing.T) {
	dispatcher, jobs, window := setup()
	settings := &runtimetest.Window{}
	dispatcher.Register("settings", settings)
	called := false
	keep := false
	runtime.Run(jobs, "settings/fonts", func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, nil
	}, func(int, error) { called = true })
	runtime.Run(jobs, "main/list", func(context.Context) (int, error) { return 1, nil },
		func(int, error) { keep = true })

	dispatcher.Unregister("settings")
	jobs.CancelPrefix("settings/")
	deliver(jobs, window)
	if called {
		t.Fatal("result delivered for a closed window")
	}
	if !keep {
		t.Fatal("job of another window was discarded")
	}
	if settings.Invalidated() != 0 {
		t.Fatalf("closed window redrawn %d times", settings.Invalidated())
	}
}

func TestPanicBecomesError(t *testing.T) {
	_, jobs, window := setup()
	var got error
	runtime.Run(jobs, "a", func(context.Context) (int, error) { panic("boom") },
		func(_ int, err error) { got = err })
	deliver(jobs, window)
	if got == nil {
		t.Fatal("panic produced no error")
	}
}

func TestErrorsAreDelivered(t *testing.T) {
	_, jobs, window := setup()
	boom := errors.New("boom")
	var got error
	runtime.Run(jobs, "a", func(context.Context) (int, error) { return 0, boom },
		func(_ int, err error) { got = err })
	deliver(jobs, window)
	if !errors.Is(got, boom) {
		t.Fatalf("got %v", got)
	}
}

func TestCloseCancelsWaitsAndRefusesNewJobs(t *testing.T) {
	_, jobs, window := setup()
	var finished atomic.Bool
	called := false
	runtime.Run(jobs, "a", func(ctx context.Context) (int, error) {
		<-ctx.Done()
		time.Sleep(5 * time.Millisecond)
		finished.Store(true)
		return 0, nil
	}, func(int, error) { called = true })
	jobs.Close()
	if !finished.Load() {
		t.Fatal("Close returned before running work finished")
	}
	started := false
	runtime.Run(jobs, "b", func(context.Context) (int, error) {
		started = true
		return 0, nil
	}, func(int, error) { called = true })
	window.Drain()
	if called || started {
		t.Fatalf("called=%v started=%v after Close", called, started)
	}
}

func TestConcurrentPostsAndRuns(t *testing.T) {
	dispatcher, jobs, window := setup()
	var wg sync.WaitGroup
	var ran atomic.Int64
	for i := range 32 {
		wg.Go(func() {
			dispatcher.Post(func() { ran.Add(1) })
			runtime.Run(jobs, "shared", func(context.Context) (int, error) { return i, nil }, func(int, error) {})
		})
	}
	wg.Wait()
	deliver(jobs, window)
	if ran.Load() != 32 {
		t.Fatalf("ran %d posts", ran.Load())
	}
}
