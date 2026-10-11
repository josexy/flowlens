package proxyservice

import (
	"container/list"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	mitmproxy "github.com/josexy/mitmproxy-go/v2"
)

const dnsCacheCapacity = 1024

type DNSCacheStatus struct {
	Enabled    bool `json:"enabled"`
	TTLSeconds int  `json:"ttlSeconds"`
	EntryCount int  `json:"entryCount"`
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dnsCacheEntry struct {
	host      string
	addresses []net.IPAddr
	expires   time.Time
}

type dnsLookupKey struct {
	host       string
	generation uint64
}

type dnsLookup struct {
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	addresses []net.IPAddr
	err       error
}

// dnsCache owns only application-level results. The system resolver, hosts file,
// and remote proxy resolution keep their existing behavior.
type dnsCache struct {
	mu         sync.Mutex
	workers    sync.WaitGroup
	enabled    bool
	ttl        time.Duration
	generation uint64
	closed     bool
	capacity   int
	entries    map[string]*list.Element
	lru        *list.List
	lookups    map[dnsLookupKey]*dnsLookup
	resolver   ipResolver
	now        func() time.Time
}

func newDNSCache(resolver ipResolver) *dnsCache {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &dnsCache{
		ttl:      time.Duration(settingservice.DefaultDNSCacheTTLSeconds) * time.Second,
		capacity: dnsCacheCapacity,
		entries:  make(map[string]*list.Element), lru: list.New(),
		lookups: make(map[dnsLookupKey]*dnsLookup), resolver: resolver, now: time.Now,
	}
}

func (c *dnsCache) configure(enabled bool, ttlSeconds int) {
	if ttlSeconds < 1 || ttlSeconds > 3600 {
		ttlSeconds = settingservice.DefaultDNSCacheTTLSeconds
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := time.Duration(ttlSeconds) * time.Second
	if c.enabled != enabled || c.ttl != ttl {
		c.resetLocked()
		c.enabled, c.ttl = enabled, ttl
	}
}

func (c *dnsCache) resetLocked() {
	c.generation++
	clear(c.entries)
	c.lru.Init()
}

func (c *dnsCache) clear() DNSCacheStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetLocked()
	return c.statusLocked()
}

func (c *dnsCache) close() {
	c.mu.Lock()
	c.closed = true
	c.resetLocked()
	for _, lookup := range c.lookups {
		lookup.cancel()
	}
	c.mu.Unlock()
	c.workers.Wait()
}

func (c *dnsCache) status() DNSCacheStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *dnsCache) statusLocked() DNSCacheStatus {
	now := c.now()
	for host, element := range c.entries {
		if !now.Before(element.Value.(*dnsCacheEntry).expires) {
			delete(c.entries, host)
			c.lru.Remove(element)
		}
	}
	return DNSCacheStatus{Enabled: c.enabled, TTLSeconds: int(c.ttl / time.Second), EntryCount: len(c.entries)}
}

func cloneDNSAddresses(addresses []net.IPAddr) []net.IPAddr {
	result := make([]net.IPAddr, len(addresses))
	for i, address := range addresses {
		result[i] = net.IPAddr{IP: append(net.IP(nil), address.IP...), Zone: address.Zone}
	}
	return result
}

func (c *dnsCache) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	addresses, hit, err := c.lookupIPAddr(ctx, host)
	if hit {
		mitmproxy.ReportDNSCacheHit(ctx)
	}
	return addresses, err
}

func (c *dnsCache) lookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, false, errors.New("DNS cache is closed")
	}
	if !c.enabled {
		c.mu.Unlock()
		addresses, err := c.resolver.LookupIPAddr(ctx, host)
		return addresses, false, err
	}
	c.mu.Unlock()
	ascii, err := syntheticASCIIHostname(host)
	if err != nil {
		return nil, false, err
	}
	// Keep the trailing dot: absolute names and names using a search suffix
	// must not share records. Ports and address families do share IP results.
	host = strings.ToLower(ascii)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, false, errors.New("DNS cache is closed")
	}
	if !c.enabled {
		c.mu.Unlock()
		addresses, err := c.resolver.LookupIPAddr(ctx, host)
		return addresses, false, err
	}
	if element := c.entries[host]; element != nil {
		entry := element.Value.(*dnsCacheEntry)
		if c.now().Before(entry.expires) {
			c.lru.MoveToFront(element)
			result := cloneDNSAddresses(entry.addresses)
			c.mu.Unlock()
			return result, true, nil
		}
		delete(c.entries, host)
		c.lru.Remove(element)
	}
	key := dnsLookupKey{host: host, generation: c.generation}
	lookup := c.lookups[key]
	if lookup == nil {
		// Shared work has its own bounded context. An individual request can
		// leave without cancelling another request's lookup.
		lookupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		lookup = &dnsLookup{done: make(chan struct{}), cancel: cancel}
		c.lookups[key] = lookup
		c.workers.Add(1)
		go c.resolve(lookupCtx, key, lookup)
	}
	lookup.waiters++
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		lookup.waiters--
		if lookup.waiters == 0 {
			lookup.cancel()
			if c.lookups[key] == lookup {
				delete(c.lookups, key)
			}
		}
		c.mu.Unlock()
		return nil, false, ctx.Err()
	case <-lookup.done:
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		return cloneDNSAddresses(lookup.addresses), false, lookup.err
	}
}

func (c *dnsCache) resolve(ctx context.Context, key dnsLookupKey, lookup *dnsLookup) {
	defer c.workers.Done()
	defer lookup.cancel()
	addresses, err := c.resolver.LookupIPAddr(ctx, key.host)
	c.mu.Lock()
	defer c.mu.Unlock()
	lookup.addresses, lookup.err = cloneDNSAddresses(addresses), err
	if c.lookups[key] == lookup {
		delete(c.lookups, key)
		if err == nil && ctx.Err() == nil && len(addresses) > 0 && !c.closed && c.enabled && key.generation == c.generation {
			entry := &dnsCacheEntry{host: key.host, addresses: cloneDNSAddresses(addresses), expires: c.now().Add(c.ttl)}
			c.entries[key.host] = c.lru.PushFront(entry)
			if c.lru.Len() > c.capacity {
				oldest := c.lru.Back()
				delete(c.entries, oldest.Value.(*dnsCacheEntry).host)
				c.lru.Remove(oldest)
			}
		}
	}
	close(lookup.done)
}

func (s *ProxyService) dnsCacheManager() *dnsCache {
	s.dnsCacheMu.Lock()
	defer s.dnsCacheMu.Unlock()
	if s.dnsCache == nil {
		s.dnsCache = newDNSCache(nil)
		if cfg, err := s.getProxyConfig(); err == nil {
			s.dnsCache.configure(cfg.DNSCacheEnabled, cfg.DNSCacheTTLSeconds)
		}
		if s.dnsCacheClosed {
			s.dnsCache.close()
		}
	}
	return s.dnsCache
}

func (s *ProxyService) GetDNSCacheStatus() DNSCacheStatus {
	return s.dnsCacheManager().status()
}

func (s *ProxyService) ClearDNSCache() DNSCacheStatus {
	status := s.dnsCacheManager().clear()
	s.emitDNSCacheStatus(status)
	return status
}

func (s *ProxyService) emitDNSCacheStatus(status DNSCacheStatus) {
	if s.app != nil {
		s.app.Event.Emit("proxy:dns-cache-status", status)
	}
}
