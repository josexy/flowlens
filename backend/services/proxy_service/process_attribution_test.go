package proxyservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/orderedmap"
	processattribution "github.com/josexy/flowlens/backend/pkg/process_attribution"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/mitmproxy-go/v2/metadata"
)

type proxyAttributionTestProvider struct {
	lookup func(context.Context, processattribution.EndpointTuple) processattribution.Result
	icon   func(context.Context, processattribution.Result) (image.Image, error)
	calls  atomic.Int32
}

func (p *proxyAttributionTestProvider) Lookup(ctx context.Context, tuple processattribution.EndpointTuple) processattribution.Result {
	p.calls.Add(1)
	if p.lookup == nil {
		return processattribution.Result{Status: processattribution.StatusNotFound}
	}
	return p.lookup(ctx, tuple)
}

func (p *proxyAttributionTestProvider) LoadIcon(ctx context.Context, result processattribution.Result) (image.Image, error) {
	if p.icon == nil {
		return nil, &processattribution.IconUnavailableError{Reason: "test_icon_unavailable"}
	}
	return p.icon(ctx, result)
}

func TestAttributionAcceptIsNonBlocking(t *testing.T) {
	settings := configureProcessAttributionSetting(t, true)
	providerStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	provider := &proxyAttributionTestProvider{lookup: func(ctx context.Context, _ processattribution.EndpointTuple) processattribution.Result {
		close(providerStarted)
		select {
		case <-releaseProvider:
			return proxyResolvedProcessResult(100)
		case <-ctx.Done():
			return processattribution.Result{Status: processattribution.StatusNotFound, Reason: "cancelled"}
		}
	}}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	service.settingService = settings
	conn := newAddressedTestConn(t, "127.0.0.1:43101", "127.0.0.1:8080")
	listener := &attributedListener{
		Listener: &singleConnListener{conn: conn},
		service:  service,
	}

	startedAt := time.Now()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 100*time.Millisecond {
		t.Fatalf("attributed Accept blocked for %s", elapsed)
	}
	attributed, ok := accepted.(*attributedConn)
	if !ok || attributed.binding == nil || attributed.binding.lookup == nil {
		t.Fatalf("Accept returned %T without lookup", accepted)
	}
	if got := attributed.binding.lookup.Snapshot().Status; got != processattribution.StatusPending {
		t.Fatalf("initial lookup status = %q, want pending", got)
	}
	waitForSignal(t, providerStarted, "provider start")
	close(releaseProvider)
	waitForProcessLookupStatus(t, attributed.binding.lookup, processattribution.StatusResolved)
}

func TestTrafficEntryUsesAcceptedConnectionTupleWithoutContextValue(t *testing.T) {
	settings := configureProcessAttributionSetting(t, true)
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(101)
		},
	}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	service.settingService = settings
	conn := service.attributeConnection(newAddressedTestConn(t, "127.0.0.1:43102", "127.0.0.1:8080"))
	t.Cleanup(func() { _ = conn.Close() })

	md := metadata.NewMD()
	md.SetLocalConnectionAddrInfo(metadata.ConnectionAddrInfo{
		SourceAddr:      netip.MustParseAddrPort("127.0.0.1:43102"),
		DestinationAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	})
	ctx := metadata.AppendToContext(context.Background(), md)
	entries := []*TrafficEntry{
		{ID: 1, Type: "https", Metadata: &Metadata{}},
		{ID: 2, Type: "https", Metadata: &Metadata{}},
	}
	for _, entry := range entries {
		service.fillEntryMetadataFromContext(ctx, entry)
		if entry.Metadata.Process == nil {
			t.Fatalf("entry %d process lookup was not recovered from accepted connection metadata", entry.ID)
		}
		service.storeTrafficEntry(entry)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.Drain(drainCtx)
	for _, entry := range entries {
		stored, ok := service.trafficEntries.Get(entry.ID)
		if !ok || stored.Metadata == nil || stored.Metadata.Process == nil {
			t.Fatalf("entry %d stored process is missing after lookup completion: %+v", entry.ID, stored)
		}
		if stored.Metadata.Process.PID != 101 || stored.Metadata.Process.ExecutablePath != "C:/test/flowlens-test.exe" {
			t.Fatalf("entry %d stored process = %+v, want resolved PID and executable path", entry.ID, stored.Metadata.Process)
		}
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls = %d, want one lookup shared by the connection", calls)
	}
}

func TestConnectionProcessLookupLifecycle(t *testing.T) {
	settings := configureProcessAttributionSetting(t, true)
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(102)
		},
	}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	service.settingService = settings
	tuple := proxyAttributionTuple(43103)

	first := service.attributeConnection(newAddressedTestConn(t, "127.0.0.1:43103", "127.0.0.1:8080"))
	attributed, ok := first.(*attributedConn)
	if !ok {
		t.Fatalf("attributed connection type = %T, want *attributedConn", first)
	}
	if _, loaded := service.connectionProcessLookups.Load(tuple); !loaded {
		t.Fatal("accepted connection lookup was not registered")
	}
	if err := attributed.Close(); err != nil {
		t.Fatalf("close attributed connection: %v", err)
	}
	if _, loaded := service.connectionProcessLookups.Load(tuple); loaded {
		t.Fatal("closed connection lookup remains registered")
	}

	second := service.attributeConnection(newAddressedTestConn(t, "127.0.0.1:43103", "127.0.0.1:8080"))
	reused, ok := second.(*attributedConn)
	if !ok {
		t.Fatalf("replacement attributed connection type = %T, want *attributedConn", second)
	}
	replacement := &connectionProcessLookup{
		lookup: processattribution.NewCompletedLookup(proxyResolvedProcessResult(103)),
	}
	service.connectionProcessLookups.Store(tuple, replacement)
	if err := reused.Close(); err != nil {
		t.Fatalf("close stale attributed connection: %v", err)
	}
	value, loaded := service.connectionProcessLookups.Load(tuple)
	if !loaded || value != replacement {
		t.Fatalf("stale close removed replacement lookup: loaded=%t value=%p replacement=%p", loaded, value, replacement)
	}
	service.connectionProcessLookups.Delete(tuple)
	replacement.lookup.Release()
}

func TestTrafficIconUpdateSurvivesConnectionLookupRelease(t *testing.T) {
	iconStarted := make(chan struct{})
	releaseIcon := make(chan struct{})
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(103)
		},
		icon: func(ctx context.Context, _ processattribution.Result) (image.Image, error) {
			close(iconStarted)
			select {
			case <-releaseIcon:
				icon := image.NewNRGBA(image.Rect(0, 0, 2, 2))
				icon.SetNRGBA(0, 0, color.NRGBA{R: 30, G: 120, B: 220, A: 255})
				return icon, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	store, err := processattribution.NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	manager := newProxyAttributionTestManager(t, provider, store)
	service := newProxyAttributionTestService(manager, store)
	tuple := proxyAttributionTuple(43104)
	connectionLookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, connectionLookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
	})

	entry := &TrafficEntry{ID: 44, Type: "http", Metadata: &Metadata{}}
	service.fillEntryProcessFromTuple(tuple, entry)
	service.storeTrafficEntry(entry)
	waitForSignal(t, iconStarted, "icon extraction")

	connectionLookup.Release()
	close(releaseIcon)
	deadline := time.Now().Add(2 * time.Second)
	for connectionLookup.Snapshot().IconKey == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if connectionLookup.Snapshot().IconKey == "" {
		t.Fatal("icon extraction did not complete")
	}

	stored, ok := service.trafficEntries.Get(entry.ID)
	if !ok || stored.Metadata == nil || stored.Metadata.Process == nil ||
		stored.Metadata.Process.IconKey == "" {
		t.Fatalf("stored process lost late icon update after connection release: %+v", stored)
	}
}

func TestAttributionUpdatesStoredAndSelectedTrafficBySameID(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("APPDATA", configDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	lookupStarted := make(chan struct{})
	releaseLookup := make(chan struct{})
	iconStarted := make(chan struct{})
	releaseIcon := make(chan struct{})
	provider := &proxyAttributionTestProvider{
		lookup: func(ctx context.Context, _ processattribution.EndpointTuple) processattribution.Result {
			close(lookupStarted)
			select {
			case <-releaseLookup:
				return proxyResolvedProcessResult(103)
			case <-ctx.Done():
				return processattribution.Result{Status: processattribution.StatusNotFound, Reason: "cancelled"}
			}
		},
		icon: func(ctx context.Context, _ processattribution.Result) (image.Image, error) {
			close(iconStarted)
			select {
			case <-releaseIcon:
				icon := image.NewNRGBA(image.Rect(0, 0, 2, 2))
				for y := range 2 {
					for x := range 2 {
						icon.SetNRGBA(x, y, color.NRGBA{R: 30, G: 120, B: 220, A: 255})
					}
				}
				return icon, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	store, err := processattribution.NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	manager := newProxyAttributionTestManager(t, provider, store)
	service := newProxyAttributionTestService(manager, store)
	tuple := proxyAttributionTuple(43104)
	lookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, lookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
		lookup.Release()
	})
	updates := make(chan TrafficEntryPatch, 4)
	service.emitTrafficPatchHook = func(patch TrafficEntryPatch) { updates <- patch }
	entry := &TrafficEntry{ID: 44, Type: "http", Metadata: &Metadata{}}
	service.fillEntryProcessFromTuple(tuple, entry)
	service.storeTrafficEntry(entry)
	service.emitTraffic(entry)
	initialGeneration := service.flushGeneration.Load()
	waitForSignal(t, lookupStarted, "lookup start")

	close(releaseLookup)
	textUpdate := waitForTrafficProcessPatch(t, updates, ProcessStatusResolved, false)
	if textUpdate.TrafficID != entry.ID {
		t.Fatalf("text update ID = %d, want %d", textUpdate.TrafficID, entry.ID)
	}
	textGeneration := service.flushGeneration.Load()
	if textGeneration <= initialGeneration {
		t.Fatalf("text update flush generation = %d, want > %d", textGeneration, initialGeneration)
	}
	waitForSignal(t, iconStarted, "icon start")
	close(releaseIcon)
	iconUpdate := waitForTrafficProcessPatch(t, updates, ProcessStatusResolved, true)
	if iconUpdate.TrafficID != entry.ID {
		t.Fatalf("icon update ID = %d, want %d", iconUpdate.TrafficID, entry.ID)
	}
	iconGeneration := service.flushGeneration.Load()
	if iconGeneration <= textGeneration {
		t.Fatalf("icon update flush generation = %d, want > %d", iconGeneration, textGeneration)
	}
	stored, ok := service.trafficEntries.Get(entry.ID)
	if !ok || stored.Metadata == nil || stored.Metadata.Process == nil || stored.Metadata.Process.IconKey == "" {
		t.Fatalf("stored process after icon update = %+v", stored)
	}
	if service.trafficEntries.Len() != 1 {
		t.Fatalf("traffic entry count = %d, want 1", service.trafficEntries.Len())
	}
	if got := service.GetStatistics().Total; got != 1 {
		t.Fatalf("traffic total = %d, want 1", got)
	}

	// The request handler still owns its original entry pointer. A later
	// response write must merge the binding's newest process snapshot instead
	// of restoring the original pending value.
	entry.StatusCode = 204
	entry.Status = "204 No Content"
	service.storeTrafficEntry(entry)
	stored, ok = service.trafficEntries.Get(entry.ID)
	if !ok || stored.StatusCode != 204 || stored.Metadata.Process.IconKey != iconUpdate.Process.IconKey {
		t.Fatalf("later traffic store regressed attribution: %+v", stored)
	}

	service.currentHistoryMetadata = HistoryMetadata{Key: "late-process-v1", CreatedAt: time.Now().UnixMilli()}
	if err := service.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flushHistoryToDisk: %v", err)
	}
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	hbin, err := os.Open(filepath.Join(historyDir, fs.GetHBinFileName("late-process-v1")))
	if err != nil {
		t.Fatalf("Open flushed hbin: %v", err)
	}
	defer hbin.Close()
	if _, err := DecodeHistoryMetadata(hbin); err != nil {
		t.Fatalf("DecodeHistoryMetadata: %v", err)
	}
	hidx, err := os.Open(filepath.Join(historyDir, fs.GetHIdxFileName("late-process-v1")))
	if err != nil {
		t.Fatalf("Open flushed hidx: %v", err)
	}
	defer hidx.Close()
	var count uint32
	var flushedID uint64
	var headerOffset, bodyOffset uint32
	for _, value := range []any{&count, &flushedID, &headerOffset, &bodyOffset} {
		if err := binary.Read(hidx, binary.BigEndian, value); err != nil {
			t.Fatalf("read flushed index: %v", err)
		}
	}
	if count != 1 || flushedID != entry.ID || bodyOffset <= headerOffset {
		t.Fatalf("flushed index = count %d id %d header %d body %d", count, flushedID, headerOffset, bodyOffset)
	}
	if _, err := hbin.Seek(int64(headerOffset), io.SeekStart); err != nil {
		t.Fatalf("Seek flushed header: %v", err)
	}
	flushed, err := DecodeTrafficEntry(hbin)
	if err != nil {
		t.Fatalf("DecodeTrafficEntry: %v", err)
	}
	if flushed.Metadata == nil || flushed.Metadata.Process == nil ||
		flushed.Metadata.Process.DisplayName != iconUpdate.Process.DisplayName ||
		flushed.Metadata.Process.IconKey != iconUpdate.Process.IconKey {
		t.Fatalf("flushed process = %+v, want latest %+v", flushed.Metadata.Process, iconUpdate.Process)
	}
}

func TestDeletedTrafficIgnoresLateAttribution(t *testing.T) {
	releaseLookup := make(chan struct{})
	provider := &proxyAttributionTestProvider{lookup: func(ctx context.Context, _ processattribution.EndpointTuple) processattribution.Result {
		select {
		case <-releaseLookup:
			return proxyResolvedProcessResult(104)
		case <-ctx.Done():
			return processattribution.Result{Status: processattribution.StatusNotFound, Reason: "cancelled"}
		}
	}}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	tuple := proxyAttributionTuple(43105)
	lookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, lookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
		lookup.Release()
	})
	entry := &TrafficEntry{ID: 45, Type: "http", Metadata: &Metadata{}}
	service.fillEntryProcessFromTuple(tuple, entry)
	service.storeTrafficEntry(entry)
	service.deleteTrafficEntry(entry.ID)
	updates := make(chan TrafficEntryPatch, 1)
	service.emitTrafficPatchHook = func(patch TrafficEntryPatch) { updates <- patch }

	close(releaseLookup)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.Drain(drainCtx)
	select {
	case update := <-updates:
		t.Fatalf("deleted traffic received late update: %+v", update)
	case <-time.After(50 * time.Millisecond):
	}
	if service.trafficEntries.Len() != 0 {
		t.Fatalf("traffic entry count = %d, want 0", service.trafficEntries.Len())
	}
}

func TestTrafficProcessUpdateEmitsOutsideAttributionLock(t *testing.T) {
	service := newProxyAttributionTestService(nil, nil)
	entry := &TrafficEntry{ID: 46, Type: "http", Metadata: &Metadata{}}
	service.storeTrafficEntry(entry)
	binding := &trafficProcessBinding{
		latest: mapProcessAttributionResult(proxyResolvedProcessResult(106)),
		active: true,
	}
	service.trafficProcessBindings.Store(entry.ID, binding)
	service.emitTrafficPatchHook = func(TrafficEntryPatch) {
		service.deleteTrafficEntry(entry.ID)
	}

	done := make(chan struct{})
	go func() {
		service.updateTrafficProcess(entry.ID, binding)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("traffic process update deadlocked while its event hook deleted the entry")
	}
	if _, ok := service.trafficEntries.Get(entry.ID); ok {
		t.Fatal("traffic event hook did not delete the entry")
	}
}

func TestTrafficProcessBindingSubscriptionDeactivationIsRaceSafe(t *testing.T) {
	for range 1000 {
		binding := &trafficProcessBinding{active: true}
		start := make(chan struct{})
		var offCalls atomic.Int32
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			off := func() { offCalls.Add(1) }
			if !binding.installOff(off) {
				off()
			}
		}()
		go func() {
			defer wait.Done()
			<-start
			if off := binding.deactivate(); off != nil {
				off()
			}
		}()
		close(start)
		wait.Wait()
		if calls := offCalls.Load(); calls != 1 {
			t.Fatalf("unsubscribe calls = %d, want exactly 1", calls)
		}
	}
}

func TestRemoteClientSkipsProviderLookup(t *testing.T) {
	settings := configureProcessAttributionSetting(t, true)
	provider := &proxyAttributionTestProvider{lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
		return proxyResolvedProcessResult(105)
	}}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	service.settingService = settings
	conn := newAddressedTestConn(t, "192.0.2.50:43106", "192.0.2.10:8080")

	attributed, ok := service.attributeConnection(conn).(*attributedConn)
	if !ok || attributed.binding == nil || attributed.binding.lookup == nil {
		t.Fatal("remote connection did not receive an attributed connection")
	}
	t.Cleanup(func() { _ = attributed.Close() })
	if got := attributed.binding.lookup.Snapshot().Status; got != processattribution.StatusRemote {
		t.Fatalf("remote lookup status = %q, want remote", got)
	}
	if calls := provider.calls.Load(); calls != 0 {
		t.Fatalf("remote provider calls = %d, want 0", calls)
	}
}

func TestLocalProcessAddressRefreshesSnapshotAfterMiss(t *testing.T) {
	service := newProxyAttributionTestService(nil, nil)
	var calls atomic.Int32
	service.localProcessAddressLoader = func() ([]net.Addr, error) {
		calls.Add(1)
		return []net.Addr{
			&net.IPNet{
				IP:   net.ParseIP("10.8.0.2"),
				Mask: net.CIDRMask(24, 32),
			},
		}, nil
	}

	if !service.isLocalProcessAddress(netip.MustParseAddr("10.8.0.2")) {
		t.Fatal("new local interface address was classified as remote")
	}
	if service.isLocalProcessAddress(netip.MustParseAddr("192.0.2.50")) {
		t.Fatal("unknown remote address was classified as local")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("interface address loader calls = %d, want 1 rate-limited refresh", got)
	}
}

func TestLocalProcessAddressRefreshPreservesLastGoodSnapshotOnError(t *testing.T) {
	service := newProxyAttributionTestService(nil, nil)
	localAddress := netip.MustParseAddr("10.8.0.2")
	service.localProcessAddresses[localAddress] = struct{}{}
	var calls atomic.Int32
	service.localProcessAddressLoader = func() ([]net.Addr, error) {
		calls.Add(1)
		return nil, errors.New("interface list unavailable")
	}

	if service.isLocalProcessAddress(netip.MustParseAddr("192.0.2.50")) {
		t.Fatal("unknown remote address was classified as local")
	}
	if !service.isLocalProcessAddress(localAddress) {
		t.Fatal("transient refresh failure discarded the last good local-address snapshot")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("interface address loader calls = %d, want 1", got)
	}
}

func TestDisabledAttributionLeavesProcessNil(t *testing.T) {
	settings := configureProcessAttributionSetting(t, false)
	provider := &proxyAttributionTestProvider{lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
		return proxyResolvedProcessResult(106)
	}}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	service.settingService = settings
	conn := newAddressedTestConn(t, "127.0.0.1:43107", "127.0.0.1:8080")

	if attributed := service.attributeConnection(conn); attributed != conn {
		_ = attributed.Close()
		t.Fatalf("disabled attribution wrapped connection as %T", attributed)
	}
	entry := &TrafficEntry{ID: 47, Metadata: &Metadata{}}
	service.fillEntryProcessFromTuple(proxyAttributionTuple(43107), entry)
	if entry.Metadata.Process != nil {
		t.Fatalf("disabled process = %+v, want nil", entry.Metadata.Process)
	}
	if calls := provider.calls.Load(); calls != 0 {
		t.Fatalf("disabled provider calls = %d, want 0", calls)
	}
}

func TestGetProcessIconRejectsPathTraversal(t *testing.T) {
	store, err := processattribution.NewIconStore(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	service := newProxyAttributionTestService(nil, store)
	if _, err := service.GetProcessIcon("../../secrets"); err == nil {
		t.Fatal("GetProcessIcon accepted a traversal key")
	}
}

func TestGetProcessIconReturnsPNGAndMissing(t *testing.T) {
	store, err := processattribution.NewIconStore(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	icon := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			icon.SetNRGBA(x, y, color.NRGBA{R: 210, G: 80, B: 40, A: 255})
		}
	}
	key, err := store.Put(icon)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	service := newProxyAttributionTestService(nil, store)

	data, err := service.GetProcessIcon(key)
	if err != nil {
		t.Fatalf("GetProcessIcon: %v", err)
	}
	if data == nil || data.MIMEType != "image/png" {
		t.Fatalf("GetProcessIcon data = %+v", data)
	}
	decodedBytes, err := base64.StdEncoding.DecodeString(data.DataBase64)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(decodedBytes)); err != nil {
		t.Fatalf("returned data is not PNG: %v", err)
	}
	missing, err := service.GetProcessIcon(strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("GetProcessIcon missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing icon = %+v, want nil", missing)
	}
}

func TestGetProcessIconRestoresMissingFileFromActiveLookup(t *testing.T) {
	store, err := processattribution.NewIconStore(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	var iconCalls atomic.Int32
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(108)
		},
		icon: func(context.Context, processattribution.Result) (image.Image, error) {
			iconCalls.Add(1)
			icon := image.NewNRGBA(image.Rect(0, 0, 2, 2))
			icon.SetNRGBA(0, 0, color.NRGBA{R: 40, G: 130, B: 220, A: 255})
			return icon, nil
		},
	}
	manager := newProxyAttributionTestManager(t, provider, store)
	service := newProxyAttributionTestService(manager, store)
	tuple := proxyAttributionTuple(43109)
	lookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, lookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
		lookup.Release()
	})

	deadline := time.Now().Add(2 * time.Second)
	for lookup.Snapshot().IconKey == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	iconKey := lookup.Snapshot().IconKey
	if iconKey == "" {
		t.Fatal("initial icon extraction did not complete")
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("delete cached icon file: %v", err)
	}

	restored, err := service.GetProcessIcon(iconKey)
	if err != nil {
		t.Fatalf("GetProcessIcon after deletion: %v", err)
	}
	if restored == nil || restored.MIMEType != "image/png" || restored.DataBase64 == "" {
		t.Fatalf("restored icon = %+v, want PNG data", restored)
	}
	if got := iconCalls.Load(); got != 2 {
		t.Fatalf("icon extraction calls = %d, want initial load plus one recovery", got)
	}
	if _, found, err := store.Get(iconKey); err != nil || !found {
		t.Fatalf("restored icon file = found %t, error %v", found, err)
	}
}

func TestClearCacheCancelsMissingIconRecovery(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("APPDATA", configDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	store, err := processattribution.NewIconStore(baseDir)
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}

	recoveryStarted := make(chan struct{})
	var iconCalls atomic.Int32
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(109)
		},
		icon: func(ctx context.Context, _ processattribution.Result) (image.Image, error) {
			if iconCalls.Add(1) == 1 {
				return image.NewNRGBA(image.Rect(0, 0, 2, 2)), nil
			}
			close(recoveryStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	manager := newProxyAttributionTestManager(t, provider, store)
	service := newProxyAttributionTestService(manager, store)
	service.resetCurrentHistoryMetadata()
	tuple := proxyAttributionTuple(43110)
	lookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, lookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
		lookup.Release()
	})

	deadline := time.Now().Add(2 * time.Second)
	for lookup.Snapshot().IconKey == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	iconKey := lookup.Snapshot().IconKey
	if iconKey == "" {
		t.Fatal("initial icon extraction did not complete")
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("delete cached icon file: %v", err)
	}

	recoveryDone := make(chan struct {
		data *ProcessIconData
		err  error
	}, 1)
	go func() {
		data, recoveryErr := service.GetProcessIcon(iconKey)
		recoveryDone <- struct {
			data *ProcessIconData
			err  error
		}{data: data, err: recoveryErr}
	}()
	waitForSignal(t, recoveryStarted, "missing icon recovery")
	if err := service.ClearCacheFiles(); err != nil {
		t.Fatalf("ClearCacheFiles: %v", err)
	}
	select {
	case recovered := <-recoveryDone:
		if recovered.err != nil || recovered.data != nil {
			t.Fatalf("cancelled recovery = data %+v, error %v", recovered.data, recovered.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cache clear did not cancel missing icon recovery")
	}
	if _, found, err := store.Get(iconKey); err != nil || found {
		t.Fatalf("icon after cancelled recovery = found %t, error %v", found, err)
	}
}

func TestClearCacheStopsOldIconWorkAndInvalidatesConnectionLookup(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("APPDATA", configDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	store, err := processattribution.NewIconStore(baseDir)
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}

	iconStarted := make(chan struct{})
	releaseIcon := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseIcon:
		default:
			close(releaseIcon)
		}
	})
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(107)
		},
		icon: func(ctx context.Context, _ processattribution.Result) (image.Image, error) {
			close(iconStarted)
			select {
			case <-releaseIcon:
				return image.NewNRGBA(image.Rect(0, 0, 2, 2)), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	manager := newProxyAttributionTestManager(t, provider, store)
	service := newProxyAttributionTestService(manager, store)
	service.resetCurrentHistoryMetadata()
	tuple := proxyAttributionTuple(43108)
	lookup := manager.Start(context.Background(), tuple)
	connectionBinding := service.registerConnectionProcessLookup(tuple, lookup)
	t.Cleanup(func() {
		service.unregisterConnectionProcessLookup(tuple, connectionBinding)
		lookup.Release()
	})

	waitForSignal(t, iconStarted, "icon extraction")
	if err := service.ClearCacheFiles(); err != nil {
		t.Fatalf("ClearCacheFiles: %v", err)
	}
	close(releaseIcon)
	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	manager.Drain(drainCtx)
	cancel()

	if got := lookup.Snapshot().IconKey; got != "" {
		t.Fatalf("connection lookup icon key after clear = %q, want empty", got)
	}
	if current := service.processManager(); current != nil {
		t.Fatalf("process manager after clear = %p, want nil", current)
	}
	replacement := service.ensureProcessAttributionManager()
	t.Cleanup(replacement.Close)
	if replacement == manager {
		t.Fatal("cache clear reused the old process attribution manager")
	}
}

func TestClearCacheOperationsResetProcessIconStore(t *testing.T) {
	tests := []struct {
		name  string
		clear func(*ProxyService) error
	}{
		{name: "cache files", clear: (*ProxyService).ClearCacheFiles},
		{name: "cache and history", clear: (*ProxyService).ClearCacheAndHistory},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("APPDATA", configDir)
			t.Setenv("XDG_CONFIG_HOME", configDir)
			t.Setenv("HOME", configDir)
			baseDir, err := fs.GetBaseStorageDir()
			if err != nil {
				t.Fatalf("GetBaseStorageDir: %v", err)
			}
			store, err := processattribution.NewIconStore(baseDir)
			if err != nil {
				t.Fatalf("NewIconStore: %v", err)
			}
			key, err := store.Put(image.NewNRGBA(image.Rect(0, 0, 2, 2)))
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			service := newProxyAttributionTestService(nil, store)
			service.SetHistoryCleaner(&testHistoryCleaner{})
			service.resetCurrentHistoryMetadata()
			if err := test.clear(service); err != nil {
				t.Fatalf("clear: %v", err)
			}
			data, found, err := store.Get(key)
			if err != nil {
				t.Fatalf("Get after clear: %v", err)
			}
			if found || data != nil {
				t.Fatalf("icon after clear = (%d bytes, %t), want missing", len(data), found)
			}
		})
	}
}

func newProxyAttributionTestManager(t *testing.T, provider processattribution.Provider, store *processattribution.IconStore) *processattribution.Manager {
	t.Helper()
	manager := processattribution.NewManager(provider, store, processattribution.Options{
		Workers: 1, QueueSize: 8, LookupTimeout: time.Second, IconWorkers: 1, IconQueueSize: 8,
	})
	t.Cleanup(manager.Close)
	return manager
}

func newProxyAttributionTestService(manager *processattribution.Manager, store *processattribution.IconStore) *ProxyService {
	return &ProxyService{
		baseCtx:                   context.Background(),
		trafficEntries:            &TrafficEntryWithStatics{OrderedMap: orderedmap.NewWithCapacity[uint64, *TrafficEntry](8), Statistics: &TrafficStatistics{}},
		processAttributionManager: manager,
		processIconStore:          store,
		localProcessAddresses: map[netip.Addr]struct{}{
			netip.MustParseAddr("127.0.0.1"): {},
			netip.MustParseAddr("::1"):       {},
		},
	}
}

func configureProcessAttributionSetting(t *testing.T, enabled bool) *settingservice.SettingService {
	t.Helper()
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	service := settingservice.New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load settings: %v", err)
	}
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get settings: %v", err)
	}
	settings.ProcessAttributionConfig.Enabled = enabled
	if err := service.Update(settings); err != nil {
		t.Fatalf("Update settings: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return service
}

func proxyResolvedProcessResult(pid uint32) processattribution.Result {
	return processattribution.Result{
		Status:             processattribution.StatusResolved,
		PID:                pid,
		StartToken:         "test-start-token",
		DisplayName:        "FlowLens Test App",
		ProcessName:        "flowlens-test.exe",
		ExecutablePath:     "C:/test/flowlens-test.exe",
		AppID:              "test.app",
		Source:             "windows_tcp_table",
		IdentityConfidence: "exact",
	}
}

func proxyAttributionTuple(clientPort uint16) processattribution.EndpointTuple {
	return processattribution.EndpointTuple{
		Client: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), clientPort),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
}

func newAddressedTestConn(t *testing.T, remote, local string) net.Conn {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() {
		_ = left.Close()
		_ = right.Close()
	})
	return &addressedTestConn{
		Conn:   left,
		remote: net.TCPAddrFromAddrPort(netip.MustParseAddrPort(remote)),
		local:  net.TCPAddrFromAddrPort(netip.MustParseAddrPort(local)),
	}
}

type addressedTestConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
}

func (c *addressedTestConn) RemoteAddr() net.Addr { return c.remote }
func (c *addressedTestConn) LocalAddr() net.Addr  { return c.local }

type singleConnListener struct {
	mu   sync.Mutex
	conn net.Conn
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil, net.ErrClosed
	}
	conn := l.conn
	l.conn = nil
	return conn, nil
}

func (l *singleConnListener) Close() error { return nil }

func (l *singleConnListener) Addr() net.Addr {
	return net.TCPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:8080"))
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForProcessLookupStatus(t *testing.T, lookup *processattribution.Lookup, status processattribution.Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lookup.Snapshot().Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for lookup status %q; final=%+v", status, lookup.Snapshot())
}

func waitForTrafficProcessPatch(
	t *testing.T,
	updates <-chan TrafficEntryPatch,
	status ProcessStatus,
	requireIcon bool,
) TrafficEntryPatch {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case patch := <-updates:
			if patch.Process == nil || patch.Process.Status != status {
				continue
			}
			if requireIcon && patch.Process.IconKey == "" {
				continue
			}
			if !requireIcon && patch.Process.IconKey != "" {
				continue
			}
			return patch
		case <-timer.C:
			t.Fatalf("timed out waiting for traffic process status=%q requireIcon=%t", status, requireIcon)
			return TrafficEntryPatch{}
		}
	}
}
