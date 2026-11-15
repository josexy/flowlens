package collection

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/menutest"
	"github.com/josexy/flowlens/locales"
)

func (f *fixture) tabToTree() {
	f.t.Helper()
	for range 20 {
		if f.tester.Focused(locales.ApiCollectionTitle()) {
			return
		}
		f.tester.Key(0, ui.KeyTab)
	}
	f.t.Fatal("Tab did not reach the collection tree")
}

func TestCollectionTreeKeyboardNavigationAndSelection(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	id := f.request(folder, "Request", "https://example.test")
	f.reload()
	f.tabToTree()
	f.tester.Key(0, ui.KeyRight)
	f.settle()
	if !f.data.Collection.Expanded[folder] {
		t.Fatal("Right did not expand the folder")
	}
	f.tester.Key(0, ui.KeyDown)
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.data.Workspace.FindTabByAPIID(id) == nil || f.data.Collection.SelectedNodeID != id {
		t.Fatal("keyboard did not open request")
	}
	f.tester.Key(0, ui.KeyLeft)
	f.tester.Key(0, ui.KeyLeft)
	f.settle()
	if f.has("Request") {
		t.Fatal("Left did not collapse the folder")
	}
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Collection.SelectedNodeID != folder || f.data.Collection.Expanded[folder] {
		t.Fatal("Enter must select the folder without expanding it")
	}
	f.tester.Key(0, ui.KeyRight)
	f.click(locales.ApiCollectionMultiSelect())
	f.tabToTree()
	f.tester.Key(0, ui.KeyHome)
	f.tester.Key(0, ui.KeyDown)
	before := len(f.data.Workspace.Tabs)
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.view.checked[id] || len(f.view.checked) != 1 || len(f.data.Workspace.Tabs) != before {
		t.Fatal("multi-selection opened the request or selected other nodes")
	}
	f.tester.Key(0, ui.KeyHome)
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.view.checked[folder] || !f.data.Collection.Expanded[folder] {
		t.Fatal("selecting a folder changed expansion")
	}
}

func TestCollectionTreeKeyboardMenuRestoresFocus(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.reload()
	f.tabToTree()
	f.tester.Key(ui.Shift, ui.KeyF10)
	f.settle()
	if !menutest.Shown(f.tester) || f.data.Collection.SelectedNodeID != folder {
		t.Fatal("keyboard menu did not select its node")
	}
	menutest.Close(f.tester, f.settle)
	if !f.tester.Focused(locales.ApiCollectionTitle()) {
		t.Fatal("closing the menu did not return focus to the tree")
	}
}

func TestCollectionTreeMultiSelectionRangeAndMenu(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	first := f.request(folder, "First", "https://example.test/1")
	second := f.request(folder, "Second", "https://example.test/2")
	third := f.request(folder, "Third", "https://example.test/3")
	f.reload()
	f.click("Folder")
	f.click(locales.ApiCollectionMultiSelect())
	// Indicator clicks must reach Tree, rather than changing only a local
	// Checkbox value or introducing another keyboard focus stop.
	x, y := f.center(locales.ApiCollectionSelectItem("First"))
	f.tester.ClickAt(x, y)
	f.settle()
	if !f.view.checked[first] || !f.tester.Focused(locales.ApiCollectionTitle()) {
		t.Fatal("checkbox click did not select and focus the tree")
	}
	x, y = f.center(locales.ApiCollectionSelectItem("Third"))
	f.tester.ClickAtWith(ui.Shift, x, y)
	f.settle()
	if len(f.view.checked) != 3 || !f.view.checked[first] || !f.view.checked[second] || !f.view.checked[third] || f.view.checked[folder] {
		t.Fatalf("range selection = %v", f.view.checked)
	}
	if f.data.Workspace.FindTabByAPIID(first) != nil || !f.data.Collection.Expanded[folder] {
		t.Fatal("range selection opened a request or collapsed its folder")
	}
	x, y = f.center(locales.ApiCollectionSelectItem("Second"))
	f.tester.ClickAt(x, y)
	f.settle()
	if f.view.checked[second] || len(f.view.checked) != 2 {
		t.Fatal("checkbox click did not clear the tree selection")
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.view.checked[second] || len(f.view.checked) != 3 {
		t.Fatal("checkbox click broke keyboard selection")
	}
	f.menu("Second", locales.ApiCollectionRename())
	if !f.view.renaming.open || f.view.renaming.id != second {
		t.Fatal("multi-selection lost the row context menu")
	}
}
