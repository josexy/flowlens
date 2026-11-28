package proxyservice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/flowlens/internal/pkg/orderedmap"
	"github.com/josexy/flowlens/internal/pkg/systemproxy"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	http "github.com/josexy/xhttp"
)

func TestStartPublishesCompleteStateAndSerializesStop(t *testing.T) {
	setTestConfigDir(t)
	certDir := t.TempDir()
	service := newTestProxyService(t, &settingservice.ProxyConfig{
		Mode:         settingservice.ProxyModeHTTP,
		Host:         "127.0.0.1",
		Port:         reserveTestTCPPort(t),
		CACertPath:   filepath.Join(certDir, "ca.crt"),
		CAKeyPath:    filepath.Join(certDir, "ca.key"),
		DisableProxy: true,
	})
	t.Cleanup(service.baseCancel)
	writeCustomTestCA(t, service.settingService)

	published := make(chan struct{})
	releaseStart := make(chan struct{})
	service.lifecycleOperationHook = func(operation string) {
		if operation != "start-published" {
			return
		}
		close(published)
		<-releaseStart
	}

	startDone := make(chan error, 1)
	go func() {
		_, err := service.Start()
		startDone <- err
	}()
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published start state")
	}

	service.mu.Lock()
	running := service.running
	serverInstalled := service.server != nil
	listenerInstalled := service.ln != nil
	cancelInstalled := service.proxyServerCancel != nil
	handlerInstalled := service.proxyHandler != nil
	configInstalled := service.startProxyConfig != nil
	service.mu.Unlock()
	if !running || !serverInstalled || !listenerInstalled || !cancelInstalled || !handlerInstalled || !configInstalled {
		t.Fatalf(
			"published state incomplete: running=%t server=%t listener=%t cancel=%t handler=%t config=%t",
			running,
			serverInstalled,
			listenerInstalled,
			cancelInstalled,
			handlerInstalled,
			configInstalled,
		)
	}
	if !service.enableFlushing.Load() {
		t.Fatal("history flushing was not enabled with the running state")
	}

	stopAttempted := make(chan struct{})
	stopDone := make(chan error, 1)
	go func() {
		close(stopAttempted)
		_, err := service.Stop()
		stopDone <- err
	}()
	<-stopAttempted
	select {
	case err := <-stopDone:
		t.Fatalf("Stop bypassed the in-flight Start lifecycle operation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseStart)
	if err := <-startDone; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if service.running || service.server != nil || service.ln != nil || service.proxyServerCancel != nil || service.proxyHandler != nil || service.startProxyConfig != nil {
		t.Fatal("Stop did not clear the complete published lifecycle state")
	}
	if service.enableFlushing.Load() {
		t.Fatal("Stop left history flushing enabled")
	}
}

func TestStopReleasesHTTPListenerBeforeServe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().String()

	// Model a bound listener whose Serve goroutine has not run yet. Closing
	// the HTTP server alone cannot release a listener it has not registered.
	server := &http.Server{}
	service := &ProxyService{running: true, address: address, server: server, ln: listener}
	status, err := service.Stop()
	if err != nil || status.Running {
		t.Fatalf("Stop: %+v %v", status, err)
	}

	replacement, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listener still bound after Stop returned: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
}

func TestHistoryMetadataConcurrentAccess(t *testing.T) {
	service := New(nil)
	start := make(chan struct{})
	var wg sync.WaitGroup

	run := func(fn func(int)) {
		wg.Go(func() {
			<-start
			for i := range 1000 {
				fn(i)
			}
		})
	}
	run(func(i int) { service.UpdateHistoryAlias(fmt.Sprintf("capture-%d", i)) })
	run(func(int) { _ = service.GetStatus() })
	run(func(int) { _ = service.CurrentHistoryKey() })
	run(func(int) { _ = service.currentHistoryMetadataSnapshot() })
	run(func(int) { service.resetCurrentHistoryMetadata() })

	close(start)
	wg.Wait()

	metadata := service.currentHistoryMetadataSnapshot()
	if _, err := uuid.Parse(metadata.Key); err != nil {
		t.Fatalf("final history key is invalid: %q: %v", metadata.Key, err)
	}
	if metadata.CreatedAt <= 0 {
		t.Fatalf("final history createdAt = %d", metadata.CreatedAt)
	}
}

func TestCaptureTransitionsDoNotLoseTrafficPublishedDuringHistoryFlush(t *testing.T) {
	operations := map[string]func(*ProxyService) error{
		"restart and save": func(service *ProxyService) error {
			return service.RestartCapture(true)
		},
		"clear cache": func(service *ProxyService) error {
			_, err := service.ClearCacheFiles()
			return err
		},
	}

	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			setTestConfigDir(t)
			service := newTestProxyService(t, nil)
			first := service.newTrafficEntry(TrafficEntry{
				Type: "https",
				URL:  "https://before-transition.example/",
			})
			if !service.storeTrafficEntry(first) {
				t.Fatal("store initial traffic")
			}

			commitReached := make(chan struct{})
			releaseCommit := make(chan struct{})
			service.historyFlushStageHook = func(stage string) error {
				if stage == historyFlushStageBeforeCommit {
					close(commitReached)
					<-releaseCommit
				}
				return nil
			}

			operationDone := make(chan error, 1)
			go func() {
				operationDone <- operation(service)
			}()
			select {
			case <-commitReached:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for history commit")
			}

			lateStored := make(chan bool, 1)
			go func() {
				late := service.newTrafficEntry(TrafficEntry{
					Type: "https",
					URL:  "https://during-transition.example/",
				})
				lateStored <- service.storeTrafficEntry(late)
			}()

			select {
			case stored := <-lateStored:
				t.Fatalf("traffic publication crossed the capture transition: stored=%t", stored)
			case <-time.After(100 * time.Millisecond):
			}

			close(releaseCommit)
			if err := <-operationDone; err != nil {
				t.Fatalf("capture transition: %v", err)
			}
			if stored := <-lateStored; !stored {
				t.Fatal("traffic created during transition was not stored in the new capture")
			}

			entries := service.GetTraffic()
			if len(entries) != 1 || entries[0].URL != "https://during-transition.example/" {
				t.Fatalf("new capture entries = %#v", entries)
			}
		})
	}
}

type failingShutdownSystemProxy struct {
	restoreCalls atomic.Int32
	restoreErr   error
}

func (f *failingShutdownSystemProxy) State() systemproxy.ControllerState {
	return systemproxy.ControllerState{Supported: true, Active: true}
}

func (f *failingShutdownSystemProxy) Supports(systemproxy.Mode) bool {
	return true
}

func (f *failingShutdownSystemProxy) Apply(systemproxy.Endpoint, string) error {
	return nil
}

func (f *failingShutdownSystemProxy) Restore() error {
	f.restoreCalls.Add(1)
	return f.restoreErr
}

func TestShutdownIsIdempotent(t *testing.T) {
	var cancelCalls atomic.Int32
	service := &ProxyService{
		baseCtx: context.Background(),
		baseCancel: func() {
			cancelCalls.Add(1)
		},
	}

	for range 2 {
		if err := service.Shutdown(); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("base cancel calls = %d, want 1", got)
	}
	if !service.systemProxyShuttingDown {
		t.Fatal("shutdown should reject subsequent system proxy changes")
	}
}

func TestShutdownContinuesAfterSystemProxyRestoreFailureAndFlushesHistory(t *testing.T) {
	setTestConfigDir(t)
	wantRestoreErr := errors.New("restore failed")
	systemProxy := &failingShutdownSystemProxy{restoreErr: wantRestoreErr}
	var cancelCalls atomic.Int32
	service := &ProxyService{
		baseCtx: context.Background(),
		baseCancel: func() {
			cancelCalls.Add(1)
		},
		running: true,
		trafficEntries: &TrafficEntryWithStatics{
			OrderedMap: orderedmap.NewWithCapacity[uint64, *TrafficEntry](1),
			Statistics: &TrafficStatistics{},
		},
		currentHistoryMetadata: HistoryMetadata{
			Key:       "shutdown-final-flush",
			CreatedAt: time.Now().UnixMilli(),
		},
		systemProxy: systemProxy,
	}
	service.trafficEntries.Set(1, &TrafficEntry{
		ID:        1,
		Type:      "http",
		StartedAt: time.Now(),
		Method:    "GET",
		URL:       "https://example.com/",
		Host:      "example.com",
		Path:      "/",
	})
	service.markHistoryDirty()

	err := service.Shutdown()
	if !errors.Is(err, wantRestoreErr) {
		t.Fatalf("Shutdown error = %v, want restore error", err)
	}
	if service.GetStatus().Running {
		t.Fatal("proxy listener state should be stopped after restore failure")
	}
	if service.historyDirty.Load() {
		t.Fatal("final history should no longer be dirty after shutdown flush")
	}
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	for _, name := range []string{
		fs.GetHBinFileName(service.currentHistoryMetadata.Key),
		fs.GetHIdxFileName(service.currentHistoryMetadata.Key),
	} {
		info, statErr := os.Stat(filepath.Join(historyDir, name))
		if statErr != nil {
			t.Fatalf("final history file %q was not written: %v", name, statErr)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != fs.PrivateFileMode {
			t.Fatalf("final history file %q mode = %04o, want %04o", name, info.Mode().Perm(), fs.PrivateFileMode)
		}
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(historyDir)
		if statErr != nil {
			t.Fatalf("stat history directory: %v", statErr)
		}
		if info.Mode().Perm() != fs.PrivateDirMode {
			t.Fatalf("history directory mode = %04o, want %04o", info.Mode().Perm(), fs.PrivateDirMode)
		}
	}

	if secondErr := service.Shutdown(); !errors.Is(secondErr, wantRestoreErr) {
		t.Fatalf("second Shutdown error = %v, want restore error", secondErr)
	}
	if got := systemProxy.restoreCalls.Load(); got != 1 {
		t.Fatalf("system proxy restore calls = %d, want 1", got)
	}
	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("base cancel calls = %d, want 1", got)
	}
}
