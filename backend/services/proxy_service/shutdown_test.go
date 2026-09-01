package proxyservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/orderedmap"
	"github.com/josexy/flowlens/backend/pkg/systemproxy"
)

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
