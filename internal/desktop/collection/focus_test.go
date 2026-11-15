package collection

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestNewFolderInputTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("Typed")
	f.settle()
	if f.view.folderName != "Typed" {
		t.Fatalf("folder name = %q", f.view.folderName)
	}
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.has("Typed") || f.view.creating {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestEscapeCancelsFolderCreationDialog(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("Dropped")
	f.settle()
	f.tester.Key(0, ui.KeyEscape)
	f.settle()
	if f.view.creating || !f.data.Collection.Empty() {
		t.Fatalf("creating %v, rows %+v", f.view.creating, f.data.Collection.Rows())
	}
	if f.has(locales.ApiCollectionFolderNamePlaceholder()) {
		t.Fatal("creation dialog still shown")
	}
}

func TestRenameDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.folder("", "Old")
	f.reload()
	f.menu("Old", locales.ApiCollectionRename())
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("New")
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.has("New") || f.has("Old") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestSaveDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.reload()
	f.data.Collection.SelectNode(folder)
	key := f.data.Workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	f.view.SaveTab(key)
	f.settle()
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("Typed request")
	f.settle()
	if f.view.save.name != "Typed request" {
		t.Fatalf("name = %q", f.view.save.name)
	}
}
