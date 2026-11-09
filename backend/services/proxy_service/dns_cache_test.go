package proxyservice

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type dnsResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f dnsResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func dnsTestIPs(address string) []net.IPAddr { return []net.IPAddr{{IP: net.ParseIP(address)}} }

func lookupDNS(t *testing.T, cache *dnsCache, host string) []net.IPAddr {
	t.Helper()
	ips, err := cache.LookupIPAddr(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	return ips
}

func TestDNSCacheDefaultsNormalizationExpiryAndCopies(t *testing.T) {
	var calls, clock atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		calls.Add(1)
		return dnsTestIPs("127.0.0.1"), nil
	}))
	t.Cleanup(cache.close)
	cache.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	if status := cache.status(); status.Enabled || status.TTLSeconds != 60 || status.EntryCount != 0 {
		t.Fatalf("defaults: %+v", status)
	}
	lookupDNS(t, cache, "example.test")
	lookupDNS(t, cache, "example.test")
	if calls.Load() != 2 || cache.status().EntryCount != 0 {
		t.Fatal("disabled cache reused results")
	}
	cache.configure(true, 60)
	ips := lookupDNS(t, cache, "BÜCHER.test")
	ips[0].IP[0] = 99
	if got := lookupDNS(t, cache, "xn--bcher-kva.test"); got[0].IP.String() != "127.0.0.1" {
		t.Fatalf("caller mutated stored result: %v", got)
	}
	if calls.Load() != 3 {
		t.Fatal("IDNA/case normalization did not share results")
	}
	lookupDNS(t, cache, "xn--bcher-kva.test.")
	if calls.Load() != 4 {
		t.Fatal("absolute/search names incorrectly shared a cache entry")
	}
	clock.Store(60)
	if cache.status().EntryCount != 0 {
		t.Fatal("expired entries counted")
	}
	lookupDNS(t, cache, "xn--bcher-kva.test")
	if calls.Load() != 5 {
		t.Fatal("expired result reused")
	}
}

func TestDNSCacheLRUAndConfigInvalidation(t *testing.T) {
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) { calls.Add(1); return dnsTestIPs("127.0.0.1"), nil }))
	t.Cleanup(cache.close)
	cache.capacity = 2
	cache.configure(true, 60)
	for _, host := range []string{"a.test", "b.test", "a.test", "c.test", "a.test", "b.test"} {
		lookupDNS(t, cache, host)
	}
	if calls.Load() != 4 || cache.status().EntryCount != 2 {
		t.Fatal("LRU evicted a recently used entry")
	}
	cache.configure(true, 120)
	if cache.status().EntryCount != 0 {
		t.Fatal("TTL change retained records")
	}
	lookupDNS(t, cache, "a.test")
	cache.configure(false, 120)
	if cache.status().EntryCount != 0 {
		t.Fatal("disabling retained records")
	}
	cache.configure(true, 120)
	lookupDNS(t, cache, "a.test")
	if status := cache.clear(); status.EntryCount != 0 || !status.Enabled || status.TTLSeconds != 120 {
		t.Fatalf("clear changed configuration: %+v", status)
	}
}

func TestDNSCacheReportsOnlyCompletedEntryHits(t *testing.T) {
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return dnsTestIPs("127.0.0.1"), nil
	}))
	t.Cleanup(cache.close)
	var clock atomic.Int64
	cache.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	cache.configure(true, 60)
	for _, step := range []struct {
		name   string
		before func()
		hit    bool
	}{
		{name: "first lookup"},
		{name: "cached lookup", hit: true},
		{name: "expired lookup", before: func() { clock.Store(60) }},
		{name: "cleared lookup", before: func() { cache.clear() }},
		{name: "disabled lookup", before: func() { cache.configure(false, 60) }},
	} {
		t.Run(step.name, func(t *testing.T) {
			if step.before != nil {
				step.before()
			}
			_, hit, err := cache.lookupIPAddr(context.Background(), "cached.test")
			if err != nil || hit != step.hit {
				t.Fatalf("cache hit = %t, error = %v; want hit = %t", hit, err, step.hit)
			}
		})
	}
}

func waitDNSWaiters(t *testing.T, cache *dnsCache, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		found := false
		for _, call := range cache.lookups {
			found = found || call.waiters == count
		}
		cache.mu.Unlock()
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("lookup waiters did not join")
}

func TestDNSCacheCoalescesAndCancellationIsIndependent(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return dnsTestIPs("127.0.0.1"), nil
		}
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	ctx, cancel := context.WithCancel(context.Background())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := cache.LookupIPAddr(ctx, "shared.test"); first <- err }()
	<-started
	go func() {
		_, hit, err := cache.lookupIPAddr(context.Background(), "shared.test")
		if hit {
			err = errors.New("in-flight shared lookup was reported as a cache hit")
		}
		second <- err
	}()
	waitDNSWaiters(t, cache, 2)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || cache.status().EntryCount != 1 {
		t.Fatal("shared lookup was cancelled or duplicated")
	}
}

func TestDNSCacheOldLookupsCannotRefillAfterInvalidation(t *testing.T) {
	for _, operation := range []string{"clear", "disable", "ttl"} {
		t.Run(operation, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int64
			cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
					return dnsTestIPs("127.0.0.1"), nil
				}
				return dnsTestIPs("127.0.0.2"), nil
			}))
			t.Cleanup(cache.close)
			cache.configure(true, 60)
			finished := make(chan struct{})
			go func() { _, _ = cache.LookupIPAddr(context.Background(), "stale.test"); close(finished) }()
			<-started
			switch operation {
			case "clear":
				cache.clear()
			case "disable":
				cache.configure(false, 60)
				cache.configure(true, 60)
			case "ttl":
				cache.configure(true, 120)
			}
			lookupDNS(t, cache, "stale.test")
			close(release)
			<-finished
			if got := lookupDNS(t, cache, "stale.test"); got[0].IP.String() != "127.0.0.2" || calls.Load() != 2 {
				t.Fatalf("old lookup refilled cache: %v (%d calls)", got, calls.Load())
			}
		})
	}
}

func TestDNSCacheErrorsEmptyResultsAndShutdown(t *testing.T) {
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		if calls.Add(1) <= 2 {
			return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
		}
		return nil, nil
	}))
	cache.configure(true, 60)
	for range 4 {
		_, _ = cache.LookupIPAddr(context.Background(), "missing.test")
	}
	if calls.Load() != 4 || cache.status().EntryCount != 0 {
		t.Fatal("errors/empty results were cached")
	}
	cache.close()
	if _, err := cache.LookupIPAddr(context.Background(), "missing.test"); err == nil {
		t.Fatal("closed cache accepted a lookup")
	}

	started, cancelled := make(chan struct{}), make(chan struct{})
	cache = newDNSCache(dnsResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}))
	cache.configure(true, 60)
	finished := make(chan error, 1)
	go func() { _, err := cache.LookupIPAddr(context.Background(), "shutdown.test"); finished <- err }()
	<-started
	cache.close()
	<-cancelled
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown lookup: %v", err)
	}
}

func TestDNSCacheCancelsWorkWhenLastWaiterLeaves(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return dnsTestIPs("127.0.0.1"), nil
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := cache.LookupIPAddr(ctx, "cancel.test"); finished <- err }()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-cancelled
	lookupDNS(t, cache, "cancel.test")
	if calls.Load() != 2 || cache.status().EntryCount != 1 {
		t.Fatal("new request joined cancelled work")
	}
}

func TestDNSCacheAppliesWhileProxyStopped(t *testing.T) {
	settings := settingservice.New(nil)
	if err := settings.Update(&settingservice.Settings{ProxyConfig: &settingservice.ProxyConfig{DNSCacheEnabled: true, DNSCacheTTLSeconds: 120}}); err != nil {
		t.Fatal(err)
	}
	svc := New(settings)
	cache := svc.dnsCacheManager()
	t.Cleanup(cache.close)
	if !svc.GetDNSCacheStatus().Enabled {
		t.Fatal("initial configuration was not loaded")
	}
	if err := settings.Update(&settingservice.Settings{ProxyConfig: &settingservice.ProxyConfig{DNSCacheEnabled: false, DNSCacheTTLSeconds: 90}}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ApplyCurrentProxyConfig()
	if err != nil || result.RestartRequired {
		t.Fatalf("apply: %+v %v", result, err)
	}
	if got := svc.GetDNSCacheStatus(); got.Enabled || got.TTLSeconds != 90 {
		t.Fatalf("stopped proxy did not apply DNS configuration: %+v", got)
	}
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	if svc.dnsCacheManager() != cache {
		t.Fatal("proxy stop replaced the application cache")
	}
}
