package logic

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestTrafficSorterReusesStorageWithoutKeepingOldEntries(t *testing.T) {
	sorter := NewTrafficTableSorter()
	order := TrafficSort{ColumnStatusCode, SortAsc}
	old := func() weak.Pointer[TrafficEntry] {
		a := tableEntry(1, 200)
		sorter.Sort([]*TrafficEntry{a, tableEntry(2, 404)}, order)
		return weak.Make(a)
	}()
	replacement := tableEntry(1, 500)
	rows := sorter.Sort([]*TrafficEntry{replacement, tableEntry(3, 201)}, order)
	if !slices.Equal(ids(rows), []uint64{3, 1}) || rows[1] != replacement || len(sorter.cached) != 2 {
		t.Fatal("same-size membership/revision change lost data")
	}
	runtime.GC()
	if old.Value() != nil {
		t.Fatal("sorter retained an obsolete revision")
	}
	sorter.Sort([]*TrafficEntry{replacement}, order)
	if len(sorter.cached) != 1 {
		t.Fatal("removed cache entry retained")
	}
	for _, entry := range sorter.previousSorted[1:cap(sorter.previousSorted)] {
		if entry != nil {
			t.Fatal("removed row retained in slice tail")
		}
	}
	sorter.Sort(nil, order)
	if sorter.cached != nil || sorter.previousSorted != nil {
		t.Fatal("empty sorter retains storage")
	}
	runtime.KeepAlive(sorter)
}

func TestTrafficSorterShrinksLargeSnapshot(t *testing.T) {
	sorter := NewTrafficTableSorter()
	entries := make([]*TrafficEntry, 5000)
	for i := range entries {
		entries[i] = tableEntry(uint64(i+1), i%500)
	}
	order := TrafficSort{ColumnStatusCode, SortAsc}
	sorter.Sort(entries, order)
	sorter.Sort(entries[:2], order)
	if sorter.capacity > 64 || cap(sorter.previousSorted) > 64 {
		t.Fatal("small snapshot retained large sorter storage")
	}
}

func TestTrafficCellPreview(t *testing.T) {
	for _, original := range []string{"", UnknownValue, "OPTIONS", strings.Repeat("x", 256)} {
		if got := TrafficCellPreview(original); got != original {
			t.Fatalf("short value changed: %q", got)
		}
	}
	for _, original := range []string{
		strings.Repeat("x", 257), strings.Repeat("界", 300), strings.Repeat("🙂", 200),
		strings.Repeat("x", 252) + "界界", strings.Repeat("x", 251) + "🙂🙂",
	} {
		got := TrafficCellPreview(original)
		if len(got) > TrafficCellPreviewBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") ||
			!strings.HasPrefix(original, strings.TrimSuffix(got, "…")) {
			t.Fatalf("invalid bounded preview: %q", got)
		}
	}
}

func TestTrafficPreviewDoesNotChangeFullPathOperations(t *testing.T) {
	prefix := "/" + strings.Repeat("x", 600)
	a, b := tableEntry(1, 200), tableEntry(2, 200)
	a.Path, b.Path = prefix+"z-last", prefix+"a-first"
	a.URL, b.URL = "https://example.test"+a.Path, "https://example.test"+b.Path
	if TrafficCellPreview(a.Path) != TrafficCellPreview(b.Path) {
		t.Fatal("test paths must share a preview")
	}
	if !TrafficMatchesSearch(a, "z-last") || TrafficMatchesSearch(b, "z-last") {
		t.Fatal("search lost the undisplayed suffix")
	}
	rows := NewTrafficTableSorter().Sort([]*TrafficEntry{a, b}, TrafficSort{ColumnPath, SortAsc})
	if rows[0] != b || TrafficPathLabel(a) != a.Path {
		t.Fatal("full path order or value was changed by previewing")
	}
	if TrafficCopyValue(a, CopyPath) != a.Path || TrafficCopyValue(a, CopyURL) != a.URL {
		t.Fatal("copy lost the undisplayed suffix")
	}
}
