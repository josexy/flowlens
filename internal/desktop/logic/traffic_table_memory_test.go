package logic

import (
	"runtime"
	"slices"
	"testing"
	"weak"
)

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
