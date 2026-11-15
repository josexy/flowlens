package capture

import (
	"slices"
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
	f.tabToCategory(ui.RoleListItem, "api.test")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 1 || f.data.Filter.SelectedHosts[0] != "api.test" {
		t.Fatal("keyboard category activation did not filter")
	}
	f.tabToCategory(ui.RoleDisclosure, locales.CategoryHost())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if slices.Contains(f.workspace.category.sections, sectionHost) {
		t.Fatal("keyboard did not collapse the host group")
	}
	f.showCategories = false
	f.settle()
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if slices.Contains(f.workspace.category.sections, sectionHost) || len(f.data.Filter.SelectedHosts) != 1 {
		t.Fatal("hidden category handled a key")
	}
}

func TestCategoryListboxKeyboardFollowsVisibleItems(t *testing.T) {
	f := categoryFixture(t)
	f.tabToCategory(ui.RoleListItem, "api.test")
	f.workspace.category.search = "cdn.test"
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !slices.Equal(f.data.Filter.SelectedHosts, []string{"cdn.test"}) {
		t.Fatalf("keyboard selected a filtered-out host: %v", f.data.Filter.SelectedHosts)
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 0 {
		t.Fatal("Space did not clear the selected host")
	}
}

func (f *fixture) tabToCategory(role ui.Role, label string) {
	f.t.Helper()
	for range 64 {
		for _, item := range f.tester.AccessAll() {
			if item.Role == role && item.Name == label && item.Focused {
				return
			}
		}
		f.tester.Key(0, ui.KeyTab)
	}
	f.t.Fatalf("keyboard focus cannot reach %q", label)
}
