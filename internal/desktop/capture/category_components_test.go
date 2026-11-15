package capture

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
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
