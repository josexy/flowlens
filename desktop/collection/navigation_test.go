package collection

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/keytest"
	"github.com/josexy/flowlens/locales"
)

func TestCollectionKeyboardOpeningAndIndependentCheckbox(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	id := f.request(folder, "Request", "https://example.test")
	f.reload()
	keytest.TabTo(t, f.tester, "Folder")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	keytest.TabTo(t, f.tester, "Request")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.data.Workspace.FindTabByAPIID(id) == nil || f.data.Collection.SelectedNodeID != id {
		t.Fatal("keyboard did not open request")
	}
	keytest.TabTo(t, f.tester, "Folder")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.has("Request") {
		t.Fatal("folder did not collapse")
	}
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.view.toggleMulti()
	f.settle()
	before := len(f.data.Workspace.Tabs)
	keytest.TabTo(t, f.tester, locales.ApiCollectionSelectItem("Request"))
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !f.view.checked[id] || len(f.view.checked) != 1 || len(f.data.Workspace.Tabs) != before || !f.data.Collection.Exists(id) {
		t.Fatal("checkbox also activated or deleted the request")
	}
}
