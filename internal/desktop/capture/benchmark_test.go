package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	nui "github.com/josexy/nuxt-ui-go/ui"
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
	first, _ := table.state.List.Visible()
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

// BenchmarkTrafficTableLive measures a complete capture-pane update after one
// explicit batch flush. Setup, warm-up, database creation and cleanup are not
// timed. No sleeps are included: the 100 ms production batching interval can
// be used to convert B/op to allocation throughput at ten flushes per second.
func BenchmarkTrafficTableLive(b *testing.B) {
	for _, count := range []int{500, store.TrafficEntryCap} {
		for _, mode := range []string{"Idle", "Patch1", "SelectedPatch1", "AllSelectedPatch1", "Patch100", "Patch1000", "SortedPatch100", "Churn100", "Churn1000"} {
			churn := mode == "Churn100" || mode == "Churn1000"
			if (churn || mode == "Patch1000" || mode == "SelectedPatch1" || mode == "AllSelectedPatch1") && count != store.TrafficEntryCap {
				continue
			}
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				f := newFixtureSized(b, rangeOfEntries(count), 1400, 800)
				table := f.workspace.Table
				if mode == "SortedPatch100" {
					table.state.Sort = ui.SortOrder{Column: string(logic.ColumnStatusCode)}
					f.settle()
				}
				if mode == "SelectedPatch1" || mode == "AllSelectedPatch1" {
					table.replaceSelection(count - 1)
					if mode == "AllSelectedPatch1" {
						for _, entry := range table.rows {
							table.state.Selection.Add(entry.ID)
						}
					}
					f.settle()
				}
				if len(table.rows) != count {
					b.Fatalf("requested %d rows, actually showing %d", count, len(table.rows))
				}
				revision, nextID := uint64(2), uint64(count+1)
				step := func() {
					batch := 100
					switch mode {
					case "Patch1", "SelectedPatch1", "AllSelectedPatch1":
						batch = 1
					case "Patch1000", "Churn1000":
						batch = 1000
					}
					if mode != "Idle" {
						for index := range batch {
							if churn {
								f.data.Traffic.AddOrUpdate(entry(nextID, "new.test"))
								nextID++
								continue
							}
							current := f.data.Traffic.Entries[len(f.data.Traffic.Entries)-batch+index]
							updated := *current
							updated.Revision = revision
							updated.StatusCode = 200 + int((revision+uint64(index))%300)
							f.data.Traffic.AddOrUpdate(&updated)
						}
						f.data.Traffic.FlushPending()
						f.window.Drain()
						revision++
					}
					f.tester.Frame()
				}
				for range 3 {
					step()
				}
				table.CellBuilds = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					step()
				}
				b.StopTimer()
				b.ReportMetric(float64(table.CellBuilds)/float64(b.N), "cells/op")
				if len(table.rows) != count {
					b.Fatalf("row count changed to %d", len(table.rows))
				}
				if churn && table.rows[len(table.rows)-1].ID != nextID-1 {
					b.Fatal("new requests were queued instead of displayed")
				}
				if mode != "Idle" && !churn && f.data.Traffic.Entries[count-1].Revision != revision-1 {
					b.Fatal("patches were not applied")
				}
			})
		}
	}
}

// benchmarkUncappedSource supplies historical-sized snapshots without silently
// truncating them to the live store's 5,000-row cap. Only filtering and the table
// surface participate; networking, disk I/O and the capture sidebar do not.
type benchmarkUncappedSource struct {
	Source
	filter *store.FilterStore
	marked bool
}

func (s benchmarkUncappedSource) Filter() *store.FilterStore { return s.filter }
func (s benchmarkUncappedSource) Live() LiveControl          { return nil }

func (s benchmarkUncappedSource) Highlight(id uint64) string {
	if s.marked {
		return "#ef4444"
	}
	return s.Source.Highlight(id)
}

// collectedHeap measures live objects, not HeapInuse (which includes unused
// space in spans). Two collections drop otherwise idle sync.Pool generations.
func collectedHeap() runtime.MemStats {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats
}

// writeTableHeapProfile optionally saves the warmed live fixture, before its
// timed loop or cleanup. Ordinary benchmarks and tests create no files.
func writeTableHeapProfile(b *testing.B) {
	b.Helper()
	dir := os.Getenv("FLOWLENS_TABLE_HEAP_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	name := strings.ReplaceAll(b.Name(), "/", "-") + ".pprof"
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		b.Fatal(err)
	}
	err = pprof.WriteHeapProfile(file)
	closeErr := file.Close()
	if err != nil {
		b.Fatal(err)
	}
	if closeErr != nil {
		b.Fatal(closeErr)
	}
}

func BenchmarkTrafficTableStress(b *testing.B) {
	for _, count := range []int{5000, 50000, 100000} {
		for _, mode := range []string{"Idle", "Patch1", "Marked", "FixedPath", "MarkedFixedPath", "ReorderedHidden", "MarkedReorderedHidden"} {
			if count == 100000 && mode != "Idle" && mode != "Patch1" {
				continue
			}
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				f := newFixtureSized(b, rangeOfEntries(1), 1400, 800)
				f.setDark(false)
				changed := mode == "Patch1"
				entries, version := rangeOfEntries(count), uint64(1)
				filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
				table := NewTable(benchmarkUncappedSource{
					Source: f.workspace.Table.source, filter: filter, marked: strings.HasPrefix(mode, "Marked"),
				}, f.workspace.Table.deps)
				if strings.HasSuffix(mode, "ReorderedHidden") {
					f.data.Settings.SetTrafficColumnVisible(logic.ColumnProcess, false, nil)
					f.settle()
				}
				table.loadLayout()
				if strings.HasSuffix(mode, "FixedPath") {
					table.state.List.Columns.Widths = map[string]float32{"path": 160}
				}
				if strings.HasSuffix(mode, "ReorderedHidden") {
					order := slices.DeleteFunc(slices.Clone(table.fullOrder), func(key string) bool { return key == "path" })
					table.state.List.Columns.Order = append(order, "path")
				}
				// Persist/warm layout and data before measuring UI retention. The
				// baseline already owns every record, sorted index and Nuxt row ID.
				table.syncLayout(table.visibleColumns())
				f.settle()
				table.refreshRows()
				before := collectedHeap()
				tester := ui.NewTester(func(c *ui.Context) {
					theme := nui.App(c, f.data.Settings.Theme().Config)
					table.Build(c, theme)
				}, 1160, 714)
				tester.SetLocale("en")
				tester.SetDark(false)
				step := func() {
					if changed {
						version++
						updated := *entries[len(entries)-1]
						updated.Revision, updated.StatusCode = version, 200+int(version%300)
						entries[len(entries)-1] = &updated
					}
					tester.Frame()
				}
				for range 8 {
					step()
				}
				if len(table.rows) != count {
					b.Fatalf("requested %d rows, actually showing %d", count, len(table.rows))
				}
				after := collectedHeap()
				retainedBytes := int64(after.HeapAlloc) - int64(before.HeapAlloc)
				retainedObjects := int64(after.HeapObjects) - int64(before.HeapObjects)
				writeTableHeapProfile(b)
				table.CellBuilds = 0
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					step()
				}
				b.StopTimer()
				b.ReportMetric(float64(table.CellBuilds)/float64(b.N), "cells/op")
				b.ReportMetric(float64(retainedBytes), "ui-retained-B")
				b.ReportMetric(float64(retainedObjects), "ui-retained-objects")
				if len(table.rows) != count || table.rows[count-1].Revision != version {
					b.Fatal("stress table did not retain or update every row")
				}
				// Keep both sides of the measurement reachable through the sample
				// and timed frames; setup and forced GC are never timed.
				runtime.KeepAlive(tester)
				runtime.KeepAlive(f)
			})
		}
	}
}

// BenchmarkTrafficSetRows is a dependency microbenchmark of Nuxt's record copy,
// ID validation, and the former string/index adapter. The production traffic
// table now binds numeric IDs directly; FullAdapter does not measure that path.
func BenchmarkTrafficSetRows(b *testing.B) {
	for _, count := range []int{500, 5000, 50000, 100000} {
		for _, mode := range []string{"CachedIDs", "FormatIDs", "FullAdapter"} {
			b.Run(fmt.Sprintf("Rows%d/%s", count, mode), func(b *testing.B) {
				indices, ids := make([]int, count), make([]string, count)
				for index := range indices {
					indices[index], ids[index] = index, strconv.FormatUint(uint64(index+1), 10)
				}
				key := func(index int) string { return ids[index] }
				if mode != "CachedIDs" {
					key = func(index int) string { return strconv.FormatUint(uint64(index+1), 10) }
				}
				var state nui.TableState[int]
				state.SetRows(indices, key)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode == "FullAdapter" {
						indices = make([]int, count)
						for index := range indices {
							indices[index] = index
						}
					}
					state.SetRows(indices, key)
				}
				b.StopTimer()
				runtime.KeepAlive(&state)
			})
		}
	}
}

// BenchmarkTrafficTableStorage measures retained filtering, sorting and native
// identity storage separately from records and visible UI elements.
func BenchmarkTrafficTableStorage(b *testing.B) {
	for _, policy := range []string{"Snapshot", "Live"} {
		for _, count := range []int{5000, 100000} {
			for _, mode := range []string{"All", "Filtered", "Sorted", "FilteredSorted", "SelectionClear", "Shrink"} {
				b.Run(fmt.Sprintf("%s/Rows%d/%s", policy, count, mode), func(b *testing.B) {
					f := newFixtureSized(b, rangeOfEntries(1), 1400, 800)
					records := rangeOfEntries(count)
					entries, version := records, uint64(1)
					before := collectedHeap()
					filter := benchmarkFilter(func() ([]*logic.TrafficEntry, uint64) { return entries, version }, policy)
					if mode == "Filtered" || mode == "FilteredSorted" {
						filter.SearchText = "h"
					}
					table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
					if mode == "Sorted" || mode == "FilteredSorted" {
						table.state.Sort = ui.SortOrder{Column: string(logic.ColumnStatusCode)}
					}
					table.refreshRows()
					var selectedBytes int64
					if mode == "SelectionClear" || mode == "Shrink" {
						unselected := collectedHeap()
						for _, row := range table.rows {
							table.state.Selection.Add(row.ID)
						}
						selectedBytes = int64(collectedHeap().HeapAlloc) - int64(unselected.HeapAlloc)
						if table.state.Selection.Len() != count {
							b.Fatal("Select All did not select the entire source")
						}
						if mode == "SelectionClear" {
							table.state.ClearSelection()
							table.pruneSelection()
						} else {
							entries, version = entries[:2], version+1
							table.refreshRows()
							table.state.ClearSelection()
							table.pruneSelection()
						}
					}
					want := count
					if mode == "Shrink" {
						want = 2
					}
					if len(table.rows) != want || len(records) != count || table.state.Selection.Len() != 0 {
						b.Fatal("storage sample changed source count or retained selected rows")
					}
					after := collectedHeap()
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						table.refreshRows()
					}
					b.StopTimer()
					b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "storage-B")
					if selectedBytes != 0 {
						b.ReportMetric(float64(selectedBytes), "selected-B")
					}
					runtime.KeepAlive(records)
					runtime.KeepAlive(table)
					runtime.KeepAlive(f)
				})
			}
		}
	}
}

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
