package capture

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestTableStablePatchesHaveBoundedAllocations(t *testing.T) {
	for _, sorted := range []bool{false, true} {
		t.Run(fmt.Sprintf("sorted=%t", sorted), func(t *testing.T) {
			f := newFixtureSized(t, rangeOfEntries(1), 1400, 800)
			entries, version := rangeOfEntries(5000), uint64(1)
			filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
			filter.SearchText = "h" // Exercise predicate reuse as well as sorted/unsorted rows.
			table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
			if sorted {
				table.state.Sort = ui.SortOrder{Column: string(logic.ColumnStatusCode)}
			}
			table.refreshRows()
			table.replaceSelection(4999)
			original := entries[4999]
			replacement := *original
			replacement.Path = "/updated-payload"
			allocs := testing.AllocsPerRun(20, func() {
				version++
				if version%2 == 0 {
					entries[4999] = &replacement
				} else {
					entries[4999] = original
				}
				if !table.refreshRows() || table.rows[4999] != entries[4999] {
					t.Fatal("patch was not displayed")
				}
			})
			if allocs > 25 {
				t.Fatalf("stable patch rebuilt full-list storage: %.0f allocations", allocs)
			}
			if table.state.Selection.Len() != 1 || !table.state.Selection.Has("5000") {
				t.Fatal("stable patch changed selection")
			}
			entries, version = entries[:2], version+1
			table.refreshRows()
			if table.state.Selection.Len() != 0 || table.selectedRow != -1 || table.state.Len() != 2 ||
				cap(table.rowIDs) > 64 || cap(table.indices) > 64 {
				t.Fatal("shrinking failed to prune selection or release identity storage")
			}
			entries, version = nil, version+1
			table.refreshRows()
			if table.rowIDs != nil || table.indices != nil || table.state.Len() != 0 {
				t.Fatal("empty table retained identity storage")
			}
		})
	}
}

func longPathTable(t testing.TB, size int) (*fixture, *Table, *ui.Tester) {
	t.Helper()
	f := newFixtureSized(t, rangeOfEntries(1), 1400, 800)
	entries := rangeOfEntries(1000)
	for i, entry := range entries {
		entry.Path = fmt.Sprintf("/p%d?q=", i) + strings.Repeat(fmt.Sprintf("%05d", i), size/5)
		entry.URL = "http://long.test" + entry.Path
	}
	filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, 1 })
	table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
	tester := ui.NewTester(func(c *ui.Context) {
		table.Build(c, nui.App(c, f.data.Settings.Theme().Config))
	}, 1160, 714)
	tester.SetLocale("en")
	tester.SetDark(false)
	for range 8 {
		tester.Frame()
	}
	return f, table, tester
}

func TestLongTrafficPathsHaveBoundedScrollAllocations(t *testing.T) {
	measure := func(t *testing.T, size int) uint64 {
		f, table, tester := longPathTable(t, size)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for row := 27; row <= 108; row += 27 {
			table.state.List.ScrollTo(row, ui.Start)
			tester.Frame()
		}
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(f)
		runtime.KeepAlive(tester)
		return (after.TotalAlloc - before.TotalAlloc) / 4
	}
	var short, long uint64
	t.Run("preview-sized", func(t *testing.T) { short = measure(t, logic.TrafficCellPreviewBytes) })
	t.Run("64KiB", func(t *testing.T) { long = measure(t, 65536) })
	t.Logf("scroll allocation: preview-sized=%d B, 64KiB=%d B", short, long)
	if long > short+2<<20 {
		t.Fatalf("scroll allocation grew with undisplayed text: %d to %d", short, long)
	}
}

func BenchmarkTrafficTableLongPathScroll(b *testing.B) {
	for _, size := range []int{64, 4096, 16384, 65536} {
		b.Run(fmt.Sprintf("PathBytes%d", size), func(b *testing.B) {
			f, table, tester := longPathTable(b, size)
			row := 27
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				table.state.List.ScrollTo(row, ui.Start)
				tester.Frame()
				row = (row + 27) % (1000 - 27)
			}
			b.StopTimer()
			runtime.KeepAlive(f)
			runtime.KeepAlive(tester)
		})
	}
}

func TestRenderTrafficCellPreviews(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, history := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%t/history=%t", dark, history), func(t *testing.T) {
				item := entry(1, "preview.test")
				item.Path = "/" + strings.Repeat("路径🙂", 1024) + "full-suffix"
				item.URL = "https://preview.test" + item.Path
				var f *fixture
				if history {
					h := newHistoryFixture(t)
					h.history.entries["k1"] = []*logic.TrafficEntry{item}
					h.click("session one")
					f = h.fixture
				} else {
					f = newFixture(t, item)
				}
				f.setDark(dark)
				if _, ok := f.tester.Find(logic.TrafficCellPreview(item.Path)); !ok {
					t.Fatal("bounded path preview not rendered")
				}
				openRowMenu(t, f, "preview.test")
				f.choose("Copy", "Path")
				if f.tester.Clipboard() != item.Path {
					t.Fatal("Copy Path truncated the full value")
				}
				rendertest.Shoot(t, f.tester, fmt.Sprintf("traffic-preview-dark-%t-history-%t", dark, history))
			})
		}
	}
}
