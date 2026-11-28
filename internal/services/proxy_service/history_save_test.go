package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/pkg/fs"
)

func TestHistoryFlushWriteFailurePreservesPreviousPair(t *testing.T) {
	service, targets := setupCommittedHistoryPair(t)
	oldData := readTestFile(t, targets.data)
	oldIndex := readTestFile(t, targets.index)

	service.UpdateHistoryAlias("new alias")
	wantErr := errors.New("injected index write failure")
	service.historyFlushStageHook = func(stage string) error {
		if stage == historyFlushStageWriteIndex {
			return wantErr
		}
		return nil
	}
	if err := service.flushHistoryToDisk(true); !errors.Is(err, wantErr) {
		t.Fatalf("flushHistoryToDisk error = %v, want %v", err, wantErr)
	}

	assertTestFileEquals(t, targets.data, oldData)
	assertTestFileEquals(t, targets.index, oldIndex)
	assertNoHistoryTransactionFiles(t, filepath.Dir(targets.data))
}

func TestHistoryFlushRenameFailureRollsBackPreviousPair(t *testing.T) {
	service, targets := setupCommittedHistoryPair(t)
	oldData := readTestFile(t, targets.data)
	oldIndex := readTestFile(t, targets.index)

	service.UpdateHistoryAlias("new alias")
	wantErr := errors.New("injected index install failure")
	failed := false
	service.renameHistoryFile = func(oldPath, newPath string) error {
		if !failed && newPath == targets.index && strings.Contains(oldPath, fs.HIDX_SUFFIX+historyTempSeparator) {
			failed = true
			return wantErr
		}
		return os.Rename(oldPath, newPath)
	}
	if err := service.flushHistoryToDisk(true); !errors.Is(err, wantErr) {
		t.Fatalf("flushHistoryToDisk error = %v, want %v", err, wantErr)
	}
	if !failed {
		t.Fatal("index install rename was not exercised")
	}

	assertTestFileEquals(t, targets.data, oldData)
	assertTestFileEquals(t, targets.index, oldIndex)
	assertNoHistoryTransactionFiles(t, filepath.Dir(targets.data))
}

func TestHistoryFlushDoubleFailureKeepsBackupsRecoverableAfterRestart(t *testing.T) {
	service, targets := setupCommittedHistoryPair(t)
	oldData := readTestFile(t, targets.data)
	oldIndex := readTestFile(t, targets.index)

	service.UpdateHistoryAlias("new alias")
	installErr := errors.New("injected index install failure")
	restoreErr := errors.New("injected index restore failure")
	indexInstallAttempts := 0
	service.renameHistoryFile = func(oldPath, newPath string) error {
		if newPath == targets.index && strings.Contains(oldPath, fs.HIDX_SUFFIX+historyTempSeparator) {
			indexInstallAttempts++
			if indexInstallAttempts == 1 {
				return installErr
			}
			if indexInstallAttempts == 2 {
				return restoreErr
			}
		}
		return os.Rename(oldPath, newPath)
	}
	err := service.flushHistoryToDisk(true)
	if !errors.Is(err, installErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("flushHistoryToDisk error = %v, want install and restore failures", err)
	}
	backups := targets.backups()
	for _, path := range backups.values() {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("recoverable backup missing after double failure: %s: %v", path, statErr)
		}
	}

	// A restarted process uses normal filesystem operations and a different
	// current key. Recovery must rebuild the entire old pair from copies without
	// consuming one backup before the other component is safely installed.
	service.renameHistoryFile = nil
	if err := RecoverHistoryFileTransactions(filepath.Dir(targets.data), "different-current-key"); err != nil {
		t.Fatalf("RecoverHistoryFileTransactions after double failure: %v", err)
	}
	assertTestFileEquals(t, targets.data, oldData)
	assertTestFileEquals(t, targets.index, oldIndex)
	for _, path := range backups.values() {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("backup remains after successful restart recovery: %s: %v", path, statErr)
		}
	}
}

func TestRecoverHistoryFileTransactionsRestoresBackupPairAndMigratesPermissions(t *testing.T) {
	service, targets := setupCommittedHistoryPair(t)
	oldData := readTestFile(t, targets.data)
	oldIndex := readTestFile(t, targets.index)
	backups := targets.backups()
	if err := os.Rename(targets.data, backups.data); err != nil {
		t.Fatalf("back up data fixture: %v", err)
	}
	if err := os.Rename(targets.index, backups.index); err != nil {
		t.Fatalf("back up index fixture: %v", err)
	}
	if err := os.WriteFile(targets.data, []byte("partial-new-data"), fs.PrivateFileMode); err != nil {
		t.Fatalf("write partial data fixture: %v", err)
	}

	historyDir := filepath.Dir(targets.data)
	oldTemp := filepath.Join(historyDir, "legacy"+fs.HBIN_SUFFIX+".tmp12345")
	if err := os.WriteFile(oldTemp, []byte("abandoned sensitive data"), 0o644); err != nil {
		t.Fatalf("write old temp fixture: %v", err)
	}
	unsupported := filepath.Join(historyDir, "unsupported"+fs.HBIN_SUFFIX)
	if err := os.WriteFile(unsupported, []byte("unknown-version-data"), 0o644); err != nil {
		t.Fatalf("write unsupported fixture: %v", err)
	}

	if err := RecoverHistoryFileTransactions(historyDir, service.CurrentHistoryKey()+"-active"); err != nil {
		t.Fatalf("RecoverHistoryFileTransactions: %v", err)
	}
	assertTestFileEquals(t, targets.data, oldData)
	assertTestFileEquals(t, targets.index, oldIndex)
	for _, path := range []string{backups.data, backups.index, oldTemp} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("transaction artifact remains at %s: %v", path, err)
		}
	}
	if _, err := os.Stat(unsupported); err != nil {
		t.Fatalf("unsupported formal history file was not preserved: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(unsupported); err != nil {
			t.Fatalf("stat unsupported history file: %v", err)
		} else if got := info.Mode().Perm(); got != fs.PrivateFileMode {
			t.Fatalf("unsupported history mode = %04o, want %04o", got, fs.PrivateFileMode)
		}
	}
}

func TestRecoverHistoryFileTransactionsDoesNotMixOrphanBackupWithPartialInstall(t *testing.T) {
	setTestConfigDir(t)
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	if err := fs.EnsurePrivateDir(historyDir); err != nil {
		t.Fatalf("EnsurePrivateDir: %v", err)
	}
	key := "44444444-4444-4444-8444-444444444444"
	targets := historyFilePairPaths{
		data:  filepath.Join(historyDir, fs.GetHBinFileName(key)),
		index: filepath.Join(historyDir, fs.GetHIdxFileName(key)),
	}
	oldOrphanIndex := []byte("old-orphan-index")
	if err := os.WriteFile(targets.index+historyBackupSuffix, oldOrphanIndex, fs.PrivateFileMode); err != nil {
		t.Fatalf("write orphan backup: %v", err)
	}
	if err := os.WriteFile(targets.data, []byte("partially-installed-new-data"), fs.PrivateFileMode); err != nil {
		t.Fatalf("write partial new data: %v", err)
	}

	if err := RecoverHistoryFileTransactions(historyDir, ""); err != nil {
		t.Fatalf("RecoverHistoryFileTransactions: %v", err)
	}
	if _, err := os.Lstat(targets.data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial new data was retained beside an old orphan index: %v", err)
	}
	assertTestFileEquals(t, targets.index, oldOrphanIndex)
	if _, err := os.Lstat(targets.index + historyBackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restored orphan backup remains: %v", err)
	}
}

func setupCommittedHistoryPair(t *testing.T) (*ProxyService, historyFilePairPaths) {
	t.Helper()
	setTestConfigDir(t)
	service := newTestProxyService(t, nil)
	entry := service.newTrafficEntry(TrafficEntry{
		Type:      "http",
		Method:    "GET",
		URL:       "https://history-transaction.example/",
		Host:      "history-transaction.example",
		Path:      "/",
		StartedAt: time.Now(),
	})
	if !service.storeTrafficEntry(entry) {
		t.Fatal("store history transaction fixture")
	}
	service.UpdateHistoryAlias("old alias")
	if err := service.flushHistoryToDisk(true); err != nil {
		t.Fatalf("initial flushHistoryToDisk: %v", err)
	}
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	key := service.CurrentHistoryKey()
	return service, historyFilePairPaths{
		data:  filepath.Join(historyDir, fs.GetHBinFileName(key)),
		index: filepath.Join(historyDir, fs.GetHIdxFileName(key)),
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return contents
}

func assertTestFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := readTestFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("file %s changed after failed transaction", path)
	}
}

func assertNoHistoryTransactionFiles(t *testing.T, historyDir string) {
	t.Helper()
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", historyDir, err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), historyBackupSuffix) {
			t.Fatalf("history transaction artifact remains: %s", entry.Name())
		}
	}
}

// fakeHistorySaveClock records the delays the periodic save waits for and
// fires each one when the test sends on fire.
type fakeHistorySaveClock struct {
	mu     sync.Mutex
	now    time.Time
	delays chan time.Duration
	fire   chan time.Time
}

func (c *fakeHistorySaveClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeHistorySaveClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeHistorySaveClock) After(d time.Duration) <-chan time.Time {
	c.delays <- d
	return c.fire
}

func TestNextHistorySaveDelay(t *testing.T) {
	for _, tt := range []struct{ save, want time.Duration }{
		{0, time.Minute},
		{2 * time.Second, time.Minute},
		{12 * time.Second, time.Minute},
		{30 * time.Second, 150 * time.Second},
	} {
		if got := nextHistorySaveDelay(tt.save); got != tt.want {
			t.Fatalf("nextHistorySaveDelay(%s) = %s, want %s", tt.save, got, tt.want)
		}
	}
}

func TestPeriodicSaveWaitsLongerAfterASlowSave(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeHistorySaveClock{now: time.Unix(1_700_000_000, 0), delays: make(chan time.Duration, 1), fire: make(chan time.Time)}
	durations := []time.Duration{30 * time.Second, time.Second}
	saves := 0
	svc := &ProxyService{
		baseCtx:          ctx,
		historySaveClock: clock,
		periodicHistorySave: func() {
			clock.advance(durations[saves])
			saves++
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.syncHistoriesToDiskPeriodically()
	}()

	for _, want := range []time.Duration{time.Minute, 150 * time.Second, time.Minute} {
		select {
		case got := <-clock.delays:
			if got != want {
				t.Fatalf("periodic save waited %s, want %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("periodic save did not schedule its next run")
		}
		if want == time.Minute && saves == len(durations) {
			break
		}
		clock.fire <- clock.Now()
	}
	cancel()
	<-done
	if saves != len(durations) {
		t.Fatalf("saves = %d, want %d", saves, len(durations))
	}
}
