package collection

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
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
