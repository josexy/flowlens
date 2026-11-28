package collection

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestLinkedSaveKeepsCollectionTreeDuringRefresh(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := newFixture(t)
		folder := f.folder("", "Folder")
		id := f.request(folder, "Linked", "http://example.com/")
		f.reload()
		f.data.Collection.Expanded[folder] = true
		f.settle()
		f.click("Linked")
		f.tester.Move(999, 759)
		f.tester.Advance(time.Second)
		before := f.tester.Image()
		rendertest.Shoot(t, f.tester, "collection-before")
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })
		t.Cleanup(finish)
		f.backend.list = func() (*store.CollectionTree, error) {
			<-release
			if fail {
				return nil, errors.New("refresh failed")
			}
			return f.service.ListCollection()
		}
		tab := f.data.Workspace.ActiveTab()
		tab.HTTP.URL = "http://example.com/changed"
		f.view.SaveTab(tab.Key)
		f.data.Runtime.Jobs.Wait()
		f.window.Drain()
		if !f.data.Collection.Loading {
			t.Fatal("save did not start its tree refresh")
		}
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			if !f.has("Linked") || f.data.Collection.SelectedNodeID != id {
				t.Fatal("refresh hid the saved request or changed selection")
			}
			if row, ok := f.tester.AccessByRole(ui.RoleTreeItem, "Linked"); !ok || !row.Disabled {
				t.Error("refresh left stale tree actions available")
			}
			rendertest.RequireSamePixels(t, before, f.tester.Image(), ui.Rect{X: 0, Y: 40, W: 320, H: 660}, "collection tree")
		}
		rendertest.Shoot(t, f.tester, "collection-refresh")
		finish()
		f.settle()
		if !f.has("Linked") || (fail && !f.has("refresh failed")) {
			t.Fatal("refresh lost the tree or its error")
		}
	}
}

func TestCollectionDialogsKeepFieldsAndCancelStable(t *testing.T) {
	for _, kind := range []string{"create", "rename", "save"} {
		for _, dark := range []bool{false, true} {
			f := newFixture(t)
			folder := f.folder("", "Folder")
			f.reload()
			f.setDark(dark)
			var input string
			var pending func()
			switch kind {
			case "create":
				f.view.beginCreate("")
				f.view.folderName = "New folder"
				input = locales.ApiCollectionFolderNamePlaceholder()
				pending = func() { f.view.createBusy = true }
			case "rename":
				f.view.renaming.show(folder, "Renamed folder")
				input = locales.ApiCollectionRenameNamePlaceholder()
				pending = func() { f.view.renaming.busy = true }
			case "save":
				f.view.save = saveDialog{open: true, name: "Request", parent: folder}
				input = locales.ApiCollectionRequestName()
				pending = func() { f.view.save.saving = true }
			}
			f.tester.Frame()
			f.tester.SetFocused(false)
			f.tester.Advance(time.Second)
			before := f.tester.Image()
			field, ok := f.tester.AccessByRole(ui.RoleTextField, input)
			if !ok {
				t.Fatal("missing dialog input", input)
			}
			cancel, ok := f.tester.AccessByRole(ui.RoleButton, locales.ApiCollectionCancel())
			if !ok {
				t.Fatal("missing cancel button")
			}
			pending()
			for range 3 {
				f.tester.Frame()
				f.tester.Advance(100 * time.Millisecond)
				for _, prior := range []ui.AccessInfo{field, cancel} {
					info, ok := f.tester.AccessByRole(prior.Role, prior.Name)
					if !ok || !info.Disabled || info.Bounds != prior.Bounds {
						t.Errorf("%s dialog moved %s or lost its write guard", kind, prior.Name)
					}
					rendertest.RequireSamePixels(t, before, f.tester.Image(), prior.Bounds, kind+" "+prior.Name)
				}
			}
		}
	}
}

func TestToolbarActionsShowTooltips(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.request(folder, "Req", "http://example.com/")
	f.reload()
	for _, label := range []string{locales.ApiCollectionRefresh(), locales.ApiCollectionNewFolder(), locales.ApiCollectionExpandAll(), locales.ApiCollectionMultiSelect()} {
		rendertest.RequireTooltip(t, f.tester, label, label)
	}
}
