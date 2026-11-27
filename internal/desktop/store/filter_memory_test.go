package store

import (
	"runtime"
	"testing"
	"weak"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

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
