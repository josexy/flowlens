package plugins

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestKeyboardPluginAndFileNavigationPreservesDrafts(t *testing.T) {
	f := newFixture(t)
	first := f.create("First", "")
	f.data.Plugins.EditorContent = "first draft"
	f.settle()
	second := f.create("Second", "")
	keytest.TabTo(t, f.tester, locales.PythonPluginsEnabled()+" First")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	plugin, err := f.backend.GetPlugin(first.ID)
	if err != nil || !plugin.Enabled || f.data.Plugins.SelectedPluginID != second.ID {
		t.Fatal("switch did not toggle once independently of navigation", err)
	}
	keytest.TabTo(t, f.tester, "First", ui.Shift)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Plugins.SelectedPluginID != first.ID || f.data.Plugins.EditorContent != "first draft" {
		t.Fatal("plugin keyboard navigation lost the draft")
	}
	f.data.Plugins.CreateFile("extra.py", func(_ *store.Plugin, err error) {
		if err != nil {
			t.Error(err)
		}
	})
	f.settle()
	f.data.Plugins.EditorContent = "extra draft"
	f.settle()
	keytest.TabTo(t, f.tester, "main.py")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.data.Plugins.SelectedFilePath != "main.py" || f.data.Plugins.EditorContent != "first draft" {
		t.Fatal("file keyboard navigation lost the draft")
	}
	keytest.TabTo(t, f.tester, "extra.py", ui.Shift)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Plugins.EditorContent != "extra draft" {
		t.Fatal("second file draft lost")
	}
}

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
	f.tester.Key(ui.Cmd, ui.KeyA)
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
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("renamed.py")
	f.settle()
	if f.view.fileModal.path != "renamed.py" {
		t.Fatalf("path = %q", f.view.fileModal.path)
	}
}
