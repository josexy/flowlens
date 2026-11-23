package store

import (
	"sync"
	"testing"

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
