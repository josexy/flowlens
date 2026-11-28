package capture

import (
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
	"weak"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func TestCategoryListboxKeepsHiddenSelections(t *testing.T) {
	f := categoryFixture(t)
	f.click("api.test")
	f.click("cdn.test")
	f.workspace.category.search = "cdn"
	f.settle()
	f.click("cdn.test")
	if !slices.Equal(f.data.Filter.SelectedHosts, []string{"api.test"}) {
		t.Fatalf("search discarded a hidden selection: %v", f.data.Filter.SelectedHosts)
	}
	f.workspace.category.search = ""
	f.settle()
	f.click(locales.CategoryHost())
	f.click(locales.CategoryHost())
	if !f.checkMark("api.test") || f.checkMark("cdn.test") {
		t.Fatal("listbox did not restore selection after remounting")
	}
	f.click("api.test")
	f.click("Browser")
	f.click(locales.CategoryProcessUnidentified())
	if len(f.data.Filter.SelectedProcessKeys) != 2 || len(f.data.Filter.Filtered()) != 2 {
		t.Fatalf("process multiselect: %v", f.data.Filter.SelectedProcessKeys)
	}
	f.click(locales.CategoryProcess())
	f.click(locales.CategoryProcess())
	if !f.checkMark("Browser") || !f.checkMark(locales.CategoryProcessUnidentified()) {
		t.Fatal("process checks were lost after remounting")
	}
	f.click("Browser")
	if !slices.Equal(f.data.Filter.SelectedProcessKeys, []string{logic.ProcessCategoryUnavailableKey}) {
		t.Fatal(f.data.Filter.SelectedProcessKeys)
	}
}

func TestCategoryTreePreservesExpansionAndSelection(t *testing.T) {
	f := categoryFixture(t)
	f.click(locales.CategoryHost())
	f.click(locales.CategoryProcess())
	f.click("cdn.test")
	f.click("assets")
	f.click("app.js")
	leaf := f.workspace.category.activeLeaf
	for _, query := range []string{"api", "", "nothing matches", ""} {
		f.workspace.category.search = query
		f.settle()
	}
	f.data.Traffic.AddOrUpdate(entry(3, "new.test"))
	f.data.Traffic.FlushPending()
	f.tester.Advance(time.Second)
	f.settle()
	item, ok := f.tester.AccessByRole(ui.RoleTreeItem, "app.js")
	if !ok || !item.Selected || f.workspace.category.activeLeaf != leaf {
		t.Fatalf("expanded selected leaf lost after search/refresh: %+v", item)
	}
	f.click(locales.CategoryStructure())
	f.click(locales.CategoryStructure())
	item, ok = f.tester.AccessByRole(ui.RoleTreeItem, "app.js")
	if !ok || !item.Selected {
		t.Fatal("accordion remount lost the selected leaf")
	}
	rendertest.ShootThemes(t, f.tester, "category-expanded", f.setDark)
}

func TestCategoryLeafMenusUseCurrentSource(t *testing.T) {
	for _, source := range []string{"capture", "history"} {
		t.Run(source, func(t *testing.T) {
			live := entry(1, "api.test")
			saved := entry(1, "api.test")
			live.Path, live.URL = "/item", "http://api.test/item?source=capture"
			saved.Path, saved.URL = "/item", "http://api.test/item?source=history"
			h := newHistoryFixture(t, live)
			f := h.fixture
			f.tester.SetPreferences(ui.Preferences{ReduceMotion: true})
			if source == "history" {
				h.history.entries["k1"] = []*logic.TrafficEntry{saved}
				f.click("session one")
			}
			f.showCategories = true
			f.settle()
			f.click(locales.CategoryHost())
			f.click(locales.CategoryProcess())
			f.click("api.test")
			f.click("item")
			selected := f.data.Traffic.Selected
			if source == "history" {
				selected = f.data.History.Traffic.Selected
			}
			want := "http://api.test/item?source=" + source
			if selected == nil || selected.URL != want {
				t.Fatalf("leaf focused the wrong source: %+v", selected)
			}
			if err := menutest.Open(f.tester, f.settle, "item"); err != nil {
				t.Fatal(err)
			}
			f.choose(locales.ContextMenuTrafficTableCopy(), locales.ContextMenuTrafficTableCopyUrl())
			if got := f.tester.Clipboard(); got != want {
				t.Fatalf("menu copied %q, want %q", got, want)
			}
			// Mouse selection must keep the tree's keyboard navigation and menu.
			f.click("item")
			f.tester.Key(ui.Shift, ui.KeyF10)
			f.settle()
			if !menutest.Shown(f.tester) {
				t.Fatal("Shift+F10 did not open the active leaf menu")
			}
			menutest.Close(f.tester, f.settle)
			f.tester.Key(0, ui.KeyLeft)
			f.tester.Key(0, ui.KeyLeft)
			f.settle()
			if _, ok := f.tester.AccessByRole(ui.RoleTreeItem, "item"); ok {
				t.Fatal("Left did not collapse the parent after using the menu")
			}
		})
	}
}

func TestCategoryGroupsShareOuterScroll(t *testing.T) {
	entries := manyEntries(5000)
	for i, entry := range entries[:80] {
		entry.Metadata = &proxyservice.Metadata{Process: &proxyservice.ProcessInfo{
			Status: proxyservice.ProcessStatusResolved, AppID: fmt.Sprintf("app-%03d", i), DisplayName: fmt.Sprintf("App %03d", i),
		}}
	}
	f := newFixture(t, entries...)
	f.showCategories = true
	f.tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	f.settle()
	view := &f.workspace.category
	if first, last := view.hostList.List.Visible(); last-first > 40 {
		t.Fatalf("host list built an unbounded viewport: %d..%d", first, last)
	}
	f.tester.Scroll(120, 150, 0, 480)
	f.settle()
	if view.scroll.Y != 480 {
		t.Fatalf("wheel over hosts moved an inner list instead of the outer scroller: %v", view.scroll.Y)
	}
	if first, _ := view.hostList.List.Visible(); first < 10 {
		t.Fatalf("host viewport did not follow the outer scroller: first=%d", first)
	}
	// Every group contributes its complete height, while its native control
	// builds only the visible window. Wheel input anywhere moves the same bar.
	for _, group := range []*categoryViewport{&view.processView, &view.structureView} {
		scroll, ok := f.tester.AccessByRole(ui.RoleScroll, locales.CategoryTitle())
		if !ok {
			t.Fatal("no outer category scroller")
		}
		view.scroll.Y += group.bounds.Y - scroll.Bounds.Y
		f.settle()
		before := view.scroll.Y
		f.tester.Scroll(120, 150, 0, 240)
		f.settle()
		if view.scroll.Y != before+240 {
			t.Fatalf("wheel over group did not move outer scroll: %v -> %v", before, view.scroll.Y)
		}
	}
	view.scroll.Y = view.scroll.MaxY
	f.settle()
	if n := view.tree.RowBuilds; n > 40 {
		t.Fatalf("tree built %d rows for its bounded viewport", n)
	}
	if first, last := view.tree.Outline.List.Visible(); first < 4900 || last != len(entries)-1 {
		t.Fatalf("cannot scroll to late tree nodes: first=%d", first)
	}
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		rendertest.SettleTheme(f.tester)
		rendertest.Shoot(t, f.tester, fmt.Sprintf("category-large-dark-%v", dark))
	}
	for _, height := range []int{500, 950} {
		f.tester.SetSize(1400, height)
		f.tester.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.25})
		f.settle()
		view.scroll.Y = view.scroll.MaxY
		f.settle()
		last := view.treeItems[view.tree.Item(view.tree.Rows()-1)].node.Label
		item, ok := f.tester.AccessByRole(ui.RoleTreeItem, last)
		scroll, _ := f.tester.AccessByRole(ui.RoleScroll, locales.CategoryTitle())
		if !ok || item.Offscreen || item.Bounds.Y+item.Bounds.H > scroll.Bounds.Y+scroll.Bounds.H+1 {
			t.Fatalf("last row clipped after resize/text scaling: %+v, viewport=%v", item, scroll.Bounds)
		}
	}
}

func TestCategoryKeyboardRevealsRowsInOuterScroll(t *testing.T) {
	f := newFixture(t, manyEntries(200)...)
	f.tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	f.showCategories = true
	f.settle()
	view := &f.workspace.category
	hosts, _ := view.facets(f.data.Filter)
	f.tabToCategory(ui.RoleListItem, hosts[0].Host)
	f.tester.Key(0, ui.KeyEnd)
	f.settle()
	last := hosts[len(hosts)-1].Host
	if view.scroll.Y == 0 || view.hostList.Active != last {
		t.Fatalf("End failed to scroll the outer container: y=%v, active=%q", view.scroll.Y, view.hostList.Active)
	}
	checkVisible := func(role ui.Role, label string) {
		t.Helper()
		item, ok := f.tester.AccessByRole(role, label)
		viewport, _ := f.tester.AccessByRole(ui.RoleScroll, locales.CategoryTitle())
		if !ok || item.Offscreen || item.Bounds.Y < viewport.Bounds.Y || item.Bounds.Y+item.Bounds.H > viewport.Bounds.Y+viewport.Bounds.H+1 {
			t.Fatalf("%q is outside shared viewport: %+v, viewport=%v", label, item, viewport.Bounds)
		}
	}
	checkVisible(ui.RoleListItem, last)
	f.tester.Key(0, ui.KeyHome)
	f.settle()
	checkVisible(ui.RoleListItem, hosts[0].Host)
	f.tabToCategory(ui.RoleDisclosure, locales.CategoryHost())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.click(locales.CategoryProcess())
	f.tabToCategory(ui.RoleTreeItem, hosts[0].Host)
	f.tester.Key(0, ui.KeyEnd)
	f.settle()
	checkVisible(ui.RoleTreeItem, last)
	f.tester.Key(0, ui.KeyRight)
	f.tester.Key(0, ui.KeyRight)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	leaf := view.treeItems[view.activeLeaf]
	if leaf == nil {
		t.Fatal("keyboard did not select the last request")
	}
	checkVisible(ui.RoleTreeItem, leaf.node.Label)
	before := view.scroll.Y
	for range 8 {
		f.settle()
	}
	if view.scroll.Y != before {
		t.Fatalf("shared scroll drifted without input: %v -> %v", before, view.scroll.Y)
	}
}

func TestSelectingTwoHostsChecksBoth(t *testing.T) {
	f := categoryFixture(t)
	f.click("api.test")
	f.click("cdn.test")
	for _, host := range []string{"api.test", "cdn.test"} {
		if !f.checkMark(host) {
			t.Fatalf("%s has no check mark", host)
		}
	}
	if got := f.data.Filter.SelectedHosts; !slices.Contains(got, "api.test") || !slices.Contains(got, "cdn.test") {
		t.Fatalf("%v", got)
	}
	if n := len(f.data.Filter.Filtered()); n != 2 {
		t.Fatalf("%d rows for either host", n)
	}
	f.click("cdn.test")
	if f.checkMark("cdn.test") {
		t.Fatal("unchosen host still checked")
	}
	if n := len(f.data.Filter.Filtered()); n != 1 {
		t.Fatalf("%d rows", n)
	}
}

// checkMark reports whether the category row of label shows an accent check
// mark at its end.
func (f *fixture) checkMark(label string) bool {
	f.t.Helper()
	item, ok := f.tester.AccessByRole(ui.RoleListItem, label)
	if !ok {
		f.t.Fatalf("no row %q", label)
	}
	row := item.Bounds
	theme := f.theme()
	accent, text, background := rgba(theme.Primary), rgba(theme.Text), rgba(theme.Background)
	frame := f.tester.Image()
	for x := row.X + row.W - 32; x < row.X+row.W-8; x++ {
		for y := row.Y + 4; y < row.Y+row.H-4; y++ {
			if p := frame.RGBAAt(int(x), int(y)); closer(p, accent, background) && closer(p, accent, text) {
				return true
			}
		}
	}
	return false
}

func TestCategoryRowsShowNoCounts(t *testing.T) {
	f := categoryFixture(t)
	// Each host has one entry; 1.0.5 showed no "1" beside it. The sidebar
	// is built first, so a count there would be the first "1".
	if one, ok := f.tester.Find(strconv.Itoa(1)); ok && one.X < 240 {
		t.Fatalf("a count is shown at %v", one)
	}
	item, _ := f.tester.AccessByRole(ui.RoleListItem, "api.test")
	row := item.Bounds
	if row.H < 28 || row.H > 32 {
		t.Fatalf("row %v", row)
	}
}

func TestCategoryAccordionCollapsesAndRestoresItems(t *testing.T) {
	f := categoryFixture(t)
	f.click(locales.CategoryHost())
	if _, ok := f.tester.Find(locales.CategoryTitle() + ": " + locales.CategoryHost()); ok {
		t.Fatal("collapsed host list is still mounted")
	}
	f.click(locales.CategoryHost())
	if _, ok := f.tester.Find(locales.CategoryTitle() + ": " + locales.CategoryHost()); !ok {
		t.Fatal("expanding did not bring the hosts back")
	}
}

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
