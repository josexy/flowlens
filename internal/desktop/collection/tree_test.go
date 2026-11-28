package collection

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

func TestTreeRefreshPreservesExpansionSelectionAndKeyboardPosition(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	nested := f.folder(parent, "Nested")
	first := f.request(nested, "First", "https://example.test/1")
	second := f.request(nested, "Second", "https://example.test/2")
	f.reload()
	f.data.Collection.SelectNode(first)
	f.settle()
	f.tabToTree()
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	if f.view.treeState.Active != second {
		t.Fatal("keyboard did not move independently of selection")
	}
	f.reload()
	if f.view.treeState.Active != second || f.data.Collection.SelectedNodeID != first ||
		!f.view.treeState.Outline.Open.Has(parent) || !f.view.treeState.Outline.Open.Has(nested) {
		t.Fatal("refresh lost selection, keyboard position or expansion")
	}
	if err := f.service.DeleteNodes([]string{first}); err != nil {
		t.Fatal(err)
	}
	f.reload()
	if f.view.treeState.Selection.Has(first) || f.data.Collection.SelectedNodeID != "" {
		t.Fatal("refresh retained a deleted selection")
	}
}

func TestTreeVirtualizesAndRevealsCreatedAndMovedFolders(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "Source")
	moved := f.folder(source, "Moved")
	for i := range 100 {
		f.folder("", fmt.Sprintf("Folder %03d", i))
	}
	target := f.folder("", "Last target")
	f.reload()
	if f.view.treeState.RowBuilds >= 40 {
		t.Fatalf("built %d rows for a 700-DIP sidebar", f.view.treeState.RowBuilds)
	}
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("Created at end")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	created := f.data.Collection.SelectedNodeID
	if !f.has("Created at end") || f.view.treeState.Active != created {
		t.Fatal("creation did not reveal the new node")
	}
	f.data.Collection.SelectNode(moved)
	f.settle()
	f.view.move(moved, target)
	f.settle()
	if !f.has("Moved") || !f.data.Collection.Expanded[target] || f.data.Collection.ParentOf(moved) != target || f.view.treeState.Active != moved {
		t.Fatal("moving the selected node did not reveal its new location")
	}
	if f.view.treeState.RowBuilds >= 40 {
		t.Fatal("revealing a node disabled virtualization")
	}
}

func TestTreeMultiSelectKeepsNameTooltipAndFolderExpansion(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Folder with a very long name that needs a tooltip")
	f.request(parent, "Request", "https://example.test")
	f.reload()
	f.click(locales.ApiCollectionMultiSelect())
	name := f.data.Collection.NodeName(parent)
	rendertest.RequireTooltip(t, f.tester, name, name)
	f.click(name)
	if !f.view.checked[parent] || f.data.Collection.Expanded[parent] || len(f.view.checked) != 1 {
		t.Fatal("folder selection expanded or selected descendants")
	}
}

func TestTreeRefreshClosesRemovedRowMenu(t *testing.T) {
	f := newFixture(t)
	id := f.folder("", "Folder")
	f.reload()
	if err := f.tester.RightClick("Folder"); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if err := f.service.DeleteNodes([]string{id}); err != nil {
		t.Fatal(err)
	}
	f.reload()
	if f.view.menu.State.Open || f.view.menuID != "" {
		t.Fatal("deleted node retained its menu")
	}
}

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
