package store

import (
	"slices"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

func TestFilterCachesRefreshOnQueryAndReplacement(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 3))
	for _, entry := range h.store.Entries {
		entry.Path = "/" + string(rune('0'+entry.ID))
		entry.URL = "https://example.test" + entry.Path
	}
	filters := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return h.store.Entries, h.store.EntriesVersion })
	filters.SearchText = "/2"
	requireIDs(t, filters.Filtered(), []uint64{2})

	changed := withRevision(trafficEntry(2), 2)
	changed.URL, changed.Path = "https://example.test/changed", "/changed"
	h.store.AddOrUpdate(changed)
	h.tick()
	if len(filters.Filtered()) != 0 {
		t.Fatal("replaced entry should stop matching")
	}
	filters.SearchText = "changed"
	requireIDs(t, filters.Filtered(), []uint64{2})
	filters.SearchText = ""
	if len(filters.Filtered()) != 3 {
		t.Fatal("empty search")
	}
}

func TestFilterVersionChangesOnlyWithTheList(t *testing.T) {
	entries := entryRange(1, 2)
	version := uint64(1)
	filters := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	before := filters.Version()
	version = 2
	if filters.Version() != before {
		t.Fatal("identical list should keep its version")
	}
	entries = append(slices.Clone(entries), trafficEntry(3))
	version = 3
	if filters.Version() == before {
		t.Fatal("new entry should change the version")
	}
}

func TestFilterAppliesCategoriesAfterBase(t *testing.T) {
	a, b := trafficEntry(1), trafficEntry(2)
	a.Host, b.Host = "a.test", "b.test"
	filters := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return []*logic.TrafficEntry{a, b}, 1 })
	filters.ToggleHost("b.test")
	requireIDs(t, filters.Filtered(), []uint64{2})
	requireIDs(t, filters.Base(), []uint64{1, 2})
	filters.ToggleHost("b.test")
	requireIDs(t, filters.Filtered(), []uint64{1, 2})
	filters.ToggleProcess(logic.ProcessCategoryUnavailableKey)
	requireIDs(t, filters.Filtered(), []uint64{1, 2})
	filters.ClearFilters()
	if filters.ActiveTab != logic.TabAll || filters.SelectedProcessKeys != nil {
		t.Fatal("clear")
	}
}

func TestFilterTabHidesCompletedNonMatchingEntry(t *testing.T) {
	entries := []*logic.TrafficEntry{trafficEntry(1)}
	version := uint64(1)
	filters := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	filters.SetActiveTab(logic.TabJSON)
	if len(filters.Filtered()) != 0 {
		t.Fatal("no content type yet")
	}
}

func TestFilterReleaseDropsEveryEntryReference(t *testing.T) {
	entries := entryRange(1, 3)
	version := uint64(1)
	filters := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	filters.SelectedHosts = []string{entries[0].Host}
	if len(filters.Filtered()) == 0 || len(filters.Base()) != 3 {
		t.Fatal("filter did not compute")
	}
	before := filters.Version()

	entries, version = nil, 2
	filters.Release()
	if filters.base != nil || filters.filtered != nil || len(filters.baseMatches) != 0 || len(filters.categoryMatch) != 0 {
		t.Fatal("released filter still references entries")
	}
	if len(filters.Base()) != 0 || len(filters.Filtered()) != 0 {
		t.Fatal("released filter returned entries of the closed source")
	}
	if filters.Version() == before {
		t.Fatal("release did not change the filter version")
	}
}
