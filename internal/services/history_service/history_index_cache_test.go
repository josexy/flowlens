package historyservice

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/josexy/flowlens/internal/pkg/fs"
)

func knownHistories(keys ...string) map[string]struct{} {
	histories := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		histories[key] = struct{}{}
	}
	return histories
}

func testHistoryKeys(count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08x-0000-4000-8000-000000000000", i+1)
	}
	return keys
}

func cachedIndexKeys(service *HistoryService) []string {
	service.mu.RLock()
	defer service.mu.RUnlock()
	keys := make([]string, 0, service.indexes.len())
	for _, item := range service.indexes.items {
		keys = append(keys, item.key)
	}
	return keys
}

func TestListingHistoriesLoadsNoIndex(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	keys := testHistoryKeys(10)
	for _, key := range keys {
		writeTestHistoryPair(t, historyDir, key, 1)
	}
	service := New(nil, nil)

	listed, err := service.ListHistoryKeys()
	if err != nil {
		t.Fatalf("ListHistoryKeys: %v", err)
	}
	if len(listed) != len(keys) {
		t.Fatalf("listed %d histories, want %d", len(listed), len(keys))
	}
	if cached := cachedIndexKeys(service); len(cached) != 0 {
		t.Fatalf("listing loaded indexes of %v", cached)
	}
	if err := service.refreshHistories(); err != nil {
		t.Fatalf("refreshHistories: %v", err)
	}
	if cached := cachedIndexKeys(service); len(cached) != 0 {
		t.Fatalf("refreshing loaded indexes of %v", cached)
	}
}

func TestListingSkipsHistoryWithTruncatedIndex(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	keys := testHistoryKeys(2)
	writeTestHistoryPair(t, historyDir, keys[0], 1)
	writeTestHistoryPair(t, historyDir, keys[1], 1)
	// The index claims one record but holds none.
	if err := os.WriteFile(filepath.Join(historyDir, fs.GetHIdxFileName(keys[1])), []byte{0, 0, 0, 1}, 0o600); err != nil {
		t.Fatalf("write truncated index: %v", err)
	}
	service := New(nil, nil)
	listed, err := service.ListHistoryKeys()
	if err != nil {
		t.Fatalf("ListHistoryKeys: %v", err)
	}
	if len(listed) != 1 || listed[0].Key != keys[0] {
		t.Fatalf("listed %+v, want only %s", listed, keys[0])
	}
}

func TestOpeningAFifthHistoryEvictsTheLeastRecentlyUsedIndex(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	keys := testHistoryKeys(5)
	for _, key := range keys {
		writeTestHistoryPair(t, historyDir, key, 1)
	}
	service := New(nil, nil)
	if err := service.refreshHistories(); err != nil {
		t.Fatalf("refreshHistories: %v", err)
	}
	for _, key := range keys[:4] {
		if _, err := service.GetHistory(key); err != nil {
			t.Fatalf("GetHistory %s: %v", key, err)
		}
	}
	// Reading a body marks the first history as recently used again.
	if _, err := service.GetHistoryTrafficBodyView(keys[0], 1); err == nil {
		t.Fatal("GetHistoryTrafficBodyView of a missing entry returned nil")
	}
	if _, err := service.GetHistory(keys[4]); err != nil {
		t.Fatalf("GetHistory fifth: %v", err)
	}

	cached := cachedIndexKeys(service)
	want := []string{keys[2], keys[3], keys[0], keys[4]}
	if fmt.Sprint(cached) != fmt.Sprint(want) {
		t.Fatalf("cached indexes = %v, want %v", cached, want)
	}
	if _, err := service.GetHistory(keys[1]); err != nil {
		t.Fatalf("reopening an evicted history: %v", err)
	}
}

func TestDeleteAndClearEvictCachedIndexes(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	keys := testHistoryKeys(3)
	for _, key := range keys {
		writeTestHistoryPair(t, historyDir, key, 1)
	}
	service := New(nil, nil)
	if err := service.refreshHistories(); err != nil {
		t.Fatalf("refreshHistories: %v", err)
	}
	for _, key := range keys {
		if _, err := service.GetHistory(key); err != nil {
			t.Fatalf("GetHistory %s: %v", key, err)
		}
	}

	if err := service.DeleteHistory(keys[0]); err != nil {
		t.Fatalf("DeleteHistory: %v", err)
	}
	service.mu.RLock()
	deletedCached := service.indexes.has(keys[0])
	service.mu.RUnlock()
	if deletedCached {
		t.Fatal("deleted history kept its cached index")
	}
	if _, err := service.GetHistory(keys[0]); err == nil {
		t.Fatal("GetHistory of a deleted history returned nil")
	}

	if err := service.ClearHistories(); err != nil {
		t.Fatalf("ClearHistories: %v", err)
	}
	if cached := cachedIndexKeys(service); len(cached) != 0 {
		t.Fatalf("cleared histories kept cached indexes %v", cached)
	}
}
