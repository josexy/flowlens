package proxyservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
	"github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/flowlens/internal/serviceruntime/topictest"
)

type residualHistoryCleaner struct {
	service    *ProxyService
	oldKey     string
	historyDir string
	err        error
	calls      int
	visible    bool
	currentKey string
}

func (c *residualHistoryCleaner) ClearHistories() error {
	c.calls++
	c.currentKey = c.service.CurrentHistoryKey()
	_, hbinErr := os.Stat(filepath.Join(c.historyDir, fs.GetHBinFileName(c.oldKey)))
	_, hidxErr := os.Stat(filepath.Join(c.historyDir, fs.GetHIdxFileName(c.oldKey)))
	c.visible = hbinErr == nil && hidxErr == nil && c.currentKey != c.oldKey
	return c.err
}

func TestRemoveStorageDirectoriesAttemptsAllAndJoinsErrors(t *testing.T) {
	cacheErr := errors.New("cache denied")
	requestErr := errors.New("request denied")
	var attempted []string
	err := removeStorageDirectories(func(path string) error {
		attempted = append(attempted, path)
		switch path {
		case "cache-path":
			return cacheErr
		case "request-draft-path":
			return requestErr
		default:
			return nil
		}
	},
		storageDirectoryRemoval{name: "cache", path: "cache-path"},
		storageDirectoryRemoval{name: "request draft cache", path: "request-draft-path"},
		storageDirectoryRemoval{name: "third", path: "third-path"},
	)
	if !errors.Is(err, cacheErr) || !errors.Is(err, requestErr) {
		t.Fatalf("joined error = %v", err)
	}
	if got := fmt.Sprint(attempted); got != "[cache-path request-draft-path third-path]" {
		t.Fatalf("attempted paths = %s", got)
	}
}

func TestClearCacheAndHistoryReturnsHistoryCleanerError(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	wantErr := errors.New("history cleanup failed")
	cleaner := &testHistoryCleaner{err: wantErr}
	svc.SetHistoryCleaner(cleaner)

	result, err := svc.clearStoredCaptureData(true)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "clear indexed histories") {
		t.Fatalf("clearStoredCaptureData error = %v", err)
	}
	if result.HistoryCleared {
		t.Fatal("failed history cleanup must not report history as cleared")
	}
	if cleaner.calls != 1 {
		t.Fatalf("history cleaner calls = %d, want 1", cleaner.calls)
	}
	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
}

func TestClearCacheAndHistoryRejectsMissingHistoryCleanerAfterStateReset(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	svc.storeTrafficEntry(&TrafficEntry{ID: 1, Type: "http"})

	result, err := svc.clearStoredCaptureData(true)
	if !result.Cleared {
		t.Fatal("clearStoredCaptureData should report the in-memory reset")
	}
	if result.HistoryCleared {
		t.Fatal("missing history cleaner must not report history as cleared")
	}
	if err == nil || !strings.Contains(err.Error(), "history cleaner is not configured") {
		t.Fatalf("clearStoredCaptureData error = %v", err)
	}
	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
}

func TestClearStoredCaptureDataReportsCompleteHistoryClear(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	cleaner := &testHistoryCleaner{}
	svc.SetHistoryCleaner(cleaner)

	result, err := svc.clearStoredCaptureData(true)
	if err != nil {
		t.Fatalf("clearStoredCaptureData: %v", err)
	}
	if !result.HistoryCleared {
		t.Fatal("successful history cleanup should report history as cleared")
	}
	if cleaner.calls != 1 {
		t.Fatalf("history cleaner calls = %d, want 1", cleaner.calls)
	}
}

func TestClearStoredCaptureDataRotatesCurrentKeyBeforeIndexingFailedPair(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{
		Type:      "http",
		Method:    "GET",
		URL:       "https://residual.example/",
		Host:      "residual.example",
		Path:      "/",
		StartedAt: time.Now(),
	})
	if !svc.storeTrafficEntry(entry) {
		t.Fatal("store current traffic entry")
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flush current history: %v", err)
	}

	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("get history storage path: %v", err)
	}
	oldKey := svc.CurrentHistoryKey()
	hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(oldKey))
	hidxPath := filepath.Join(historyDir, fs.GetHIdxFileName(oldKey))
	wantRemoveErr := errors.New("hbin is busy")
	svc.removeHistoryFile = func(path string) error {
		if path == hbinPath {
			return wantRemoveErr
		}
		return os.Remove(path)
	}
	wantCleanerErr := errors.New("residual history remains")
	cleaner := &residualHistoryCleaner{
		service:    svc,
		oldKey:     oldKey,
		historyDir: historyDir,
		err:        wantCleanerErr,
	}
	svc.SetHistoryCleaner(cleaner)

	result, err := svc.clearStoredCaptureData(true)
	if !errors.Is(err, wantRemoveErr) || !errors.Is(err, wantCleanerErr) {
		t.Fatalf("clearStoredCaptureData error = %v", err)
	}
	if result.HistoryCleared {
		t.Fatal("failed current history pair must report a partial history clear")
	}
	if cleaner.calls != 1 || !cleaner.visible {
		t.Fatalf("cleaner observation = calls:%d visible:%t current:%q old:%q", cleaner.calls, cleaner.visible, cleaner.currentKey, oldKey)
	}
	if cleaner.currentKey == oldKey {
		t.Fatal("current history key was not rotated before indexing residual histories")
	}
	for _, path := range []string{hbinPath, hidxPath} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("failed current history pair should remain retryable: %s: %v", path, statErr)
		}
	}
}

// newSessionCacheTestService returns a running service whose capture holds
// one entry with a body spooled to the session's body-cache directory.
func newSessionCacheTestService(t *testing.T) (*ProxyService, string) {
	t.Helper()
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	svc.running = true
	svc.ensureBodyCacheLocked()
	if svc.bodyCache == nil {
		t.Fatal("body cache was not created")
	}
	sessionDir := svc.bodyCache.SessionDir()
	entry := svc.newTrafficEntry(TrafficEntry{Type: "http", Method: "GET", URL: "https://example.com/"})
	if !svc.storeTrafficEntry(entry) {
		t.Fatal("traffic entry was rejected")
	}
	if err := svc.bodyCache.Write(entry.ID, bodycache.KindResponse, []byte("cached response")); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}
	return svc, sessionDir
}

func assertDirRemoved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("session directory %s still exists: %v", dir, err)
	}
}

func TestRestartCaptureRemovesPreviousBodyCacheSession(t *testing.T) {
	for _, save := range []bool{true, false} {
		t.Run(fmt.Sprintf("save=%t", save), func(t *testing.T) {
			svc, sessionDir := newSessionCacheTestService(t)
			savedKey := svc.CurrentHistoryKey()

			if err := svc.RestartCapture(save); err != nil {
				t.Fatalf("RestartCapture: %v", err)
			}
			assertDirRemoved(t, sessionDir)
			if svc.bodyCache == nil || svc.bodyCache.SessionDir() == sessionDir {
				t.Fatal("restart did not start a new body-cache session")
			}
			if _, err := os.Stat(svc.bodyCache.SessionDir()); err != nil {
				t.Fatalf("new session directory: %v", err)
			}
			historyDir, err := getHistoryStoragePath()
			if err != nil {
				t.Fatalf("getHistoryStoragePath: %v", err)
			}
			_, statErr := os.Stat(filepath.Join(historyDir, fs.GetHBinFileName(savedKey)))
			if save && statErr != nil {
				t.Fatalf("saved history was removed: %v", statErr)
			}
			if !save && !os.IsNotExist(statErr) {
				t.Fatalf("discarded history still exists: %v", statErr)
			}
		})
	}
}

func TestShutdownRemovesBodyCacheSessionAfterFinalSave(t *testing.T) {
	svc, sessionDir := newSessionCacheTestService(t)
	if err := svc.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	assertDirRemoved(t, sessionDir)
}

func TestShutdownRemovesBodyCacheSessionWithNothingToSave(t *testing.T) {
	svc, sessionDir := newSessionCacheTestService(t)
	svc.historyDirty.Store(false)
	svc.lastFlushGeneration.Store(svc.flushGeneration.Load())
	if err := svc.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	assertDirRemoved(t, sessionDir)
}

func TestShutdownKeepsBodyCacheSessionWhenFinalSaveFails(t *testing.T) {
	svc, sessionDir := newSessionCacheTestService(t)
	wantErr := errors.New("injected final flush failure")
	svc.historyFlushStageHook = func(string) error { return wantErr }

	if err := svc.Shutdown(); !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown error = %v, want final flush failure", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "1_resp.body")); err != nil {
		t.Fatalf("cached body of an unsaved session was removed: %v", err)
	}
}

func TestServiceStartupRemovesOnlyStaleBodyCacheSessions(t *testing.T) {
	setTestConfigDir(t)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	cacheRoot := filepath.Join(baseDir, "cache")
	stale := []string{
		"0b4f8f5e-62a4-4c4f-9a8a-0d2b9e0c1a11",
		"6f1c2d3e-4b5a-4978-8a6b-5c4d3e2f1a00",
	}
	kept := []string{"process-icons", "not-a-session"}
	for _, name := range append(append([]string{}, stale...), kept...) {
		dir := filepath.Join(cacheRoot, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(name), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	svc := newTestProxyService(t, nil)
	if err := svc.ServiceStartup(context.Background()); err != nil {
		t.Fatalf("ServiceStartup: %v", err)
	}
	t.Cleanup(func() { _ = svc.Shutdown() })

	for _, name := range stale {
		assertDirRemoved(t, filepath.Join(cacheRoot, name))
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(cacheRoot, name, "file")); err != nil {
			t.Fatalf("%s was removed: %v", name, err)
		}
	}
}

func TestClearLocalDataReportsWhatWasCleared(t *testing.T) {
	historyErr := errors.New("history index is busy")
	tests := []struct {
		name        string
		clear       func(*ProxyService) (LocalDataClearResult, error)
		cleanerErr  error
		wantScope   LocalDataClearScope
		wantHistory bool
		wantErr     error
	}{
		{name: "cache only", clear: (*ProxyService).ClearCacheFiles, wantScope: LocalDataScopeCache},
		{name: "cache and history", clear: (*ProxyService).ClearCacheAndHistory, wantScope: LocalDataScopeCacheAndHistory, wantHistory: true},
		{name: "history not cleared", clear: (*ProxyService).ClearCacheAndHistory, cleanerErr: historyErr, wantScope: LocalDataScopeCacheAndHistory, wantErr: historyErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestConfigDir(t)
			svc := newTestProxyService(t, nil)
			cleaner := &testHistoryCleaner{err: tt.cleanerErr}
			svc.SetHistoryCleaner(cleaner)
			svc.storeTrafficEntry(&TrafficEntry{ID: 1, Type: "http"})
			baseDir, err := fs.GetBaseStorageDir()
			if err != nil {
				t.Fatalf("GetBaseStorageDir: %v", err)
			}

			result, err := tt.clear(svc)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("clear: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("clear error = %v, want %v", err, tt.wantErr)
			}
			want := LocalDataClearResult{
				Scope:                 tt.wantScope,
				Cleared:               true,
				HistoryCleared:        tt.wantHistory,
				RequestDraftCacheRoot: filepath.Join(baseDir, "request-draft-cache"),
			}
			if result != want {
				t.Fatalf("result = %+v, want %+v", result, want)
			}
			if svc.trafficEntries.Len() != 0 {
				t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
			}
		})
	}
}

func TestClearLocalDataReportsPartialFailureWithResult(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not stop root from removing files")
	}
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	locked := filepath.Join(baseDir, "cache", "locked")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locked, "file"), []byte("kept"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	result, err := svc.ClearCacheFiles()
	if err == nil || !strings.Contains(err.Error(), "remove cache directory") {
		t.Fatalf("ClearCacheFiles error = %v, want a cache removal failure", err)
	}
	if !result.Cleared || result.Scope != LocalDataScopeCache || result.HistoryCleared {
		t.Fatalf("result = %+v, want a cleared cache-only result", result)
	}
	if result.RequestDraftCacheRoot != filepath.Join(baseDir, "request-draft-cache") {
		t.Fatalf("RequestDraftCacheRoot = %q", result.RequestDraftCacheRoot)
	}
}

func TestProxyTopicsPublishStatusChanges(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	svc.SetHistoryCleaner(&testHistoryCleaner{})
	status := topictest.Record(t, &svc.Events.Status)
	dns := topictest.Record(t, &svc.Events.DNSCache)
	live := topictest.Record(t, &svc.Events.Live)

	cleared := svc.ClearDNSCache()
	if got, ok := dns.Last(); !ok || dns.Len() != 1 || got != cleared {
		t.Fatalf("DNS cache values = %+v", dns.Values())
	}
	if _, err := svc.ClearCacheAndHistory(); err != nil {
		t.Fatalf("ClearCacheAndHistory: %v", err)
	}
	if got, ok := status.Last(); !ok || got.Running {
		t.Fatalf("status values = %+v", status.Values())
	}
	// Without a running batcher, each live event is published at once.
	batches := live.Values()
	if len(batches) != 1 || len(batches[0].Events) != 1 || batches[0].Events[0] != (TrafficResetEvent{CaptureGeneration: svc.captureGeneration}) {
		t.Fatalf("live batches = %+v", batches)
	}
}
