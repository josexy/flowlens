package capture

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
)

func TestTrafficSourceProcessingPolicies(t *testing.T) {
	f := newFixtureSized(t, rangeOfEntries(2), 1400, 800, &fakeHistoryBackend{})
	if f.data.Filter.ProcessingPolicy() != logic.TrafficLive || f.data.History.Filter.ProcessingPolicy() != logic.TrafficSnapshot {
		t.Fatal("capture/history processing policy is not explicit")
	}
	filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return nil, 0 }, logic.TrafficLive)
	source := benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}
	table := NewTable(source, f.workspace.Table.deps)
	table.Reset()
	if source.Live() != nil || table.source.Filter().ProcessingPolicy() != logic.TrafficLive {
		t.Fatal("uncapped mutable source inferred snapshot behavior from live controls")
	}
}

func TestSnapshotSelectionSurvivesFilterDeleteAndSort(t *testing.T) {
	f := newFixtureSized(t, nil, 1400, 800)
	entries, version := rangeOfEntries(3), uint64(1)
	entries[0].Host, entries[1].Host, entries[2].Host = "keep.test", "keep.test", "remove.test"
	filter := benchmarkFilter(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, "Snapshot")
	table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
	table.refreshRows()
	table.replaceSelection(0)
	table.state.Selection.Add(2)
	table.state.Sort = ui.SortOrder{Column: string(logic.ColumnID), Descending: true}
	table.refreshRows()
	filter.SearchText = "keep.test"
	table.refreshRows()
	if table.state.Selection.Len() != 2 || table.rows[0].ID != 2 {
		t.Fatal("filter/sort lost surviving selection")
	}
	entries, version = entries[1:], version+1
	table.refreshRows()
	if table.state.Selection.Len() != 1 || !table.state.Selection.Has(2) {
		t.Fatal("deletion did not reconcile selection by ID")
	}
	table.state.Sort = ui.SortOrder{}
	table.refreshRows()
	if table.rows[0] != entries[0] {
		t.Fatal("unsorted snapshot failed to restore source order")
	}
	table.Reset()
	if table.rows != nil || table.state.Selection.Len() != 0 {
		t.Fatal("reset retained history state")
	}
}

func TestSnapshotTableReplacesPointersWithReusedIDs(t *testing.T) {
	f := newFixtureSized(t, nil, 1400, 800)
	entries, version := rangeOfEntries(3), uint64(1)
	filter := benchmarkFilter(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, "Snapshot")
	table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
	table.refreshRows()
	for _, search := range []string{"", "h"} {
		filter.SearchText = search
		table.refreshRows()
		entries[0], version = entry(1, "h-new.test"), version+1
		if !table.refreshRows() || table.rows[0] != entries[0] {
			t.Fatal("same-ID source replacement left stale table rows")
		}
		if table.refreshRows() {
			t.Fatal("unchanged revision recomputed table")
		}
	}
	filter.Release()
	if entries[0] == nil {
		t.Fatal("table release corrupted borrowed source")
	}
}
