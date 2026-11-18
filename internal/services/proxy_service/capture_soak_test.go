package proxyservice

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

// TestCaptureSoak captures 200000 small HTTP exchanges and 20 long WebSocket
// connections through the capture interceptors with the spiller running, and
// checks that memory stays flat. It takes a while, so it runs only with
// FLOWLENS_SOAK=1.
func TestCaptureSoak(t *testing.T) {
	if os.Getenv("FLOWLENS_SOAK") == "" {
		t.Skip("set FLOWLENS_SOAK=1 to run the capture soak check")
	}
	const (
		exchanges   = 200000
		checkpoint  = 50000
		connections = 20
	)
	svc := newSpillTestService(t)
	sessionDir := svc.bodyCache.SessionDir()
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	svc.startCaptureSpiller(ctx)

	// Long WebSocket connections stay open for the whole run.
	watchers := make([]testCaptureFrameWatcher, connections)
	var sockets sync.WaitGroup
	for i := range watchers {
		watchers[i] = make(testCaptureFrameWatcher, 16)
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("ws://soak.test/socket/%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		sockets.Go(func() {
			svc.websocketInterceptor(nil)(newRawTCPMetadataContext(), req,
				&http.Response{StatusCode: http.StatusSwitchingProtocols, Proto: "HTTP/1.1"}, watchers[i])
		})
	}

	invoker := consumingInvoker(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, `{"ok":true,"padding":"`+strings.Repeat("x", 512)+`"}`), nil
	})
	heapAt := map[int]uint64{}
	started := time.Now()
	for i := 1; i <= exchanges; i++ {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://soak.test/item/%d", i), strings.NewReader(`{"request":true}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, invoker)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if i%1000 == 0 {
			watchers[(i/1000)%connections] <- newTestCaptureFrame(mitmproxy.Receive, 1, []byte(strings.Repeat("frame ", 32)))
		}
		if i == checkpoint || i == exchanges {
			heapAt[i] = settledLiveHeap(t, svc)
		}
	}
	elapsed := time.Since(started)
	inMemory, spilled := svc.trafficEntries.Len(), svc.captureSpillStore().Len()
	t.Logf("captured %d exchanges and %d WebSocket connections in %s", exchanges, connections, elapsed.Round(time.Millisecond))
	t.Logf("live heap after GC: %.1f MiB at %d entries, %.1f MiB at %d entries",
		float64(heapAt[checkpoint])/(1<<20), checkpoint, float64(heapAt[exchanges])/(1<<20), exchanges)
	t.Logf("entries in memory %d, spilled %d", inMemory, spilled)
	if inMemory > captureMemoryWindow+captureSpillBatch+connections {
		t.Fatalf("%d entries in memory, want at most the window plus a batch and the open connections", inMemory)
	}
	if inMemory+spilled != exchanges+connections {
		t.Fatalf("entries in memory %d + spilled %d, want %d", inMemory, spilled, exchanges+connections)
	}
	// Live memory must not grow with the entry count: four times as many
	// entries may only add the spill index (about 24 bytes per entry) and
	// noise.
	if growth := int64(heapAt[exchanges]) - int64(heapAt[checkpoint]); growth > 24<<20 {
		t.Fatalf("live heap grew by %.1f MiB from %d to %d entries", float64(growth)/(1<<20), checkpoint, exchanges)
	}

	for _, watcher := range watchers {
		close(watcher)
	}
	sockets.Wait()
	cancel()
	svc.waitCaptureSpiller()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > baselineGoroutines {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines = %d after the capture stopped, baseline %d", runtime.NumGoroutine(), baselineGoroutines)
		}
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("goroutines after stopping: %d (baseline %d)", runtime.NumGoroutine(), baselineGoroutines)

	if err := svc.RestartCapture(true); err != nil {
		t.Fatalf("RestartCapture: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session cache directory still exists after restart: %v", err)
	}
}

// settledLiveHeap waits for the spiller to catch up and returns the live heap
// after a full collection. HeapInuse is logged too: it also counts allocator
// fragmentation, which varies with collection timing.
func settledLiveHeap(t *testing.T, svc *ProxyService) uint64 {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for svc.trafficEntries.Len() > captureMemoryWindow+captureSpillBatch+20 {
		if time.Now().After(deadline) {
			t.Fatalf("spiller left %d entries in memory", svc.trafficEntries.Len())
		}
		time.Sleep(10 * time.Millisecond)
	}
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("at %d entries: HeapAlloc %.1f MiB, HeapInuse %.1f MiB, HeapObjects %d",
		svc.GetStatistics().Total, float64(stats.HeapAlloc)/(1<<20), float64(stats.HeapInuse)/(1<<20), stats.HeapObjects)
	return stats.HeapAlloc
}
