package historyservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/josexy/flowlens/backend/pkg/fs"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

const (
	knownHistoryKey   = "11111111-1111-4111-8111-111111111111"
	unknownHistoryKey = "22222222-2222-4222-8222-222222222222"
	unsupportedKey    = "33333333-3333-4333-8333-333333333333"
)

type rotatingHistoryProxy struct {
	currentKey string
}

func (p *rotatingHistoryProxy) CurrentHistoryKey() string {
	return p.currentKey
}

func (p *rotatingHistoryProxy) ResendRequestWithTrafficEntry(
	context.Context,
	proxyservice.ResendConfig,
	*proxyservice.TrafficEntry,
	[]byte,
) (proxyservice.ResendResult, error) {
	return proxyservice.ResendResult{}, nil
}

func TestInitializeHistoryIndexPreservesActiveCommitWindowIndex(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	indexPath := filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey))
	if err := os.WriteFile(indexPath, []byte("active index awaiting data commit"), fs.PrivateFileMode); err != nil {
		t.Fatalf("write active history index: %v", err)
	}

	service := New(nil, &rotatingHistoryProxy{currentKey: knownHistoryKey})
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("active history index was removed during a data-file commit window: %v", err)
	}
	if _, indexed := service.indexMap[knownHistoryKey]; indexed {
		t.Fatal("active history commit window was indexed")
	}
}

func TestInitializeHistoryIndexPreservesPairWhenRecoveryFails(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, knownHistoryKey, 1)
	dataPath := filepath.Join(historyDir, fs.GetHBinFileName(knownHistoryKey))
	indexPath := filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey))
	backupPath := dataPath + ".bak"
	dataContents, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read history data fixture: %v", err)
	}
	if err := os.Remove(dataPath); err != nil {
		t.Fatalf("remove canonical data fixture: %v", err)
	}
	if err := os.Mkdir(backupPath, fs.PrivateDirMode); err != nil {
		t.Fatalf("create temporarily unreadable backup fixture: %v", err)
	}

	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err == nil {
		t.Fatal("initializeHistoryIndexMap returned nil for failed recovery")
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("canonical index was deleted after failed data recovery: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("recoverable backup marker was deleted: %v", err)
	}

	if err := os.RemoveAll(backupPath); err != nil {
		t.Fatalf("remove failed backup fixture: %v", err)
	}
	if err := os.WriteFile(backupPath, dataContents, fs.PrivateFileMode); err != nil {
		t.Fatalf("repair history backup fixture: %v", err)
	}
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("retry initializeHistoryIndexMap: %v", err)
	}
	if _, indexed := service.indexMap[knownHistoryKey]; !indexed {
		t.Fatal("recovered history pair was not indexed on retry")
	}
	assertHistoryPairExists(t, historyDir, knownHistoryKey, true)
	if _, err := os.Lstat(backupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup remains after successful recovery: %v", err)
	}
}

func TestDeleteHistoryDeletesKnownUUIDPair(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryFiles(t, historyDir, knownHistoryKey)
	managedArtifacts := []string{
		filepath.Join(historyDir, fs.GetHBinFileName(knownHistoryKey)) + ".bak",
		filepath.Join(historyDir, fs.GetHBinFileName(knownHistoryKey)) + ".tmp12345",
		filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey)) + ".bak",
		filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey)) + ".tmp-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	}
	for _, path := range managedArtifacts {
		if err := os.WriteFile(path, []byte("managed transaction artifact"), 0o600); err != nil {
			t.Fatalf("write managed history artifact %s: %v", path, err)
		}
	}
	service := &HistoryService{indexMap: historyIndexMap{
		knownHistoryKey: {entries: newOrderedIndexList()},
	}}

	if err := service.DeleteHistory(knownHistoryKey); err != nil {
		t.Fatalf("DeleteHistory: %v", err)
	}
	assertHistoryPairExists(t, historyDir, knownHistoryKey, false)
	if _, ok := service.indexMap[knownHistoryKey]; ok {
		t.Fatal("deleted history remained indexed")
	}
	for _, path := range managedArtifacts {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed history artifact remains after deletion: %s: %v", path, err)
		}
	}
}

func TestDeleteHistoryRejectsUnsafeAndUnknownKeys(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	outsideBase := filepath.Join(filepath.Dir(historyDir), "outside-history")
	writeHistoryFiles(t, filepath.Dir(outsideBase), filepath.Base(outsideBase))
	absoluteBase := filepath.Join(t.TempDir(), "absolute-history")
	writeHistoryFiles(t, filepath.Dir(absoluteBase), filepath.Base(absoluteBase))
	writeHistoryFiles(t, historyDir, unknownHistoryKey)

	service := &HistoryService{indexMap: historyIndexMap{
		"../outside-history": {entries: newOrderedIndexList()},
		absoluteBase:         {entries: newOrderedIndexList()},
		"fixture-key":        {entries: newOrderedIndexList()},
	}}
	tests := []struct {
		name string
		key  string
	}{
		{name: "traversal", key: "../outside-history"},
		{name: "absolute", key: absoluteBase},
		{name: "non UUID", key: "fixture-key"},
		{name: "unknown UUID", key: unknownHistoryKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := service.DeleteHistory(tt.key); err == nil {
				t.Fatal("DeleteHistory returned nil")
			}
		})
	}

	assertHistoryPairExists(t, filepath.Dir(outsideBase), filepath.Base(outsideBase), true)
	assertHistoryPairExists(t, filepath.Dir(absoluteBase), filepath.Base(absoluteBase), true)
	assertHistoryPairExists(t, historyDir, unknownHistoryKey, true)
}

func TestClearHistoriesDeletesPairs(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryFiles(t, historyDir, knownHistoryKey)
	writeHistoryFiles(t, historyDir, unknownHistoryKey)
	service := &HistoryService{indexMap: historyIndexMap{
		knownHistoryKey:   {entries: newOrderedIndexList()},
		unknownHistoryKey: {entries: newOrderedIndexList()},
	}}

	if err := service.ClearHistories(); err != nil {
		t.Fatalf("ClearHistories: %v", err)
	}
	assertHistoryPairExists(t, historyDir, knownHistoryKey, false)
	assertHistoryPairExists(t, historyDir, unknownHistoryKey, false)
	if len(service.indexMap) != 0 {
		t.Fatalf("indexMap length = %d, want 0", len(service.indexMap))
	}
}

func TestClearHistoriesPreservesUnindexedUnsupportedFiles(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryFiles(t, historyDir, knownHistoryKey)
	writeHistoryFiles(t, historyDir, unsupportedKey)
	service := &HistoryService{indexMap: historyIndexMap{
		knownHistoryKey: {entries: newOrderedIndexList()},
	}}

	if err := service.ClearHistories(); err != nil {
		t.Fatalf("ClearHistories: %v", err)
	}
	assertHistoryPairExists(t, historyDir, knownHistoryKey, false)
	assertHistoryPairExists(t, historyDir, unsupportedKey, true)
}

func TestPreviousCurrentHistoryBecomesVisibleAfterKeyRotation(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, knownHistoryKey, 1)
	proxy := &rotatingHistoryProxy{currentKey: knownHistoryKey}
	service := &HistoryService{
		proxyService: proxy,
		indexMap:     make(historyIndexMap),
	}

	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initialize current history index: %v", err)
	}
	if _, ok := service.indexMap[knownHistoryKey]; ok {
		t.Fatal("active current history should not be indexed")
	}

	proxy.currentKey = unknownHistoryKey
	metadata, err := service.ListHistoryKeys()
	if err != nil {
		t.Fatalf("ListHistoryKeys after key rotation: %v", err)
	}
	if len(metadata) != 1 || metadata[0].Key != knownHistoryKey {
		t.Fatalf("visible histories = %#v, want previous current key %s", metadata, knownHistoryKey)
	}
}

func TestInitializeHistoryIndexRecoversInterruptedPairCommit(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, knownHistoryKey, 1)
	hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(knownHistoryKey))
	hidxPath := filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey))
	hbinBefore, err := os.ReadFile(hbinPath)
	if err != nil {
		t.Fatalf("read hbin fixture: %v", err)
	}
	hidxBefore, err := os.ReadFile(hidxPath)
	if err != nil {
		t.Fatalf("read hidx fixture: %v", err)
	}
	if err := os.Rename(hbinPath, hbinPath+".bak"); err != nil {
		t.Fatalf("back up hbin fixture: %v", err)
	}
	if err := os.Rename(hidxPath, hidxPath+".bak"); err != nil {
		t.Fatalf("back up hidx fixture: %v", err)
	}
	if err := os.WriteFile(hbinPath, []byte("partial-new-history"), 0o600); err != nil {
		t.Fatalf("write partial hbin fixture: %v", err)
	}

	service := &HistoryService{indexMap: make(historyIndexMap)}
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}
	if _, ok := service.indexMap[knownHistoryKey]; !ok {
		t.Fatal("recovered history pair was not indexed")
	}
	if got, err := os.ReadFile(hbinPath); err != nil {
		t.Fatalf("read recovered hbin: %v", err)
	} else if string(got) != string(hbinBefore) {
		t.Fatal("recovered hbin differs from the previous committed file")
	}
	if got, err := os.ReadFile(hidxPath); err != nil {
		t.Fatalf("read recovered hidx: %v", err)
	} else if string(got) != string(hidxBefore) {
		t.Fatal("recovered hidx differs from the previous committed file")
	}
	for _, path := range []string{hbinPath + ".bak", hidxPath + ".bak"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("history backup remains after recovery: %s: %v", path, err)
		}
	}
}

func TestClearHistoriesReportsFailureAndRetainsIndex(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(knownHistoryKey))
	writeTestHistoryPair(t, historyDir, knownHistoryKey, 1)
	hbinContents, err := os.ReadFile(hbinPath)
	if err != nil {
		t.Fatalf("read hbin fixture: %v", err)
	}
	if err := os.Remove(hbinPath); err != nil {
		t.Fatalf("remove hbin fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(hbinPath, "child"), 0o700); err != nil {
		t.Fatalf("create non-empty hbin directory: %v", err)
	}
	hidxPath := filepath.Join(historyDir, fs.GetHIdxFileName(knownHistoryKey))
	service := &HistoryService{indexMap: historyIndexMap{
		knownHistoryKey: {entries: newOrderedIndexList()},
	}}

	err = service.ClearHistories()
	if err == nil || !strings.Contains(err.Error(), fs.GetHBinFileName(knownHistoryKey)) {
		t.Fatalf("ClearHistories error = %v, want hbin deletion error", err)
	}
	if _, ok := service.indexMap[knownHistoryKey]; !ok {
		t.Fatal("failed history was removed from indexMap")
	}
	if _, statErr := os.Stat(hbinPath); statErr != nil {
		t.Fatalf("failed hbin target should remain: %v", statErr)
	}
	if _, statErr := os.Stat(hidxPath); statErr != nil {
		t.Fatalf("paired hidx should remain available for retry: %v", statErr)
	}

	if err := os.RemoveAll(hbinPath); err != nil {
		t.Fatalf("remove failed hbin target: %v", err)
	}
	if err := os.WriteFile(hbinPath, hbinContents, 0o600); err != nil {
		t.Fatalf("restore hbin fixture: %v", err)
	}
	restartedService := &HistoryService{indexMap: make(historyIndexMap)}
	if err := restartedService.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initialize history after restart: %v", err)
	}
	if _, ok := restartedService.indexMap[knownHistoryKey]; !ok {
		t.Fatal("preserved history pair was not reindexed after restart")
	}
	if err := restartedService.ClearHistories(); err != nil {
		t.Fatalf("retry ClearHistories: %v", err)
	}
	assertHistoryPairExists(t, historyDir, knownHistoryKey, false)
}

func TestInitializeHistoryIndexTightensExistingStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, knownHistoryKey, 1)
	if err := os.Chmod(historyDir, 0o755); err != nil {
		t.Fatalf("Chmod history dir setup: %v", err)
	}
	for _, name := range []string{fs.GetHBinFileName(knownHistoryKey), fs.GetHIdxFileName(knownHistoryKey)} {
		if err := os.Chmod(filepath.Join(historyDir, name), 0o644); err != nil {
			t.Fatalf("Chmod history file setup: %v", err)
		}
	}

	service := &HistoryService{indexMap: make(historyIndexMap)}
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}
	if info, err := os.Stat(historyDir); err != nil {
		t.Fatalf("Stat history dir: %v", err)
	} else if got := info.Mode().Perm(); got != fs.PrivateDirMode {
		t.Fatalf("history dir mode = %04o, want %04o", got, fs.PrivateDirMode)
	}
	for _, name := range []string{fs.GetHBinFileName(knownHistoryKey), fs.GetHIdxFileName(knownHistoryKey)} {
		if info, err := os.Stat(filepath.Join(historyDir, name)); err != nil {
			t.Fatalf("Stat history file: %v", err)
		} else if got := info.Mode().Perm(); got != fs.PrivateFileMode {
			t.Fatalf("history file mode = %04o, want %04o", got, fs.PrivateFileMode)
		}
	}
}

func writeHistoryFiles(t *testing.T, dir, key string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll history fixture: %v", err)
	}
	for _, name := range []string{fs.GetHBinFileName(key), fs.GetHIdxFileName(key)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
}
