package capture

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Mutable Patch1 fixtures always remain live, independently of Live controls.
func benchmarkFilter(source store.TrafficSource, policy string) *store.FilterStore {
	switch policy {
	case "Snapshot":
		return store.NewFilterStore(source, logic.TrafficSnapshot)
	case "Live":
		return store.NewFilterStore(source, logic.TrafficLive)
	default:
		panic("unknown benchmark processing policy: " + policy)
	}
}

// Read only scalar counts from the pinned Tester's engine. This diagnostic
// never accesses renderer pointers or changes framework state.
func benchmarkNodeCounts(tester *ui.Tester) (nodes, states int) {
	rt := reflect.ValueOf(tester).Elem().FieldByName("rt").Elem()
	return int(rt.FieldByName("c").FieldByName("used").Int()), rt.FieldByName("states").Len()
}

func BenchmarkTrafficTableViewport(b *testing.B) {
	for _, count := range []int{5000, 50000, 100000} {
		for _, height := range []int{714, 1414, 2114} {
			b.Run(fmt.Sprintf("Rows%d/Height%d", count, height), func(b *testing.B) {
				f := newFixtureSized(b, rangeOfEntries(1), 1400, 800)
				entries := rangeOfEntries(count)
				filter := benchmarkFilter(func() ([]*logic.TrafficEntry, uint64) { return entries, 1 }, "Snapshot")
				table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
				table.refreshRows()
				before := collectedHeap()
				tester := ui.NewTester(func(c *ui.Context) {
					table.Build(c, nui.App(c, f.data.Settings.Theme().Config))
				}, 1160, height)
				tester.SetLocale("en")
				tester.SetDark(false)
				for range 8 {
					tester.Frame()
				}
				after := collectedHeap()
				nodes, states := benchmarkNodeCounts(tester)
				table.CellBuilds = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					tester.Frame()
				}
				b.StopTimer()
				cells := float64(table.CellBuilds) / float64(b.N)
				if len(table.rows) != count || cells == 0 || cells > float64((height/28+4)*11) {
					b.Fatalf("source/viewport changed: rows=%d cells=%g", len(table.rows), cells)
				}
				b.ReportMetric(cells, "cells/op")
				b.ReportMetric(cells/11, "rows/op")
				b.ReportMetric(float64(nodes), "nodes")
				b.ReportMetric(float64(states), "states")
				b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "ui-retained-B")
				runtime.KeepAlive(entries)
				runtime.KeepAlive(tester)
				runtime.KeepAlive(f)
			})
		}
	}
}

func BenchmarkHistorySidebar(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("Sessions%d", count), func(b *testing.B) {
			f := newFixtureSized(b, nil, 1400, 800, &fakeHistoryBackend{})
			metadata := make([]*proxyservice.HistoryMetadata, count)
			for i := range metadata {
				metadata[i] = &proxyservice.HistoryMetadata{Key: fmt.Sprintf("session-%d", i), Alias: fmt.Sprintf("Session %d", i), CreatedAt: 1700000000000 + int64(i), Total: 100000}
			}
			f.data.History.Metadata = metadata
			before := collectedHeap()
			tester := ui.NewTester(func(c *ui.Context) {
				theme := nui.App(c, f.data.Settings.Theme().Config)
				ui.Column(c).Fill().Children(func() { f.workspace.historySidebar(c, theme) })
			}, 240, 714)
			tester.SetLocale("en")
			tester.SetDark(false)
			for range 8 {
				tester.Frame()
			}
			after := collectedHeap()
			nodes, states := benchmarkNodeCounts(tester)
			f.workspace.historyRowBuilds = 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tester.Frame()
			}
			b.StopTimer()
			if len(f.data.History.Metadata) != count || len(f.data.History.Traffic.Entries) != 0 {
				b.Fatal("sidebar changed metadata or loaded unopened history entries")
			}
			if rows := float64(f.workspace.historyRowBuilds) / float64(b.N); rows == 0 || rows > 25 {
				b.Fatalf("sidebar built %g rows per frame", rows)
			}
			b.ReportMetric(float64(f.workspace.historyRowBuilds)/float64(b.N), "rows/op")
			b.ReportMetric(float64(nodes), "nodes")
			b.ReportMetric(float64(states), "states")
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "ui-retained-B")
			runtime.KeepAlive(tester)
			runtime.KeepAlive(f)
		})
	}
}

func BenchmarkCategorySidebar(b *testing.B) {
	for _, mode := range []string{"Unchanged", "Search"} {
		b.Run("Rows100000/"+mode, func(b *testing.B) {
			f := newFixtureSized(b, nil, 1400, 800, &fakeHistoryBackend{})
			entries := rangeOfEntries(100000)
			f.data.History.Traffic.Load("benchmark", entries)
			f.data.Workbench.SelectCaptureItem(historyItemPrefix + "benchmark")
			before := collectedHeap()
			tester := ui.NewTester(func(c *ui.Context) {
				f.workspace.CategorySidebar(c, nui.App(c, f.data.Settings.Theme().Config))
			}, 240, 714)
			tester.SetLocale("en")
			tester.SetDark(false)
			for range 8 {
				tester.Frame()
			}
			after := collectedHeap()
			v := &f.workspace.category
			v.facetBuilds, v.rootBuilds, v.itemBuilds = 0, 0, 0
			iteration := 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if mode == "Search" {
					v.search = fmt.Sprintf("h%d", iteration%50)
					iteration++
				}
				tester.Frame()
			}
			b.StopTimer()
			if len(f.data.History.Traffic.Entries) != 100000 {
				b.Fatal("category fixture lost source rows")
			}
			if mode == "Unchanged" && (v.facetBuilds != 0 || v.rootBuilds != 0 || v.itemBuilds != 0) {
				b.Fatal("unchanged category frame recomputed derived data")
			}
			b.ReportMetric(float64(v.facetBuilds)/float64(b.N), "facets/op")
			b.ReportMetric(float64(v.rootBuilds)/float64(b.N), "roots/op")
			b.ReportMetric(float64(v.itemBuilds)/float64(b.N), "items/op")
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "derived-ui-B")
			runtime.KeepAlive(entries)
			runtime.KeepAlive(tester)
			runtime.KeepAlive(f)
		})
	}
}
