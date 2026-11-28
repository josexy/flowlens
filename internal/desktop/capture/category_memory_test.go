package capture

import (
	"runtime"
	"slices"
	"testing"
	"time"
	"weak"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestCategoryCachesFollowBaseRevisionSearchAndLocale(t *testing.T) {
	f := newFixture(t, entry(1, "chosen.test"), entry(2, "outside.test"))
	f.showCategories = true
	f.settle()
	v := &f.workspace.category
	before := [3]int{v.facetBuilds, v.rootBuilds, v.itemBuilds}
	for range 8 {
		f.tester.Frame()
	}
	if before != [3]int{v.facetBuilds, v.rootBuilds, v.itemBuilds} {
		t.Fatal("unchanged frame rebuilt categories")
	}
	f.data.Filter.SelectedHosts = []string{"chosen.test"}
	f.settle()
	before = [3]int{v.facetBuilds, v.rootBuilds, v.itemBuilds}
	updated := entry(2, "new-outside.test")
	updated.Revision = 2
	f.data.Traffic.AddOrUpdate(updated)
	f.data.Traffic.FlushPending()
	f.window.Drain()
	f.tester.Advance(time.Second)
	f.settle()
	if v.facetBuilds != before[0]+1 || v.rootBuilds != before[1] || !slices.ContainsFunc(v.hosts, func(h logic.HostSummaryItem) bool { return h.Host == "new-outside.test" }) {
		t.Fatal("base-only update did not refresh facets independently")
	}
	before = [3]int{v.facetBuilds, v.rootBuilds, v.itemBuilds}
	v.search = "new-outside"
	f.settle()
	if v.facetBuilds != before[0] || v.rootBuilds != before[1] || v.itemBuilds != before[2]+2 || !slices.Equal(f.data.Filter.SelectedHosts, []string{"chosen.test"}) {
		t.Fatal("sidebar search rebuilt source data or lost hidden selection")
	}
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	f.tester.SetLocale("zh-Hans")
	f.settle()
	if v.facetBuilds != before[0]+1 || v.fallback != locales.CategoryProcessUnidentified() {
		t.Fatal("locale did not invalidate labels")
	}
}

func TestCategoryIntentSurvivesTemporaryTableFilter(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
	f.showCategories = true
	f.settle()
	v := &f.workspace.category
	const key = "host:b.test"
	if !slices.ContainsFunc(v.roots, func(root *logic.StructureNode) bool { return root.Key == key }) {
		t.Fatalf("missing %s root", key)
	}
	v.expanded[key] = true
	f.data.Filter.SearchText = "no-match"
	f.settle()
	f.data.Filter.SearchText = ""
	f.settle()
	if !v.expanded[key] {
		t.Fatal("a temporary table search discarded category expansion")
	}
}

func TestCategoryHideReleasesLargeTreeAndPreservesIntent(t *testing.T) {
	f := newFixtureSized(t, nil, 1400, 800, &fakeHistoryBackend{})
	entries := rangeOfEntries(100000)
	f.data.History.Traffic.Load("large", entries)
	f.data.Workbench.SelectCaptureItem(historyItemPrefix + "large")
	f.showCategories = true
	f.settle()
	v := &f.workspace.category
	rootKey := v.roots[0].Key
	v.expanded[rootKey] = true
	v.search = "h"
	f.data.History.Filter.SelectedHosts = []string{entries[0].Host}
	f.settle()
	rootKey = v.roots[0].Key
	v.expanded[rootKey] = true
	v.scroll.Y = 50
	oldRoot := weak.Make(v.roots[0])
	menu := f.workspace.categoryMenus.For("hidden")
	menu.Open = true
	f.showCategories = false
	f.workspace.CategorySidebarHidden()
	if v.entryCount != 0 || v.roots != nil || v.treeItems != nil || v.hostSelection.Len() != 0 || v.processSelection.Len() != 0 || menu.Open {
		t.Fatal("hidden category retains derived data/menu")
	}
	if v.context != "history:large" || v.search != "h" || !v.expanded[rootKey] || v.scroll.Y != 50 || len(f.data.History.Filter.SelectedHosts) != 1 {
		t.Fatal("hide discarded same-source intent")
	}
	f.settle()
	runtime.GC()
	runtime.GC()
	if oldRoot.Value() != nil {
		t.Fatal("hidden sidebar retained old tree through UI callbacks")
	}
	before := [3]int{v.facetBuilds, v.rootBuilds, v.itemBuilds}
	f.showCategories = true
	f.settle()
	if v.facetBuilds != before[0]+1 || v.rootBuilds != before[1]+1 || v.itemBuilds != before[2]+2 || !v.expanded[rootKey] {
		t.Fatal("reopen failed to rebuild exactly once/restore expansion")
	}
	if v.scroll.Y > v.scroll.MaxY {
		t.Fatal("reopen did not clamp scroll")
	}
	f.showCategories = false
	f.workspace.CategorySidebarHidden()
	f.data.History.Traffic.Load("replacement", []*logic.TrafficEntry{entry(1, "fresh.test")})
	f.data.Workbench.SelectCaptureItem(historyItemPrefix + "replacement")
	if v.context != "" || len(v.expanded) != 0 || v.activeLeaf != "" {
		t.Fatal("hidden source replacement kept source intent")
	}
	f.data.History.CloseOpened()
	if v.entryCount != 0 || v.roots != nil {
		t.Fatal("hidden close restored derived data")
	}
	runtime.KeepAlive(f)
}

func TestHistoryMemoryCyclesSettleAfterCollection(t *testing.T) {
	f := newFixtureSized(t, nil, 1400, 800, &fakeHistoryBackend{})
	beforeRecords := collectedHeap()
	records := rangeOfEntries(100000)
	afterRecords := collectedHeap()
	cycle := func() {
		f.data.History.Traffic.Load("cycle", slices.Clone(records))
		f.data.Workbench.SelectCaptureItem(historyItemPrefix + "cycle")
		f.data.History.Filter.SearchText = "h"
		table := f.workspace.HistoryTable
		table.state.Sort = ui.SortOrder{Column: string(logic.ColumnStatusCode)}
		f.showCategories = true
		f.settle()
		for _, row := range table.rows {
			table.state.Selection.Add(row.ID)
		}
		table.state.ClearSelection()
		f.data.History.Filter.SearchText = "h1.test"
		f.settle()
		f.showCategories = false
		f.workspace.CategorySidebarHidden()
		f.data.History.CloseOpened()
		f.data.Workbench.SelectCaptureItem(store.CaptureItemLive)
		f.settle()
		if table.rows != nil || table.state.Len() != 0 || f.workspace.category.entryCount != 0 || f.workspace.category.treeItems != nil {
			t.Fatal("cycle left history working state reachable")
		}
	}
	cycle()
	before := collectedHeap()
	for range 4 {
		cycle()
	}
	after := collectedHeap()
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("100k source records: %d B; four post-warmup cycles: heap delta=%d B, object delta=%d", int64(afterRecords.HeapAlloc)-int64(beforeRecords.HeapAlloc), growth, int64(after.HeapObjects)-int64(before.HeapObjects))
	if growth > 2<<20 {
		t.Fatalf("collected lifecycle heap grew by %d B", growth)
	}
	runtime.KeepAlive(records)
	runtime.KeepAlive(f)
}
