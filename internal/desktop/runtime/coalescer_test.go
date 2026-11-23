package runtime_test

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
)

func TestCoalescerBatchesInOrderWithOnePostPerInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher, _, window := setup()
		var batches [][]int
		coalescer := runtime.NewCoalescer(dispatcher, func(batch []int) { batches = append(batches, batch) }, nil)
		defer coalescer.Close()

		for value := range 100 {
			coalescer.Add(value)
		}
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		if window.Updated() != 1 {
			t.Fatalf("posts = %d, want one for the interval", window.Updated())
		}
		window.Drain()
		if len(batches) != 1 || !slices.Equal(batches[0], seq(0, 100)) {
			t.Fatalf("batches = %v", batches)
		}

		coalescer.Add(100)
		coalescer.Add(101)
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		window.Drain()
		if window.Updated() != 2 || len(batches) != 2 || !slices.Equal(batches[1], []int{100, 101}) {
			t.Fatalf("posts = %d, batches = %v", window.Updated(), batches)
		}
	})
}

func TestCoalescerHoldsValuesWhileABatchWaitsForTheMainThread(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher, _, window := setup()
		var delivered []int
		keepLast := func(pending []int) []int {
			if len(pending) > 3 {
				return pending[len(pending)-3:]
			}
			return pending
		}
		coalescer := runtime.NewCoalescer(dispatcher, func(batch []int) { delivered = append(delivered, batch...) }, keepLast)
		defer coalescer.Close()

		coalescer.Add(0)
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		// The main thread is stalled: later values wait in the coalescer.
		for value := 1; value <= 10; value++ {
			coalescer.Add(value)
			time.Sleep(runtime.CoalesceInterval)
			synctest.Wait()
		}
		if window.Updated() != 1 {
			t.Fatalf("posts while the first batch waits = %d, want 1", window.Updated())
		}
		window.Drain()
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		window.Drain()
		if !slices.Equal(delivered, []int{0, 8, 9, 10}) || window.Updated() != 2 {
			t.Fatalf("delivered %v in %d posts, want the bound's newest values in a second post", delivered, window.Updated())
		}
	})
}

func TestCoalescerCloseDropsPendingValuesWithoutBlocking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher, _, window := setup()
		delivered := 0
		coalescer := runtime.NewCoalescer(dispatcher, func(batch []int) { delivered += len(batch) }, nil)
		coalescer.Add(1)
		coalescer.Close()
		coalescer.Add(2)
		time.Sleep(10 * runtime.CoalesceInterval)
		synctest.Wait()
		window.Drain()
		if delivered != 0 || window.Updated() != 0 {
			t.Fatalf("delivered %d values in %d posts after Close", delivered, window.Updated())
		}
	})
}

func TestCoalescerCloseWhileABatchWaitsDiscardsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher, _, window := setup()
		delivered := 0
		coalescer := runtime.NewCoalescer(dispatcher, func(batch []int) { delivered += len(batch) }, nil)
		coalescer.Add(1)
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		coalescer.Close()
		window.Drain()
		if delivered != 0 {
			t.Fatal("a batch was delivered after Close")
		}
	})
}

func TestCoalescerDiscardDropsUnpostedValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher, _, window := setup()
		var delivered []int
		coalescer := runtime.NewCoalescer(dispatcher, func(batch []int) { delivered = append(delivered, batch...) }, nil)
		defer coalescer.Close()
		coalescer.Add(1)
		coalescer.Discard()
		coalescer.Add(2)
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		window.Drain()
		if !slices.Equal(delivered, []int{2}) {
			t.Fatalf("delivered %v", delivered)
		}
	})
}

func TestCoalescerAfterDispatcherCloseDoesNotUpdateWindows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatcher := runtime.NewDispatcher()
		window := &runtimetest.Window{}
		dispatcher.SetMain(window)
		coalescer := runtime.NewCoalescer(dispatcher, func([]int) { t.Error("delivered after shutdown") }, nil)
		coalescer.Add(1)
		dispatcher.Close()
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		window.Drain()
		if window.Updated() != 0 {
			t.Fatal("a closed dispatcher updated the window")
		}
	})
}

func seq(from, to int) []int {
	values := make([]int, 0, to-from)
	for value := from; value < to; value++ {
		values = append(values, value)
	}
	return values
}
