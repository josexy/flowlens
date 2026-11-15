package store

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
)

type pluginFilesBackend struct {
	PluginBackend
	files []string
}

func (b *pluginFilesBackend) ListPluginFiles(string) ([]string, error) { return b.files, nil }
func (b *pluginFilesBackend) ReadPluginFile(_, path string) (*PluginFile, error) {
	return &PluginFile{Path: path, Content: "saved"}, nil
}
func (b *pluginFilesBackend) RenamePluginFile(id, _, _ string) (*Plugin, error) {
	return &Plugin{ID: id}, nil
}
func (b *pluginFilesBackend) DeletePluginFile(id, _ string) (*Plugin, error) {
	return &Plugin{ID: id}, nil
}

func pluginFilesStore(t *testing.T) (*PluginsStore, *runtimetest.Window) {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	s := NewPluginsStore(rt, &pluginFilesBackend{files: []string{"main.py", "extra.py"}}, nil)
	s.SelectedPluginID, s.editorPluginID, s.SelectedFilePath = "first", "first", "extra.py"
	s.selectedFiles["first"] = "extra.py"
	s.EditorContent, s.SavedEditor = "draft", "saved"
	return s, window
}

func TestPluginFileRefreshFinishesSupersededCallbacksAndKeepsDraft(t *testing.T) {
	s, window := pluginFilesStore(t)
	finished := 0
	done := func(err error) {
		if err != nil {
			t.Error(err)
		}
		finished++
	}
	// A mutation's refresh and a registry notification can arrive together.
	s.loadFiles("first", false, done)
	s.loadFiles("first", true, done)
	for range 4 {
		s.rt.Jobs.Wait()
		window.Drain()
	}
	if finished != 2 || s.EditorContent != "draft" || !s.EditorDirty() {
		t.Fatalf("callbacks = %d, content = %q, dirty = %v", finished, s.EditorContent, s.EditorDirty())
	}
}

func TestPluginFileRefreshPreservesEditsMadeDuringRead(t *testing.T) {
	s, window := pluginFilesStore(t)
	s.EditorContent = s.SavedEditor
	finished := false
	s.loadFile("first", "extra.py", true, func(err error) {
		if err != nil {
			t.Error(err)
		}
		finished = true
	})
	s.EditorContent = "typed during refresh"
	s.rt.Jobs.Wait()
	window.Drain()
	if !finished || s.EditorContent != "typed during refresh" || !s.EditorDirty() {
		t.Fatalf("finished = %v, content = %q", finished, s.EditorContent)
	}
}

func TestPluginFileMutationsDoNotChangeAnotherPluginEditor(t *testing.T) {
	for _, operation := range []string{"rename", "delete"} {
		t.Run(operation, func(t *testing.T) {
			s, window := pluginFilesStore(t)
			finished := false
			done := func(_ *Plugin, err error) {
				if err != nil {
					t.Error(err)
				}
				finished = true
			}
			if operation == "rename" {
				s.RenameFile("renamed.py", done)
			} else {
				s.DeleteFile(done)
			}
			// Switch before the completed background operation reaches the UI.
			s.SelectedPluginID, s.editorPluginID, s.SelectedFilePath = "second", "second", "main.py"
			s.EditorContent = "second draft"
			s.rt.Jobs.Wait()
			window.Drain()
			if !finished || s.SelectedFilePath != "main.py" || s.EditorContent != "second draft" {
				t.Fatalf("finished = %v, path = %q, content = %q", finished, s.SelectedFilePath, s.EditorContent)
			}
		})
	}
}

func TestPluginFileDeleteDoesNotRestoreDeletedDraft(t *testing.T) {
	s, window := pluginFilesStore(t)
	s.editorDrafts[pluginDraftKey("first", "main.py")] = pluginDraft{content: "main draft", saved: "saved"}
	s.backend.(*pluginFilesBackend).files = []string{"main.py"}
	finished := false
	s.DeleteFile(func(_ *Plugin, err error) {
		if err != nil {
			t.Error(err)
		}
		finished = true
	})
	for range 4 {
		s.rt.Jobs.Wait()
		window.Drain()
	}
	if !finished || s.SelectedFilePath != "main.py" || s.EditorContent != "main draft" {
		t.Fatalf("finished = %v, path = %q, content = %q", finished, s.SelectedFilePath, s.EditorContent)
	}
	if _, ok := s.editorDrafts[pluginDraftKey("first", "extra.py")]; ok {
		t.Fatal("deleted file draft survived")
	}
}
