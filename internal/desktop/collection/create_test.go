package collection

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

func TestCreateChildDialogAutofocusAndFailureRetry(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	f.reload()
	f.menu("Parent", locales.ApiCollectionNewChildFolder())
	f.tester.Advance(300 * time.Millisecond)
	if dialog, ok := f.tester.AccessByRole(ui.RoleDialog, locales.ApiCollectionNewChildFolder()); !ok || dialog.Bounds.W != 420 {
		t.Fatalf("child creation dialog = %+v, found %v", dialog, ok)
	}
	f.tester.Type("  Child  ")
	f.backend.createErr = errors.New("disk full")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.view.creating || f.view.folderName != "  Child  " || f.view.folderParent != parent || f.view.createBusy {
		t.Fatal("failure lost the draft or left creation busy")
	}
	if !f.has(locales.ApiCollectionCreateFolderFailed("disk full")) {
		t.Fatal("missing creation error")
	}
	f.backend.createErr = nil
	f.click(locales.ApiCollectionCreate())
	selected := f.data.Collection.SelectedNodeID
	if f.view.creating || f.data.Collection.ParentOf(selected) != parent || f.data.Collection.NodeName(selected) != "Child" || !f.has("Child") {
		t.Fatal("retry did not create and reveal the trimmed name in its parent")
	}
}

func TestCreateDialogBlocksDuplicateSubmissionAndClosingWhileBusy(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("Once")
	f.tester.Key(0, ui.KeyEnter)
	// Leave the worker result queued on the main dispatcher while exercising
	// the pending dialog. No backend timing or sleeps are involved.
	f.tester.Frame()
	for _, label := range []string{locales.ApiCollectionCreate(), locales.ApiCollectionCancel(), locales.ApiCollectionFolderNamePlaceholder()} {
		if control, ok := f.tester.Access(label); !ok || !control.Disabled {
			t.Fatalf("%s is not disabled while submitting: %+v", label, control)
		}
	}
	f.tester.Key(0, ui.KeyEnter)
	f.tester.Key(0, ui.KeyEscape)
	f.view.cancelCreate()
	if !f.view.creating || !f.view.createBusy {
		t.Fatal("pending creation was dismissed")
	}
	f.settle()
	if f.backend.createCalls != 1 || f.view.creating || !f.has("Once") {
		t.Fatalf("create calls=%d, dialog=%v", f.backend.createCalls, f.view.creating)
	}
}

func TestCreateDialogCancelClearsDraft(t *testing.T) {
	for _, closeWith := range []string{"cancel", "close", "escape"} {
		t.Run(closeWith, func(t *testing.T) {
			f := newFixture(t)
			parent := f.folder("", "Parent")
			f.reload()
			f.menu("Parent", locales.ApiCollectionNewChildFolder())
			f.tester.Type("Discard")
			switch closeWith {
			case "cancel":
				f.click(locales.ApiCollectionCancel())
			case "close":
				f.click("Close " + locales.ApiCollectionNewChildFolder())
			case "escape":
				f.tester.Key(0, ui.KeyEscape)
				f.settle()
			}
			if f.view.creating || f.view.folderName != "" || f.view.folderParent != "" || f.backend.createCalls != 0 {
				t.Fatal("cancel retained a draft or saved a folder")
			}
			f.click(locales.ApiCollectionNewFolder())
			f.tester.Type("Root")
			f.tester.Key(0, ui.KeyEnter)
			f.settle()
			if id := f.data.Collection.SelectedNodeID; id == parent || f.data.Collection.ParentOf(id) != "" {
				t.Fatal("reopening retained the canceled destination")
			}
		})
	}
}

func TestCreateDialogMissingParentDoesNotCreateAtRoot(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	f.reload()
	f.menu("Parent", locales.ApiCollectionNewChildFolder())
	f.tester.Type("Child")
	if err := f.service.DeleteNodes([]string{parent}); err != nil {
		t.Fatal(err)
	}
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	tree, err := f.service.ListCollection()
	if err != nil || len(tree.Folders) != 0 || !f.view.creating || f.view.folderName != "Child" || f.view.folderParent != parent {
		t.Fatal("missing parent changed the destination or discarded the draft", err)
	}
}

func TestCreateDialogRejectsWhitespace(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("   ")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.view.creating || f.backend.createCalls != 0 || !f.has(locales.ApiCollectionFolderNameRequired()) {
		t.Fatal("blank name reached the backend or did not explain the validation")
	}
}

func TestRenderCreateFolderDialogs(t *testing.T) {
	f := newFixture(t)
	f.click(locales.ApiCollectionNewFolder())
	f.tester.Type("New collection")
	f.tester.Advance(300 * time.Millisecond)
	rendertest.ShootThemes(t, f.tester, "collection-create-root", f.setDark)
	f.click(locales.ApiCollectionCancel())
	f.folder("", "Saved requests")
	f.reload()
	f.menu("Saved requests", locales.ApiCollectionNewChildFolder())
	f.tester.Type("Authentication")
	f.tester.Advance(300 * time.Millisecond)
	rendertest.ShootThemes(t, f.tester, "collection-create-child", f.setDark)
}
