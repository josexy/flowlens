package capture

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
)

func TestNumericSelectionPreservesHighIDsAndShrinks(t *testing.T) {
	const high = uint64(1)<<63 + 17
	f := newFixture(t, entry(high, "first.test"), entry(high+1, "second.test"))
	f.clickRow(0, "first.test")
	f.clickRow(ui.Cmd, "second.test")
	table := f.workspace.Table
	if !table.state.Selection.Has(high) || !table.state.Selection.Has(high+1) {
		t.Fatal("numeric identity lost precision")
	}
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.settle()
	if table.state.Selection.Len() != 2 {
		t.Fatal("native Select All failed")
	}
	bound := table.state.List.Selection
	table.state.ClearSelection()
	if bound != table.state.List.Selection || !reflect.ValueOf(&table.state.Selection).Elem().FieldByName("keys").IsNil() {
		t.Fatal("clear retained buckets or changed the bound selector address")
	}
	f.tester.Key(0, ui.KeyUp)
	f.settle()
	if table.state.Selection.Len() != 1 {
		t.Fatal("clear interrupted native keyboard choice")
	}
}

func TestNumericSelectionReleasesLargeMapAfterShrink(t *testing.T) {
	f := newFixtureSized(t, nil, 1400, 800)
	entries, version := rangeOfEntries(100000), uint64(1)
	filter := benchmarkFilter(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, "Snapshot")
	table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
	table.refreshRows()
	for _, entry := range entries {
		table.state.Selection.Add(entry.ID)
	}
	entries, version = entries[:2], version+1
	table.refreshRows()
	if table.state.Selection.Len() != 2 || table.state.Len() != 2 {
		t.Fatal("selection/identity storage retained the old full source")
	}
	table.Reset()
	if table.state.Len() != 0 {
		t.Fatal("reset retained numeric identities")
	}
}
