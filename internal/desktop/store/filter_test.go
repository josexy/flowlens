package store

import (
	"runtime"
	"slices"
	"testing"
	"weak"

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
	if filters.base != nil || filters.filtered != nil || len(filters.matches) != 0 {
		t.Fatal("released filter still references entries")
	}
	if len(filters.Base()) != 0 || len(filters.Filtered()) != 0 {
		t.Fatal("released filter returned entries of the closed source")
	}
	if filters.Version() == before {
		t.Fatal("release did not change the filter version")
	}
}

func TestFilterReusesStorageAndInvalidatesMatches(t *testing.T) {
	entries, version := entryRange(1, 3), uint64(1)
	entries[0].Host, entries[1].Host, entries[2].Host = "a.test", "b.test", "a.test"
	f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	f.SelectedHosts = []string{"a.test"}
	requireIDs(t, f.Filtered(), []uint64{1, 3})
	before := f.Version()
	updated := *entries[2]
	updated.Host = "b.test"
	entries[2], version = &updated, version+1
	requireIDs(t, f.Filtered(), []uint64{1})
	if f.Version() == before {
		t.Fatal("removal did not invalidate the table")
	}
	f.ClearSelectedHosts()
	requireIDs(t, f.Filtered(), []uint64{1, 2, 3})
	f.SearchText = "b.test"
	requireIDs(t, f.Filtered(), []uint64{2, 3})
	entries, version = []*logic.TrafficEntry{entries[2], trafficEntry(4)}, version+1
	requireIDs(t, f.Filtered(), []uint64{3})
	if len(f.matches) != 2 {
		t.Fatal("absent records remain cached")
	}
	f.ClearFilters()
	requireIDs(t, f.Filtered(), []uint64{3, 4})
	for _, rows := range [][]*logic.TrafficEntry{f.base, f.filtered} {
		for _, entry := range rows[len(rows):cap(rows)] {
			if entry != nil {
				t.Fatal("slice tail retains a removed record")
			}
		}
	}
}

func TestFilterReleasesReplacedAndShrunkSnapshots(t *testing.T) {
	var entries []*logic.TrafficEntry
	version := uint64(1)
	f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	f.SearchText = "example"
	old := func() weak.Pointer[logic.TrafficEntry] {
		entries = entryRange(1, 5000)
		f.Refresh()
		return weak.Make(entries[0])
	}()
	entries, version = []*logic.TrafficEntry{trafficEntry(1)}, version+1
	f.Refresh()
	if f.matchCapacity > 64 || cap(f.base) > 64 || cap(f.filtered) > 64 {
		t.Fatal("shrunk source retained large filter storage")
	}
	runtime.GC()
	if old.Value() != nil {
		t.Fatal("filter retained the replaced record")
	}
	entries, version = nil, version+1
	f.Refresh()
	if f.matches != nil || f.base != nil || f.filtered != nil {
		t.Fatal("empty source retained filter storage")
	}
	runtime.KeepAlive(f)
}

func TestSnapshotFilterBorrowsOnlyUnchangedViews(t *testing.T) {
	for _, search := range []bool{false, true} {
		for _, category := range []bool{false, true} {
			entries := entryRange(1, 3)
			for _, entry := range entries {
				entry.Host = "keep.test"
			}
			original := slices.Clone(entries)
			f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, 1 }, logic.TrafficSnapshot)
			if search {
				f.SearchText = "keep"
			}
			if category {
				f.SelectedHosts = []string{"keep.test"}
			}
			rows := f.Filtered()
			if len(rows) != 3 || f.matches != nil {
				t.Fatal("snapshot result/cache differs")
			}
			if (&f.base[0] == &entries[0]) == search {
				t.Fatal("wrong base ownership")
			}
			if (&rows[0] == &f.base[0]) == category {
				t.Fatal("wrong final ownership")
			}
			f.ClearFilters()
			f.Refresh()
			if f.baseOwned != nil || f.filteredOwned != nil || &f.filtered[0] != &entries[0] {
				t.Fatal("All retained unused buffers")
			}
			f.Release()
			if !slices.Equal(entries, original) {
				t.Fatal("release/All corrupted source")
			}
		}
	}
}

func TestSnapshotCategoryChangeKeepsSearchResult(t *testing.T) {
	entries := entryRange(1, 3)
	for _, entry := range entries {
		entry.Host, entry.URL = "a.test", "https://a.test/"
	}
	entries[2].Host, entries[2].URL = "b.test", "https://b.test/"
	f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, 1 }, logic.TrafficSnapshot)
	f.SearchText = "a.test"
	requireIDs(t, f.Filtered(), []uint64{1, 2})
	baseVersion := f.BaseVersion()
	// Published records are immutable; a rescan would observe this edit.
	entries[0].Host, entries[0].URL = "c.test", "https://c.test/"
	f.SelectedHosts = []string{"c.test"}
	requireIDs(t, f.Filtered(), []uint64{1})
	if f.BaseVersion() != baseVersion || len(f.Base()) != 2 {
		t.Fatal("a category change rescanned the unchanged search result")
	}
	f.SearchText = "c.test"
	requireIDs(t, f.Base(), []uint64{1})
}

func TestSnapshotFilterShrinksToResultSize(t *testing.T) {
	entries := entryRange(1, 100000)
	for _, entry := range entries {
		entry.Host = "many.test"
	}
	entries[0].Host, entries[1].Host = "two.test", "two.test"
	f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, 1 }, logic.TrafficSnapshot)
	f.SearchText = "test"
	f.SelectedHosts = []string{"many.test", "two.test"}
	f.Refresh()
	f.SearchText = "two.test"
	f.Refresh()
	if len(f.filtered) != 2 || cap(f.baseOwned) > 64 || cap(f.filteredOwned) > 64 {
		t.Fatal("selective result retained source-sized storage")
	}
	f.SearchText = "no-match"
	f.Refresh()
	if f.baseOwned != nil || f.filteredOwned != nil {
		t.Fatal("empty result retained buffers")
	}
	f.Release()
	for i, entry := range entries {
		if entry == nil || entry.ID != uint64(i+1) {
			t.Fatal("shrink corrupted source")
		}
	}
}

func TestSnapshotFilterEmptyAndSingleRow(t *testing.T) {
	entries, version := []*logic.TrafficEntry(nil), uint64(1)
	f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, logic.TrafficSnapshot)
	if len(f.Filtered()) != 0 {
		t.Fatal("empty snapshot returned rows")
	}
	entries, version = []*logic.TrafficEntry{trafficEntry(1)}, version+1
	if len(f.Filtered()) != 1 {
		t.Fatal("single row absent")
	}
	f.Release()
	if entries[0] == nil {
		t.Fatal("single row cleared")
	}
}

func TestFilterBaseRevisionAndSameIDReplacement(t *testing.T) {
	for _, policy := range []logic.TrafficProcessingPolicy{logic.TrafficLive, logic.TrafficSnapshot} {
		entries, version := entryRange(1, 2), uint64(1)
		entries[0].Host, entries[1].Host = "chosen.test", "other.test"
		f := NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, policy)
		f.SelectedHosts = []string{"chosen.test"}
		base, result := f.BaseVersion(), f.Version()
		replaced := *entries[1]
		replaced.Host = "outside.test"
		entries[1], version = &replaced, version+1
		if f.BaseVersion() == base || len(f.Filtered()) != 1 {
			t.Fatal("base-only update lost")
		}
		if policy == logic.TrafficLive && f.Version() != result {
			t.Fatal("live stable subset changed revision")
		}
		f.ClearFilters()
		before := f.Version()
		old := weak.Make(entries[0])
		entries[0], version = trafficEntry(1), version+1
		if f.Version() == before || f.Filtered()[0] != entries[0] {
			t.Fatal("aliased same-ID update stale")
		}
		runtime.GC()
		if old.Value() != nil {
			t.Fatal("replaced pointer retained")
		}
		base = f.BaseVersion()
		f.SearchText = "outside"
		if f.BaseVersion() == base {
			t.Fatal("search did not publish base revision")
		}
		runtime.KeepAlive(f)
	}
}
