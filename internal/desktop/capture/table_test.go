package capture

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestNativeSelectionFollowsIdentityAndDisplayedRange(t *testing.T) {
	f := newFixture(t, entry(1, "d.test"), entry(2, "b.test"), entry(3, "a.test"), entry(4, "c.test"))
	f.clickRow(0, "b.test")
	table := f.workspace.Table
	table.state.Sort = ui.SortOrder{Column: string(logic.ColumnHost)}
	table.lastSort = table.state.Sort
	f.settle()
	f.requireSelection([]uint64{2}, 2, false)
	f.clickRow(ui.Shift, "d.test")
	f.requireSelection([]uint64{2, 4, 1}, 1, false)
	f.data.Filter.SearchText = "b.test"
	f.settle()
	f.requireSelection([]uint64{2}, 2, false)
	f.data.Filter.SearchText = "no-such-host"
	f.settle()
	f.requireSelection(nil, 0, false)
}

func TestNativeDoubleClickTogglesDetails(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			name := map[bool]string{false: "live", true: "history"}[history] + "/" + map[bool]string{false: "light", true: "dark"}[dark]
			t.Run(name, func(t *testing.T) {
				f := newHistoryFixture(t, manyEntries(3)...).fixture
				f.setDark(dark)
				table := f.workspace.Table
				var data inspect.Data = inspect.LiveData{Traffic: f.data.Traffic}
				if history {
					f.click("session one")
					table = f.workspace.HistoryTable
					data = store.HistoryDetailData{History: f.data.History}
				}
				panel := inspect.NewPanel(data, inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier})
				panel.OnClose = table.CloseDetails
				if history {
					f.workspace.HistoryDetail = panel.Build
				} else {
					f.workspace.Detail = panel.Build
				}
				f.settle()
				first, second := table.rows[0], table.rows[1]
				check := func(ids []uint64, focused uint64, visible bool) {
					t.Helper()
					entry := table.source.SelectedEntry()
					if got := selectedIDs(table); !slices.Equal(got, ids) || entry == nil || entry.ID != focused || table.source.ShowDetail() != visible {
						t.Fatalf("selected %v focused %+v detail %v, want %v %d %v", got, entry, table.source.ShowDetail(), ids, focused, visible)
					}
					if !f.tester.Focused(locales.TrafficColumns()) {
						t.Fatal("table lost keyboard focus")
					}
				}
				for _, visible := range []bool{true, false, true} {
					f.tester.Advance(time.Second)
					f.clickRow(0, first.Host)
					check([]uint64{first.ID}, first.ID, !visible)
					f.clickRow(0, first.Host)
					check([]uint64{first.ID}, first.ID, visible)
				}
				// Enter opens details even after a double-click; it never hides them.
				for range 2 {
					f.tester.Key(0, ui.KeyEnter)
					f.settle()
					check([]uint64{first.ID}, first.ID, true)
				}
				// Modifier clicks only change selection, even when repeated quickly.
				for _, visible := range []bool{true, false} {
					if !visible {
						table.CloseDetails()
						f.settle()
					}
					f.tester.Advance(time.Second)
					f.clickRow(ui.Cmd, second.Host)
					check([]uint64{first.ID, second.ID}, second.ID, visible)
					f.clickRow(ui.Cmd, second.Host)
					check([]uint64{first.ID}, first.ID, visible)
				}
				// Restore the native cursor from the deselected second row.
				f.tester.Advance(time.Second)
				f.clickRow(0, first.Host)
				check([]uint64{first.ID}, first.ID, false)
				f.tester.Key(0, ui.KeyEnter)
				f.settle()
				check([]uint64{first.ID}, first.ID, true)
				// The first click changes the inspected row; the second hides details.
				f.tester.Advance(time.Second)
				f.clickRow(0, second.Host)
				check([]uint64{second.ID}, second.ID, true)
				f.clickRow(0, second.Host)
				check([]uint64{second.ID}, second.ID, false)
			})
		}
	}
}

func TestDetailCloseRetainsSelectionAndReturnsFocus(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "history"}[history], func(t *testing.T) {
			h := newHistoryFixture(t, entry(1, "a.test"), entry(2, "b.test"))
			f := h.fixture
			table := f.workspace.Table
			var data inspect.Data = inspect.LiveData{Traffic: f.data.Traffic}
			if history {
				f.click("session one")
				table = f.workspace.HistoryTable
				data = store.HistoryDetailData{History: f.data.History}
			}
			panel := inspect.NewPanel(data, inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier})
			panel.OnClose = table.CloseDetails
			if history {
				f.workspace.HistoryDetail = panel.Build
			} else {
				f.workspace.Detail = panel.Build
			}
			f.settle()
			// The history fixture uses its own traffic hosts.
			row := table.rows[0]
			f.openDetails(row.Host)
			keytest.TabTo(t, f.tester, locales.DetailClosePanel())
			f.tester.Key(0, ui.KeySpace)
			f.settle()
			if table.source.ShowDetail() || !table.state.Selection.Has(row.ID) || table.state.Selection.Len() != 1 || !f.tester.Focused(locales.TrafficColumns()) {
				t.Fatalf("close: visible %v selected %v focus %v", table.source.ShowDetail(), table.selectedIDs(), f.tester.Focused(locales.TrafficColumns()))
			}
			f.tester.Key(0, ui.KeyDown)
			f.settle()
			if table.source.ShowDetail() {
				t.Fatal("selection reopened details")
			}
		})
	}
}

func TestDetailEnterStaysWithinTheTable(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.clickRow(0, "host1.test")
	f.tester.Key(ui.Cmd, ui.KeyF)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.requireSelection([]uint64{1}, 1, false)
	f.confirm.Ask(widgets.ConfirmRequest{Title: "blocking", Message: "modal", Confirm: widgets.ConfirmAction{Label: "okay"}})
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Traffic.ShowDetailPanel {
		t.Fatal("Enter in a modal opened details behind it")
	}
}

func TestLateDetailBodyCannotReplaceTheCurrentView(t *testing.T) {
	for _, closePanel := range []bool{false, true} {
		t.Run(map[bool]string{false: "switch", true: "close"}[closePanel], func(t *testing.T) {
			f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
			started, release := make(chan struct{}), make(chan struct{})
			f.backend.readBody = func(id uint64) (*proxyservice.TrafficBodyView, error) {
				if id == 1 {
					close(started)
					<-release
					return &proxyservice.TrafficBodyView{ResponseBody: "late-a"}, nil
				}
				return &proxyservice.TrafficBodyView{ResponseBody: "current-b"}, nil
			}
			f.clickRow(0, "a.test")
			f.tester.Key(0, ui.KeyEnter)
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("body request did not start")
			}
			if closePanel {
				f.workspace.Table.CloseDetails()
			} else if err := f.tester.Click("b.test"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			f.settle()
			body := f.data.Traffic.BodyView
			if closePanel && body != nil || !closePanel && (body == nil || body.ResponseBody != "current-b") {
				t.Fatalf("late body published: %+v", body)
			}
		})
	}
}

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
			if table.state.Selection.Len() != 1 || !table.state.Selection.Has(5000) {
				t.Fatal("stable patch changed selection")
			}
			entries, version = entries[:2], version+1
			table.refreshRows()
			if table.state.Selection.Len() != 0 || table.selectedRow != -1 || len(table.rows) != 2 ||
				table.state.Len() != 2 {
				t.Fatal("shrinking failed to prune selection or release identity storage")
			}
			entries, version = nil, version+1
			table.refreshRows()
			if table.state.Len() != 0 || len(table.rows) != 0 {
				t.Fatal("empty table retained identity storage")
			}
		})
	}
}

func TestTableAllSelectedPatchesHaveBoundedAllocations(t *testing.T) {
	f := newFixtureSized(t, rangeOfEntries(1), 1400, 800)
	entries, version := rangeOfEntries(10000), uint64(1)
	filter := store.NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return entries, version })
	table := NewTable(benchmarkUncappedSource{Source: f.workspace.Table.source, filter: filter}, f.workspace.Table.deps)
	table.refreshRows()
	table.replaceSelection(len(entries) - 1)
	for _, entry := range entries {
		table.state.Selection.Add(entry.ID)
	}
	original := entries[len(entries)-1]
	replacement := *original
	replacement.Path = "/updated-payload"
	patch := func() {
		version++
		if version%2 == 0 {
			entries[len(entries)-1] = &replacement
		} else {
			entries[len(entries)-1] = original
		}
		changed := table.refreshRows()
		table.syncChoice(changed)
		if !changed || table.state.Selection.Len() != len(entries) || table.selectedRow != len(entries)-1 ||
			table.source.SelectedEntry() != entries[len(entries)-1] {
			t.Fatal("patch lost selected identities, cursor or current detail")
		}
	}
	patch() // Warm filter/sort storage before measuring the complete refresh.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const patches = 20
	for range patches {
		patch()
	}
	runtime.ReadMemStats(&after)
	// Rebinding the same IDs must not copy the entire selection into a scratch
	// map. Count bytes, since even a large map can use only a few allocations.
	if perPatch := (after.TotalAlloc - before.TotalAlloc) / patches; perPatch > 16*1024 {
		t.Fatalf("all-selected patch allocated %d bytes; want at most 16 KiB", perPatch)
	}
	entries, version = entries[:2], version+1
	table.syncChoice(table.refreshRows())
	if table.state.Selection.Len() != 2 || !table.state.Selection.Has(entries[0].ID) ||
		!table.state.Selection.Has(entries[1].ID) || table.source.SelectedEntry() != entries[0] {
		t.Fatal("shrinking did not prune missing IDs and restore a surviving detail")
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
