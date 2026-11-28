package store

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/serviceruntime"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
)

// countingTopic is a service topic that counts its subscribers.
type countingTopic[T any] struct {
	serviceruntime.Topic[T]

	mu          sync.Mutex
	subscribers int
}

func (c *countingTopic[T]) Subscribe(fn func(T)) func() {
	c.mu.Lock()
	c.subscribers++
	c.mu.Unlock()
	off := c.Topic.Subscribe(fn)
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.subscribers--
			c.mu.Unlock()
			off()
		})
	}
}

func (c *countingTopic[T]) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subscribers
}

type fakeProxyStatus struct{ ProxyBackend }

func (fakeProxyStatus) GetStatus() proxyservice.ProxyStatus { return proxyservice.ProxyStatus{} }
func (fakeProxyStatus) GetSystemProxyStatus() proxyservice.SystemProxyStatus {
	return proxyservice.SystemProxyStatus{}
}
func (fakeProxyStatus) GetDNSCacheStatus() proxyservice.DNSCacheStatus {
	return proxyservice.DNSCacheStatus{}
}

func TestProxyStoreFollowsTypedTopicsOnTheMainThread(t *testing.T) {
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	store := NewProxyStore(rt, fakeProxyStatus{})
	var events proxyservice.Events
	store.Initialize(&events)

	events.Status.Publish(proxyservice.ProxyStatus{Revision: 2, Running: true, Address: "127.0.0.1:9090"})
	events.SystemProxy.Publish(proxyservice.SystemProxyStatus{Supported: true, Active: true})
	events.DNSCache.Publish(proxyservice.DNSCacheStatus{EntryCount: 3})
	if store.Status.Running || store.SystemProxy.Active || store.DNSCache.EntryCount != 0 {
		t.Fatal("topic values were applied off the main thread")
	}
	window.Drain()
	if !store.Status.Running || !store.SystemProxy.Active || store.DNSCache.EntryCount != 3 {
		t.Fatalf("status = %+v, system = %+v, dns = %+v", store.Status, store.SystemProxy, store.DNSCache)
	}

	store.Cleanup()
	events.Status.Publish(proxyservice.ProxyStatus{Revision: 3})
	window.Drain()
	if !store.Status.Running {
		t.Fatal("a status after Cleanup was applied")
	}
}

type fakeMemStats struct{ MemStatsBackend }

func (fakeMemStats) GetMonitoringStatus() memstatsservice.MonitoringStatus {
	return memstatsservice.MonitoringStatus{}
}

func TestMemStatsStoreFollowsSamples(t *testing.T) {
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	store := NewMemStatsStore(rt, fakeMemStats{})
	samples := &countingTopic[memstatsservice.MemSnapshot]{}
	store.Initialize(samples, rt.Dispatcher.Post)
	store.Initialize(samples, rt.Dispatcher.Post)
	if samples.count() != 1 {
		t.Fatalf("subscribers = %d", samples.count())
	}
	samples.Publish(memstatsservice.MemSnapshot{Timestamp: 7})
	rt.Jobs.Wait()
	window.Drain()
	if store.Latest == nil || store.Latest.Timestamp != 7 {
		t.Fatalf("latest = %+v", store.Latest)
	}
	store.Cleanup()
	if samples.count() != 0 {
		t.Fatalf("subscribers after Cleanup = %d", samples.count())
	}
}

func TestRewriteStorePostsChangesToTheMainThread(t *testing.T) {
	h := newRewriteHarness(t)
	h.store.Initialize()
	h.await("load", func() bool { return h.store.State.Revision == 1 })
	next := h.backend.server_()
	next.Revision = 2
	h.backend.setServer(next)
	h.changes.Publish(rewriteservice.Changed{Revision: 2})
	h.await("change", func() bool { return h.store.State.Revision == 2 })
}

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
