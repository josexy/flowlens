package proxyservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
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
	if result.historyCleared {
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
	if !result.stateCleared {
		t.Fatal("clearStoredCaptureData should report the in-memory reset")
	}
	if result.historyCleared {
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
	if !result.historyCleared {
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
	if result.historyCleared {
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
