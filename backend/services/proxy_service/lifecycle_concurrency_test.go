package proxyservice

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
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
	if _, err := service.settingService.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{}); err != nil {
		t.Fatalf("generate MITM CA: %v", err)
	}

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
	if !running || !serverInstalled || listenerInstalled || !cancelInstalled || !handlerInstalled || !configInstalled {
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
			return service.ClearCacheFiles()
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
