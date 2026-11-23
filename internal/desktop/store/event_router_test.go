package store

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/josexy/flowlens/internal/serviceruntime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// heldPoster stands in for a busy main thread: it keeps posted functions
// until the test runs them.
type heldPoster struct {
	mu   sync.Mutex
	held []func()
}

func (p *heldPoster) post(fn func()) {
	p.mu.Lock()
	p.held = append(p.held, fn)
	p.mu.Unlock()
}

func (p *heldPoster) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.held)
}

func (p *heldPoster) waitFor(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for p.count() < count {
		if time.Now().After(deadline) {
			t.Fatalf("posted batches = %d, want %d", p.count(), count)
		}
		time.Sleep(time.Millisecond)
	}
}

func (p *heldPoster) runNext() {
	p.mu.Lock()
	fn := p.held[0]
	p.held = p.held[1:]
	p.mu.Unlock()
	fn()
}

func TestConnectKeepsAtMostOneBatchWaitingForTheMainThread(t *testing.T) {
	synctest.Test(t, testConnectKeepsAtMostOneBatchWaitingForTheMainThread)
}

func testConnectKeepsAtMostOneBatchWaitingForTheMainThread(t *testing.T) {
	h := newTrafficHarness(t, nil)
	var live serviceruntime.Topic[proxyservice.LiveBatch]
	poster := &heldPoster{}
	off := h.router.Connect(&live, poster.post)
	defer off()
	publisher := proxyservice.NewLivePublisher(&live)
	defer publisher.Close()

	if err := publisher.Publish(proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	poster.waitFor(t, 1)

	// The main thread has not applied the first batch. A burst larger than
	// the pending bound is held back, and its excess is dropped.
	var dropped int
	for id := uint64(2); id <= 2000; id++ {
		if err := publisher.Publish(proxyservice.TrafficEntryEvent{Entry: trafficEntry(id)}); err != nil {
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatal("burst beyond the pending bound dropped nothing")
	}
	// The real router must keep the claim beyond the old two-second timeout.
	time.Sleep(10 * time.Second)
	if got := poster.count(); got != 1 {
		t.Fatalf("batches waiting for the main thread = %d, want 1", got)
	}

	h.backend.setSnapshot(entryRange(1, 2000))
	poster.runNext()
	poster.waitFor(t, 1)
	time.Sleep(50 * time.Millisecond)
	if got := poster.count(); got != 1 {
		t.Fatalf("batches waiting after the first release = %d, want 1", got)
	}

	// The second batch reports the drops, which recovers from the snapshot.
	poster.runNext()
	h.settle()
	h.tick()
	h.settle()
	requireIDs(t, h.store.Entries, idRange(1, 2000))
}
