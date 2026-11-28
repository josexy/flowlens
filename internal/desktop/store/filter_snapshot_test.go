package store

import (
	"runtime"
	"slices"
	"testing"
	"weak"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

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
