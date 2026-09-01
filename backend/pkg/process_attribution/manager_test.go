package processattribution

import (
	"context"
	"image"
	"image/color"
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProvider struct {
	lookup func(context.Context, EndpointTuple) Result
	icon   func(context.Context, Result) (image.Image, error)
	calls  atomic.Int32
}

func (p *fakeProvider) Lookup(ctx context.Context, tuple EndpointTuple) Result {
	p.calls.Add(1)
	if p.lookup == nil {
		return Result{Status: StatusNotFound}
	}
	return p.lookup(ctx, tuple)
}

func (p *fakeProvider) LoadIcon(ctx context.Context, result Result) (image.Image, error) {
	if p.icon == nil {
		return nil, &IconUnavailableError{Reason: "test_icon_unavailable"}
	}
	return p.icon(ctx, result)
}

func TestNormalizeEndpointTupleIPv4MappedIPv6(t *testing.T) {
	tuple := EndpointTuple{
		Client: netip.MustParseAddrPort("[::ffff:127.0.0.1]:43120"),
		Proxy:  netip.MustParseAddrPort("[::ffff:127.0.0.1]:8080"),
	}

	got := normalizeEndpointTuple(tuple)
	if got.Client != netip.MustParseAddrPort("127.0.0.1:43120") {
		t.Fatalf("Client = %s, want 127.0.0.1:43120", got.Client)
	}
	if got.Proxy != netip.MustParseAddrPort("127.0.0.1:8080") {
		t.Fatalf("Proxy = %s, want 127.0.0.1:8080", got.Proxy)
	}
}

func TestDefaultOptionsLockedValues(t *testing.T) {
	got := DefaultOptions()
	wantWorkers := min(4, max(1, runtime.GOMAXPROCS(0)))
	if got.Workers != wantWorkers ||
		got.QueueSize != 256 ||
		got.LookupTimeout != time.Second ||
		got.SocketSnapshotTTL != 100*time.Millisecond ||
		got.ProcessCacheSize != 1024 ||
		got.ProcessCacheTTL != 5*time.Minute ||
		got.NegativeCacheTTL != time.Second ||
		got.IconWorkers != 2 ||
		got.IconQueueSize != 128 {
		t.Fatalf("DefaultOptions() = %+v", got)
	}
}

func TestManagerStartDoesNotWaitForProvider(t *testing.T) {
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		close(providerStarted)
		select {
		case <-releaseProvider:
			return resolvedResult(100, "start-a", "test")
		case <-ctx.Done():
			return Result{Status: StatusNotFound, Reason: ctx.Err().Error()}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Second})

	startedAt := time.Now()
	lookup := manager.Start(context.Background(), testTuple(41001))
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("Start blocked for %s", elapsed)
	}
	if got := lookup.Snapshot().Status; got != StatusPending {
		t.Fatalf("initial status = %q, want %q", got, StatusPending)
	}
	waitClosed(t, providerStarted, "provider start")
	close(releaseProvider)
	waitForStatus(t, lookup, StatusResolved)
}

func TestManagerDeduplicatesConcurrentTupleLookups(t *testing.T) {
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		close(providerStarted)
		select {
		case <-releaseProvider:
			return resolvedResult(101, "start-a", "deduplicated")
		case <-ctx.Done():
			return Result{Status: StatusNotFound, Reason: ctx.Err().Error()}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 2, LookupTimeout: time.Second})
	tuple := testTuple(41002)

	first := manager.Start(context.Background(), tuple)
	waitClosed(t, providerStarted, "provider start")
	second := manager.Start(context.Background(), tuple)
	close(releaseProvider)
	waitForStatus(t, first, StatusResolved)
	waitForStatus(t, second, StatusResolved)
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
}

func TestManagerPublishesTextBeforeIcon(t *testing.T) {
	lookupStarted := make(chan struct{})
	releaseLookup := make(chan struct{})
	iconStarted := make(chan struct{})
	releaseIcon := make(chan struct{})
	provider := &fakeProvider{
		lookup: func(ctx context.Context, _ EndpointTuple) Result {
			close(lookupStarted)
			select {
			case <-releaseLookup:
				return resolvedResult(102, "start-a", "with-icon")
			case <-ctx.Done():
				return Result{Status: StatusNotFound, Reason: ctx.Err().Error()}
			}
		},
		icon: func(ctx context.Context, _ Result) (image.Image, error) {
			close(iconStarted)
			select {
			case <-releaseIcon:
				return deterministicIcon(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	manager := newTestManager(t, provider, store, Options{
		Workers: 1, QueueSize: 1, LookupTimeout: time.Second, IconWorkers: 1, IconQueueSize: 1,
	})
	tuple := testTuple(41003)
	lookup := manager.Start(context.Background(), tuple)
	updates := make(chan Result, 2)
	off := lookup.Subscribe(func(result Result) { updates <- result })
	defer off()

	waitClosed(t, lookupStarted, "lookup start")
	close(releaseLookup)
	textResult := waitResult(t, updates)
	if textResult.Status != StatusResolved || textResult.IconKey != "" {
		t.Fatalf("first update = %+v, want resolved text without icon", textResult)
	}
	waitClosed(t, iconStarted, "icon start")
	secondLookup := manager.Start(context.Background(), tuple)
	if secondSnapshot := secondLookup.Snapshot(); secondSnapshot.Status != StatusResolved || secondSnapshot.IconKey != "" {
		t.Fatalf("deduplicated icon-pending snapshot = %+v", secondSnapshot)
	}
	secondUpdates := make(chan Result, 1)
	secondOff := secondLookup.Subscribe(func(result Result) { secondUpdates <- result })
	defer secondOff()
	close(releaseIcon)
	iconResult := waitResult(t, updates)
	if iconResult.Status != StatusResolved || iconResult.IconKey == "" {
		t.Fatalf("second update = %+v, want resolved result with icon", iconResult)
	}
	secondIconResult := waitResult(t, secondUpdates)
	if secondIconResult.IconKey != iconResult.IconKey {
		t.Fatalf("deduplicated icon key = %q, want %q", secondIconResult.IconKey, iconResult.IconKey)
	}
}

func TestManagerDeduplicatesProcessIconWorkAcrossTuples(t *testing.T) {
	const lookupCount = 8
	iconStarted := make(chan struct{})
	releaseIcon := make(chan struct{})
	var iconStartOnce sync.Once
	var iconCalls atomic.Int32
	provider := &fakeProvider{
		lookup: func(context.Context, EndpointTuple) Result {
			return resolvedResult(103, "shared-start", "shared-process")
		},
		icon: func(ctx context.Context, _ Result) (image.Image, error) {
			iconCalls.Add(1)
			iconStartOnce.Do(func() { close(iconStarted) })
			select {
			case <-releaseIcon:
				return deterministicIcon(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	manager := newTestManager(t, provider, store, Options{
		Workers: lookupCount, QueueSize: lookupCount, LookupTimeout: time.Second,
		IconWorkers: 2, IconQueueSize: 1,
	})
	lookups := make([]*Lookup, 0, lookupCount)
	iconCallbacks := make([]atomic.Int32, lookupCount)
	for index := range lookupCount {
		lookup := manager.Start(context.Background(), testTuple(uint16(41200+index)))
		lookup.Subscribe(func(result Result) {
			if result.IconKey != "" {
				iconCallbacks[index].Add(1)
			}
		})
		lookups = append(lookups, lookup)
	}
	waitClosed(t, iconStarted, "shared icon start")
	deadline := time.Now().Add(time.Second)
	for provider.calls.Load() < lookupCount && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls := provider.calls.Load(); calls != lookupCount {
		t.Fatalf("provider lookup calls = %d, want %d", calls, lookupCount)
	}
	close(releaseIcon)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	manager.Drain(drainCtx)
	cancel()

	if calls := iconCalls.Load(); calls != 1 {
		t.Fatalf("process icon calls = %d, want 1", calls)
	}
	for index, lookup := range lookups {
		if result := lookup.Snapshot(); result.IconKey == "" {
			t.Fatalf("lookup %d result = %+v, want shared icon", index, result)
		}
		if calls := iconCallbacks[index].Load(); calls != 1 {
			t.Fatalf("lookup %d icon callbacks = %d, want 1", index, calls)
		}
	}
}

func TestManagerTimesOutWithoutBlockingSubscribers(t *testing.T) {
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		<-ctx.Done()
		return Result{Status: StatusNotFound, Reason: "provider_cancelled"}
	}}
	manager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: 25 * time.Millisecond})
	lookup := manager.Start(context.Background(), testTuple(41004))
	updates := make(chan Result, 1)
	off := lookup.Subscribe(func(result Result) { updates <- result })
	defer off()

	result := waitResult(t, updates)
	if result.Status != StatusNotFound || result.Reason != "lookup_timeout" {
		t.Fatalf("timeout update = %+v", result)
	}
}

func TestManagerBoundsTimedOutProviderCalls(t *testing.T) {
	releaseProvider := make(chan struct{})
	t.Cleanup(func() { close(releaseProvider) })
	provider := &fakeProvider{lookup: func(context.Context, EndpointTuple) Result {
		<-releaseProvider
		return Result{Status: StatusNotFound, Reason: "released"}
	}}
	manager := newTestManager(t, provider, nil, Options{
		Workers: 1, QueueSize: 4, LookupTimeout: 10 * time.Millisecond,
	})

	lookups := make([]*Lookup, 0, 3)
	for index := range 3 {
		lookups = append(lookups, manager.Start(context.Background(), testTuple(uint16(41100+index))))
	}
	for _, lookup := range lookups {
		result := waitForStatus(t, lookup, StatusNotFound)
		if result.Reason != "lookup_timeout" {
			t.Fatalf("timeout result = %+v, want lookup_timeout", result)
		}
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after timeouts = %d, want bounded to 1", calls)
	}
}

func TestManagerBoundsTimedOutIconCalls(t *testing.T) {
	releaseIcon := make(chan struct{})
	t.Cleanup(func() { close(releaseIcon) })
	var iconCalls atomic.Int32
	provider := &fakeProvider{
		lookup: func(_ context.Context, tuple EndpointTuple) Result {
			return resolvedResult(uint32(tuple.Client.Port()), strconv.Itoa(int(tuple.Client.Port())), "icon-timeout")
		},
		icon: func(context.Context, Result) (image.Image, error) {
			iconCalls.Add(1)
			<-releaseIcon
			return nil, &IconUnavailableError{Reason: "released"}
		},
	}
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	manager := newTestManager(t, provider, store, Options{
		Workers: 1, QueueSize: 4, LookupTimeout: 10 * time.Millisecond,
		IconWorkers: 1, IconQueueSize: 4,
	})

	for index := range 3 {
		manager.Start(context.Background(), testTuple(uint16(41110+index)))
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	manager.Drain(drainCtx)
	cancel()
	if calls := iconCalls.Load(); calls != 1 {
		t.Fatalf("icon calls after timeouts = %d, want bounded to 1", calls)
	}
}

func TestManagerDrainRejectsNewLookups(t *testing.T) {
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		select {
		case <-providerStarted:
		default:
			close(providerStarted)
		}
		select {
		case <-releaseProvider:
			return Result{Status: StatusNotFound, Reason: "released"}
		case <-ctx.Done():
			return Result{Status: StatusNotFound, Reason: "cancelled"}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{
		Workers: 1, QueueSize: 2, LookupTimeout: time.Second,
	})

	first := manager.Start(context.Background(), testTuple(41120))
	waitClosed(t, providerStarted, "provider start")
	drainDone := make(chan struct{})
	go func() {
		manager.Drain(context.Background())
		close(drainDone)
	}()
	waitForManagerDrain(t, manager)

	late := manager.Start(context.Background(), testTuple(41121))
	if result := late.Snapshot(); result.Status != StatusNotFound || result.Reason != "manager_closed" {
		t.Fatalf("lookup started during drain = %+v, want manager_closed", result)
	}
	close(releaseProvider)
	waitForStatus(t, first, StatusNotFound)
	waitClosed(t, drainDone, "manager drain")
}

func TestManagerBoundsTupleCache(t *testing.T) {
	provider := &fakeProvider{lookup: func(context.Context, EndpointTuple) Result {
		return Result{Status: StatusNotFound, Reason: "not_found"}
	}}
	options := Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Second}
	manager := newTestManager(t, provider, nil, options)

	for index := range 300 {
		lookup := manager.Start(context.Background(), testTuple(uint16(41200+index)))
		waitForStatus(t, lookup, StatusNotFound)
	}
	manager.mu.Lock()
	cacheSize := len(manager.tupleCache)
	manager.mu.Unlock()
	const wantMaximum = 256
	if cacheSize > wantMaximum {
		t.Fatalf("tuple cache size = %d, want at most %d", cacheSize, wantMaximum)
	}
}

func TestManagerRejectsWorkWhenQueueIsFull(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	var startOnce sync.Once
	provider := &fakeProvider{lookup: func(ctx context.Context, tuple EndpointTuple) Result {
		startOnce.Do(func() { close(firstStarted) })
		select {
		case <-releaseProvider:
			return resolvedResult(uint32(tuple.Client.Port()), "start", "queued")
		case <-ctx.Done():
			return Result{Status: StatusNotFound, Reason: ctx.Err().Error()}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Second})

	first := manager.Start(context.Background(), testTuple(41005))
	waitClosed(t, firstStarted, "first provider call")
	second := manager.Start(context.Background(), testTuple(41006))
	third := manager.Start(context.Background(), testTuple(41007))
	if got := third.Snapshot(); got.Status != StatusNotFound || got.Reason != "queue_full" {
		t.Fatalf("queue-full snapshot = %+v", got)
	}

	close(releaseProvider)
	waitForStatus(t, first, StatusResolved)
	waitForStatus(t, second, StatusResolved)
}

func TestProcessCacheKeyIncludesStartToken(t *testing.T) {
	provider := &fakeProvider{lookup: func(_ context.Context, tuple EndpointTuple) Result {
		switch tuple.Client.Port() {
		case 41008:
			return resolvedResult(200, "start-a", "first identity")
		case 41009:
			return Result{Status: StatusResolved, PID: 200, StartToken: "start-a", Source: "test", IdentityConfidence: "none"}
		case 41010:
			return resolvedResult(200, "start-b", "reused pid identity")
		default:
			return Result{Status: StatusNotFound}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{
		Workers: 1, QueueSize: 3, LookupTimeout: time.Second, ProcessCacheSize: 8, ProcessCacheTTL: time.Minute,
	})

	first := manager.Start(context.Background(), testTuple(41008))
	waitForStatus(t, first, StatusResolved)
	second := manager.Start(context.Background(), testTuple(41009))
	waitForStatus(t, second, StatusResolved)
	if got := second.Snapshot().DisplayName; got != "first identity" {
		t.Fatalf("same-process cached DisplayName = %q, want %q", got, "first identity")
	}
	third := manager.Start(context.Background(), testTuple(41010))
	waitForStatus(t, third, StatusResolved)
	if got := third.Snapshot().DisplayName; got != "reused pid identity" {
		t.Fatalf("PID-reuse DisplayName = %q, want %q", got, "reused pid identity")
	}
}

func TestLookupUnsubscribeAndReleaseAreIdempotent(t *testing.T) {
	releaseProvider := make(chan struct{})
	provider := &fakeProvider{lookup: func(ctx context.Context, _ EndpointTuple) Result {
		select {
		case <-releaseProvider:
			return resolvedResult(300, "start", "released")
		case <-ctx.Done():
			return Result{Status: StatusNotFound, Reason: ctx.Err().Error()}
		}
	}}
	manager := newTestManager(t, provider, nil, Options{Workers: 1, QueueSize: 1, LookupTimeout: time.Second})
	lookup := manager.Start(context.Background(), testTuple(41011))
	var calls atomic.Int32
	off := lookup.Subscribe(func(Result) { calls.Add(1) })
	off()
	off()
	lookup.Release()
	lookup.Release()
	close(releaseProvider)
	waitForStatus(t, lookup, StatusResolved)
	if got := calls.Load(); got != 0 {
		t.Fatalf("unsubscribed callback calls = %d, want 0", got)
	}
}

func TestLookupRetainKeepsSubscriptionsIndependent(t *testing.T) {
	state := &lookupState{
		result:      Result{Status: StatusPending},
		subscribers: make(map[uint64]func(Result)),
		refs:        1,
	}
	original := newLookup(state)
	retained := original.Retain()
	if retained == nil {
		t.Fatal("Retain returned nil for active lookup")
	}
	var calls atomic.Int32
	retained.Subscribe(func(Result) { calls.Add(1) })

	original.Release()
	state.complete(Result{Status: StatusResolved}, true)
	if got := calls.Load(); got != 1 {
		t.Fatalf("retained subscription calls = %d, want 1", got)
	}
	retained.Release()
}

func TestLookupSubscribeAndReleaseAreConcurrencySafe(t *testing.T) {
	for range 1000 {
		state := &lookupState{
			result:      Result{Status: StatusPending},
			subscribers: make(map[uint64]func(Result)),
			refs:        1,
		}
		lookup := newLookup(state)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			off := lookup.Subscribe(func(Result) {})
			off()
		}()
		go func() {
			defer wg.Done()
			<-start
			lookup.Release()
		}()
		close(start)
		state.complete(Result{Status: StatusResolved}, true)
		wg.Wait()
	}
}

func newTestManager(t *testing.T, provider Provider, store *IconStore, options Options) *Manager {
	t.Helper()
	manager := NewManager(provider, store, options)
	t.Cleanup(manager.Close)
	return manager
}

func testTuple(clientPort uint16) EndpointTuple {
	return EndpointTuple{
		Client: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), clientPort),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
}

func resolvedResult(pid uint32, startToken, displayName string) Result {
	return Result{
		Status:             StatusResolved,
		PID:                pid,
		StartToken:         startToken,
		DisplayName:        displayName,
		ProcessName:        "test-process",
		ExecutablePath:     "C:/test/test-process.exe",
		Source:             "test",
		IdentityConfidence: "exact",
	}
}

func deterministicIcon() image.Image {
	icon := image.NewRGBA(image.Rect(0, 0, 2, 2))
	icon.Set(0, 0, color.NRGBA{R: 255, A: 255})
	icon.Set(1, 0, color.NRGBA{G: 255, A: 255})
	icon.Set(0, 1, color.NRGBA{B: 255, A: 255})
	icon.Set(1, 1, color.NRGBA{R: 255, G: 255, A: 128})
	return icon
}

func waitClosed(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitResult(t *testing.T, updates <-chan Result) Result {
	t.Helper()
	select {
	case result := <-updates:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for attribution result")
		return Result{}
	}
}

func waitForStatus(t *testing.T, lookup *Lookup, status Status) Result {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result := lookup.Snapshot()
		if result.Status == status {
			return result
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q; final snapshot: %+v", status, lookup.Snapshot())
	return Result{}
}

func waitForManagerDrain(t *testing.T, manager *Manager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		draining := manager.draining
		manager.mu.Unlock()
		if draining {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for manager drain to start")
}
