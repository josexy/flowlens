package plugins

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/keytest"
	"github.com/josexy/flowlens/desktop/store"
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
