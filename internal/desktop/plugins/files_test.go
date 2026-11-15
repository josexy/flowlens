package plugins

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestFileActionsProtectRequiredFilesAndKeepRootDirectoryAction(t *testing.T) {
	f := newFixture(t)
	f.create("Files", "")
	if _, ok := f.tester.AccessByRole(ui.RoleButton, locales.PythonPluginsOpenDirectory()); ok {
		t.Fatal("redundant plugin directory action remains")
	}
	f.click(locales.PythonPluginsOpenRootDirectory())
	if !slices.Equal(f.backend.opened, []string{"root"}) {
		t.Fatal(f.backend.opened)
	}
	for _, path := range []string{"main.py", "manifest.json", "helpers.py"} {
		f.click(path)
		for _, label := range []string{locales.PythonPluginsRenameFile(), locales.PythonPluginsDeleteFile()} {
			button, ok := f.tester.AccessByRole(ui.RoleButton, label)
			if !ok || button.Disabled != (path != "helpers.py") {
				t.Fatalf("%s for %s: %+v", label, path, button)
			}
		}
	}
}

func TestFileDialogFailuresRetryAndPreserveDrafts(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Files", "")
	f.click("helpers.py")
	plugins := f.data.Plugins
	original, err := f.backend.ReadPluginFile(plugin.ID, "helpers.py")
	if err != nil {
		t.Fatal(err)
	}
	plugins.EditorContent = "unsaved helper"
	f.settle()
	f.click(locales.PythonPluginsNewFile())
	f.tester.Type("helpers.py")
	f.click(locales.PythonPluginsConfirm())
	if !f.view.fileModal.open || f.view.fileModal.err == "" || f.view.fileModal.busy {
		t.Fatalf("duplicate create: %+v", f.view.fileModal)
	}
	saved, err := f.backend.ReadPluginFile(plugin.ID, "helpers.py")
	if err != nil || saved.Content != original.Content || plugins.EditorContent != "unsaved helper" {
		t.Fatal("duplicate create changed file or draft", err)
	}
	f.click(locales.PythonPluginsFilePath())
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("lib\\extra.py")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.view.fileModal.open || plugins.SelectedFilePath != "lib/extra.py" {
		t.Fatalf("corrected create: %+v, selected %q", f.view.fileModal, plugins.SelectedFilePath)
	}
	f.click("helpers.py")
	if plugins.EditorContent != "unsaved helper" || !plugins.EditorDirty() {
		t.Fatal("creating a file lost the previous draft")
	}
	f.click(locales.PythonPluginsRenameFile())
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("lib/extra.py")
	f.click(locales.PythonPluginsConfirm())
	if !f.view.fileModal.open || f.view.fileModal.err == "" || plugins.EditorContent != "unsaved helper" {
		t.Fatalf("rename collision: %+v", f.view.fileModal)
	}
	f.click(locales.PythonPluginsCancel())
	f.click("lib/extra.py")
	f.backend.deleteFile = func(string, string) (*store.Plugin, error) { return nil, errors.New("file locked") }
	f.click(locales.PythonPluginsDeleteFile())
	f.click(locales.PythonPluginsDelete())
	if !f.view.deleting.file || f.view.deleting.fileBusy || !slices.Contains(plugins.Files, "lib/extra.py") || !f.has(locales.PythonPluginsDeleteFileFailed("file locked")) {
		t.Fatal("failed deletion did not keep a retryable dialog and file")
	}
	f.backend.deleteFile = nil
	f.click(locales.PythonPluginsDelete())
	if f.view.deleting.file || slices.Contains(plugins.Files, "lib/extra.py") {
		t.Fatal("successful deletion did not close and refresh")
	}
}

func TestFileDialogsStayBusyUntilCompletion(t *testing.T) {
	for _, operation := range []string{"create", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := newFixture(t)
			f.create("Files", "")
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var calls atomic.Int32
			confirm := locales.PythonPluginsConfirm()
			if operation == "create" {
				f.backend.createFile = func(id, path string) (*store.Plugin, error) {
					calls.Add(1)
					<-release
					return f.backend.PythonPluginService.CreatePluginFile(id, path)
				}
				f.click(locales.PythonPluginsNewFile())
				f.tester.Type("new.py")
			} else {
				f.backend.deleteFile = func(id, path string) (*store.Plugin, error) {
					calls.Add(1)
					<-release
					return f.backend.PythonPluginService.DeletePluginFile(id, path)
				}
				f.click("helpers.py")
				f.click(locales.PythonPluginsDeleteFile())
				confirm = locales.PythonPluginsDelete()
			}
			if err := f.tester.Click(confirm); err != nil {
				t.Fatal(err)
			}
			f.tester.Frame()
			for _, label := range []string{confirm, locales.PythonPluginsCancel()} {
				button, ok := f.tester.AccessByRole(ui.RoleButton, label)
				if !ok || !button.Disabled {
					t.Fatalf("busy button %q: %+v", label, button)
				}
			}
			f.tester.Key(0, ui.KeyEscape)
			f.tester.Frame()
			if operation == "create" {
				f.view.fileModal.submit(f.view)
				if !f.view.fileModal.open || !f.view.fileModal.busy {
					t.Fatal("busy create dialog was dismissed")
				}
			} else if !f.view.deleting.file || !f.view.deleting.fileBusy {
				t.Fatal("busy delete dialog was dismissed")
			}
			unblock()
			f.settle()
			if calls.Load() != 1 || f.view.fileModal.open || f.view.deleting.file {
				t.Fatalf("calls = %d, file dialog = %+v, deleting = %+v", calls.Load(), f.view.fileModal, f.view.deleting)
			}
		})
	}
}
