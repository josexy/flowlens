package capture

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/keytest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestCaptureAndHistoryKeyboardNavigation(t *testing.T) {
	f := newHistoryFixture(t)
	keytest.TabTo(t, f.tester, "session one")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.data.History.Traffic.Key != "k1" {
		t.Fatal("keyboard did not open history")
	}
	keytest.TabTo(t, f.tester, locales.MenuCapture())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Workbench.CaptureSelection != store.CaptureItemLive || len(f.backend.restarts) != 0 {
		t.Fatal("capture activation did not select capture independently")
	}
}

func TestCategoryKeyboardNavigationAndHiddenEntries(t *testing.T) {
	f := categoryFixture(t)
	keytest.TabTo(t, f.tester, locales.CategoryTitle()+": api.test")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 1 || f.data.Filter.SelectedHosts[0] != "api.test" {
		t.Fatal("keyboard category activation did not filter")
	}
	keytest.TabTo(t, f.tester, locales.CategoryTitle()+": "+locales.CategoryHost())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.workspace.category.collapsed[sectionHost] {
		t.Fatal("keyboard did not collapse the host group")
	}
	f.showCategories = false
	f.settle()
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.workspace.category.collapsed[sectionHost] || len(f.data.Filter.SelectedHosts) != 1 {
		t.Fatal("hidden category handled a key")
	}
}

func TestFilteredCategoryLosesKeyboardActivation(t *testing.T) {
	f := categoryFixture(t)
	keytest.TabTo(t, f.tester, locales.CategoryTitle()+": api.test")
	f.workspace.category.search = "cdn.test"
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 0 {
		t.Fatal("filtered-out host handled a key")
	}
}
