package proxyservice_test

import (
	"reflect"
	"testing"

	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

type savedHistory struct {
	entries []*proxyservice.TrafficEntry
	bodies  map[uint64]*proxyservice.TrafficBodyView
}

// loadSavedHistory reads a saved capture with a new history service, as the
// History list does after a restart.
func loadSavedHistory(t *testing.T, key string) savedHistory {
	t.Helper()
	history := historyservice.New(nil, nil)
	if _, err := history.ListHistoryKeys(); err != nil {
		t.Fatalf("ListHistoryKeys: %v", err)
	}
	entries, err := history.GetHistory(key)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	saved := savedHistory{entries: entries, bodies: make(map[uint64]*proxyservice.TrafficBodyView, len(entries))}
	for _, entry := range entries {
		body, err := history.GetHistoryTrafficBodyView(key, entry.ID)
		if err != nil {
			t.Fatalf("GetHistoryTrafficBodyView %d: %v", entry.ID, err)
		}
		saved.bodies[entry.ID] = body
	}
	return saved
}

func TestPartlySpilledCaptureSavesEveryEntry(t *testing.T) {
	svc := proxyservice.NewSpillTestServiceForTest(t)
	httpID, wsID := proxyservice.CaptureSpillCandidatesForTest(t, svc)
	proxyservice.AddSettledEntriesForTest(t, svc, 5300)
	key := svc.CurrentHistoryKey()

	if err := svc.FlushHistoryForTest(); err != nil {
		t.Fatalf("baseline save: %v", err)
	}
	baseline := loadSavedHistory(t, key)
	if spilled := svc.SpillAllForTest(); spilled < 300 {
		t.Fatalf("spilled %d entries, want the ones beyond the window", spilled)
	}
	if err := svc.FlushHistoryForTest(); err != nil {
		t.Fatalf("save with spilled entries: %v", err)
	}
	saved := loadSavedHistory(t, key)

	if len(saved.entries) != 5302 || len(saved.entries) != len(baseline.entries) {
		t.Fatalf("saved %d entries, baseline %d", len(saved.entries), len(baseline.entries))
	}
	for i, entry := range saved.entries {
		if entry.ID != uint64(i+1) {
			t.Fatalf("entry %d has ID %d, want ID order", i, entry.ID)
		}
		if !reflect.DeepEqual(entry, baseline.entries[i]) {
			t.Fatalf("entry %d differs from the unspilled save", entry.ID)
		}
		if !reflect.DeepEqual(saved.bodies[entry.ID], baseline.bodies[entry.ID]) {
			t.Fatalf("bodies of entry %d differ from the unspilled save", entry.ID)
		}
	}
	if len(saved.bodies[wsID].WebSocketMessages) != 2 || saved.bodies[httpID].ResponseBody == "" {
		t.Fatal("saved history lost the captured bodies or messages")
	}
}
