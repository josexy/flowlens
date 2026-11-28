package logic

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"weak"

	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestSnapshotSortOrderAndPublication(t *testing.T) {
	s := NewTrafficTableSorter(TrafficSnapshot)
	entries := []*TrafficEntry{tableEntry(3, 200), tableEntry(2, 500), tableEntry(1, 200)}
	original := slices.Clone(entries)
	asc, desc := TrafficSort{ColumnStatusCode, SortAsc}, TrafficSort{ColumnStatusCode, SortDesc}
	for _, test := range []struct {
		sort TrafficSort
		want []uint64
	}{
		{asc, []uint64{1, 3, 2}}, {desc, []uint64{2, 1, 3}}, {TrafficSort{}, []uint64{3, 2, 1}},
	} {
		if !slices.Equal(ids(s.Sort(entries, test.sort, 1)), test.want) {
			t.Fatal("snapshot order differs")
		}
		if !slices.Equal(entries, original) {
			t.Fatal("sort modified source order")
		}
	}
	s.Sort(entries, asc, 1)
	computed := s.computed
	s.Sort(entries, asc, 1)
	if s.computed != computed {
		t.Fatal("unchanged snapshot recomputed keys")
	}
	entries[0] = tableEntry(3, 100)
	if rows := s.Sort(entries, asc, 2); rows[0] != entries[0] {
		t.Fatal("same-ID replacement lost")
	}
	entries[0] = tableEntry(4, 600)
	if !slices.Equal(ids(s.Sort(entries, asc, 3)), []uint64{1, 2, 4}) {
		t.Fatal("equal-sized membership stale")
	}
	if s.cached != nil {
		t.Fatal("snapshot retained live key map")
	}
}

func TestSnapshotSortFullTextAndUnknowns(t *testing.T) {
	s := NewTrafficTableSorter(TrafficSnapshot)
	entries := []*TrafficEntry{tableEntry(3, 0), tableEntry(2, 0), tableEntry(1, 0)}
	prefix := "/" + strings.Repeat("MiXeD", 100)
	entries[0].Path, entries[1].Path, entries[2].Path = prefix+"b", prefix+"a", prefix+"a"
	for _, test := range []struct {
		order SortOrder
		want  []uint64
	}{{SortAsc, []uint64{1, 2, 3}}, {SortDesc, []uint64{3, 1, 2}}} {
		if !slices.Equal(ids(s.Sort(entries, TrafficSort{ColumnPath, test.order}, 1)), test.want) {
			t.Fatal("full-text/tie ordering changed")
		}
	}
	complete := entries[1]
	complete.Request = &proxyservice.HTTPMessage{Metrics: metrics(100, 110, 0, 0, proxyservice.HTTPMessageStateCompleted)}
	complete.Response = &proxyservice.HTTPMessage{Metrics: metrics(100, 120, 0, 0, proxyservice.HTTPMessageStateCompleted)}
	for _, key := range []TrafficColumnKey{ColumnDuration, ColumnSize} {
		for _, order := range []SortOrder{SortAsc, SortDesc} {
			if got := ids(s.Sort(entries, TrafficSort{key, order}, 2)); !slices.Equal(got, []uint64{2, 1, 3}) {
				t.Fatalf("unknown %s/%s: %v", key, order, got)
			}
		}
	}
}

func TestSnapshotSortReleasesOldRowsAndShrinks(t *testing.T) {
	s := NewTrafficTableSorter(TrafficSnapshot)
	order := TrafficSort{ColumnPath, SortAsc}
	old := func() weak.Pointer[TrafficEntry] {
		entries := make([]*TrafficEntry, 100000)
		for i := range entries {
			entries[i] = tableEntry(uint64(i+1), 200)
		}
		s.Sort(entries, order, 1)
		return weak.Make(entries[0])
	}()
	s.Sort([]*TrafficEntry{tableEntry(1, 0), tableEntry(2, 0)}, order, 2)
	runtime.GC()
	if old.Value() != nil || cap(s.previousSorted) > 64 || s.cached != nil {
		t.Fatal("snapshot sort retained old rows or caches")
	}
	s.Sort(nil, order, 3)
	if s.previousSorted != nil || s.snapshotReady {
		t.Fatal("empty sort retained output")
	}
	runtime.KeepAlive(s)
}

func TestSnapshotSortDoesNotRetainFoldedLongText(t *testing.T) {
	entries := make([]*TrafficEntry, 512)
	for i := range entries {
		entries[i] = tableEntry(uint64(i+1), 0)
		entries[i].Path = strings.Repeat("MiXeD", 13108)
	}
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s := NewTrafficTableSorter(TrafficSnapshot)
	s.Sort(entries, TrafficSort{ColumnPath, SortAsc}, 1)
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	if retained := int64(after.HeapAlloc) - int64(before.HeapAlloc); retained > 1<<20 {
		t.Fatalf("folded sort keys retained %d bytes", retained)
	}
	s.Sort(entries, TrafficSort{}, 2)
	if s.previousSorted != nil || s.cached != nil {
		t.Fatal("unsorted state retained sorting storage")
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(entries)
}
