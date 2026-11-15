package plugins

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/locales"
)

func TestCreateDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.click(locales.PythonPluginsCreate())
	f.tester.Type("Typed")
	f.settle()
	if f.view.create.name != "Typed" {
		t.Fatalf("name = %q", f.view.create.name)
	}
}

func TestRenameDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.create("Old", "")
	f.click(locales.PythonPluginsRenamePlugin())
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("New")
	f.settle()
	if f.view.metadata.name != "New" {
		t.Fatalf("name = %q", f.view.metadata.name)
	}
}

func TestFileDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.create("Files", "")
	f.click(locales.PythonPluginsNewFile())
	f.tester.Type("util.py")
	f.settle()
	if f.view.fileModal.path != "util.py" {
		t.Fatalf("path = %q", f.view.fileModal.path)
	}
	f.view.fileModal.open = false
	f.settle()
	f.click("helpers.py")
	f.click(locales.PythonPluginsRenameFile())
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("renamed.py")
	f.settle()
	if f.view.fileModal.path != "renamed.py" {
		t.Fatalf("path = %q", f.view.fileModal.path)
	}
}
