package capture

import (
	"fmt"
	"testing"

	"github.com/josexy/flowlens/desktop/logic"
)

func rangeOfEntries(count int) []*logic.TrafficEntry {
	entries := make([]*logic.TrafficEntry, count)
	for index := range entries {
		entries[index] = entry(uint64(index+1), fmt.Sprintf("h%d.test", index%50))
	}
	return entries
}

// patchVisibleRow changes the status of a row on screen, as a live update does.
func patchVisibleRow(f *fixture, revision uint64) {
	table := f.workspace.Table
	first, _ := table.list.Visible()
	current := table.rows[first]
	updated := *current
	updated.Revision, updated.StatusCode = revision, 200+int(revision%300)
	f.data.Traffic.AddOrUpdate(&updated)
	f.data.Traffic.FlushPending()
	f.window.Drain()
}

func cellsPerFrame(t *testing.T, rows int) int {
	t.Helper()
	f := newFixtureSized(t, rangeOfEntries(rows), 1400, 800)
	table := f.workspace.Table
	revision := uint64(2)
	for range 3 {
		patchVisibleRow(f, revision)
		f.tester.Frame()
		revision++
	}
	table.CellBuilds = 0
	const frames = 20
	for range frames {
		patchVisibleRow(f, revision)
		f.tester.Frame()
		revision++
	}
	return table.CellBuilds / frames
}

func TestVisibleCellBuildsDoNotGrowWithTotalRows(t *testing.T) {
	small, large := cellsPerFrame(t, 500), cellsPerFrame(t, 5000)
	if small == 0 {
		t.Fatal("no cells built")
	}
	if large > small+small/10 {
		t.Fatalf("cells per frame grew from %d at 500 rows to %d at 5000 rows", small, large)
	}
	const viewportRowsTimesColumns = 40 * 11
	if large > viewportRowsTimesColumns {
		t.Fatalf("built %d cells in a frame", large)
	}
}

func benchmarkFrame(b *testing.B, rows int) {
	f := newFixtureSized(b, rangeOfEntries(rows), 1400, 800)
	revision := uint64(2)
	b.ResetTimer()
	for b.Loop() {
		patchVisibleRow(f, revision)
		f.tester.Frame()
		revision++
	}
}

func BenchmarkTrafficTableFrame500(b *testing.B)  { benchmarkFrame(b, 500) }
func BenchmarkTrafficTableFrame5000(b *testing.B) { benchmarkFrame(b, 5000) }
