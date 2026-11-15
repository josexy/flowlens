package collection

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

func TestNativeDragRejectsCanceledOutsideAndStaleDrops(t *testing.T) {
	for _, reason := range []string{"escape", "cancel", "outside", "refresh", "disabled", "removed"} {
		t.Run(reason, func(t *testing.T) {
			f := newFixture(t)
			source := f.folder("", "Source")
			f.folder("", "Target")
			id := f.request(source, "Request", "https://example.test")
			f.reload()
			f.data.Collection.Expanded[source] = true
			f.settle()
			x, y := f.grip("Request", 1)
			targetX, targetY := f.center("Target")
			f.tester.Press(x, y)
			f.tester.Move(targetX, targetY)
			f.tester.Frame()
			if !f.view.drag.active || f.data.Collection.ParentOf(id) != source {
				t.Fatal("drag did not start or changed the tree before drop")
			}
			switch reason {
			case "escape":
				f.tester.Key(0, ui.KeyEscape)
			case "cancel":
				f.tester.CancelPress()
			case "outside":
				targetX = 500 // The same Y, outside the tree's actual bounds.
			case "refresh":
				f.view.refresh()
				f.settle()
			case "disabled":
				f.view.toggleMulti()
				f.tester.Frame()
				f.view.toggleMulti()
			case "removed":
				if err := f.service.DeleteNodes([]string{id}); err != nil {
					t.Fatal(err)
				}
				f.reload()
			}
			f.tester.Frame()
			f.tester.Release(targetX, targetY)
			f.settle()
			if f.backend.moveCalls != 0 || f.view.drag.active || f.view.drag.target != "" || f.view.drag.root {
				t.Fatalf("%s left a move or feedback: calls %d, drag %+v", reason, f.backend.moveCalls, f.view.drag)
			}
			if reason != "removed" && f.data.Collection.ParentOf(id) != source {
				t.Fatal("canceled drop changed the parent")
			}
		})
	}
}

func TestNativeDragFailureKeepsSavedParentAndCanRetry(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "Source")
	target := f.folder("", "Target")
	id := f.request(source, "Request", "https://example.test")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	f.backend.moveErr = errors.New("disk full")
	x, y := f.center("Target")
	f.drag("Request", 1, x, y)
	request, err := f.service.GetRequest(id)
	if err != nil || request.ParentID != source || f.data.Collection.ParentOf(id) != source {
		t.Fatal("failed move changed saved data", err)
	}
	if f.backend.moveCalls != 1 || !f.has(locales.ApiCollectionMoveFailed("disk full")) || f.view.drag.active {
		t.Fatal("failed move did not report and clear the drag")
	}
	f.backend.moveErr = nil
	f.drag("Request", 1, x, y)
	if f.backend.moveCalls != 2 || f.data.Collection.ParentOf(id) != target {
		t.Fatal("retry did not save exactly once")
	}
}

func TestNativeDragScrollsAndMovesToHiddenFolder(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "Source")
	id := f.request(source, "Request", "https://example.test")
	for index := range 30 {
		f.folder("", fmt.Sprintf("Folder %02d", index))
	}
	target := f.folder("", "Last target")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	x, y := f.grip("Request", 1)
	f.tester.Press(x, y)
	f.tester.Move(x, 696)
	for range 70 {
		f.tester.Advance(time.Second / 60)
	}
	if first, _ := f.view.treeState.Outline.List.Visible(); first < 5 {
		t.Fatalf("native edge scrolling stopped at row %d", first)
	}
	targetX, targetY := f.center("Last target")
	if targetY < 40 || targetY >= 700 {
		t.Fatalf("target is still outside the viewport: %v", targetY)
	}
	f.tester.Move(targetX, targetY)
	f.tester.Frame()
	f.tester.Release(targetX, targetY)
	f.settle()
	if f.data.Collection.ParentOf(id) != target || f.backend.moveCalls != 1 {
		t.Fatalf("drop after scrolling: parent=%s calls=%d drag=%+v", f.data.Collection.ParentOf(id), f.backend.moveCalls, f.view.drag)
	}
}

func TestNativeFolderDragScrollsBelowRootTarget(t *testing.T) {
	f := newFixture(t)
	parent := f.folder("", "Parent")
	for index := range 35 {
		f.folder(parent, fmt.Sprintf("Child %02d", index))
	}
	f.reload()
	f.data.Collection.Expanded[parent] = true
	f.settle()
	f.view.treeState.Outline.List.ScrollToEnd()
	f.settle()
	x, y := f.grip("Child 34", 1)
	f.tester.Press(x, y)
	f.tester.Move(x, y-8)
	f.tester.Frame()
	before, _ := f.view.treeState.Outline.List.Visible()
	root, ok := f.tester.Find(locales.ApiCollectionMoveToRoot())
	if !ok {
		t.Fatal("missing root target")
	}
	// The native scroll edge belongs to the tree viewport below the header.
	f.tester.Move(x, root.Y+root.H+8)
	for range 15 {
		f.tester.Advance(time.Second / 60)
	}
	if after, _ := f.view.treeState.Outline.List.Visible(); after >= before-1 {
		t.Fatalf("root target blocked native scrolling: row %d -> %d", before, after)
	}
	f.tester.Key(0, ui.KeyEscape)
	f.tester.CancelPress()
	f.settle()
	if f.backend.moveCalls != 0 || f.view.drag.active {
		t.Fatal("canceling edge drag moved a folder")
	}
}

func TestRootTargetLeavesFirstFolderVisibleAndDroppable(t *testing.T) {
	f := newFixture(t)
	first := f.folder("", "First folder")
	source := f.folder("", "Source")
	id := f.folder(source, "Nested folder")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	before, _ := f.tester.Find("First folder")
	startDrag := func() {
		t.Helper()
		x, y := f.grip("Nested folder", 1)
		f.tester.Press(x, y)
		f.tester.Move(x, y+8)
		f.tester.Frame()
		root, rootOK := f.tester.Find(locales.ApiCollectionMoveToRoot())
		row, rowOK := f.tester.Find("First folder")
		if !rootOK || !rowOK || row.Y < root.Y+root.H || row.H != before.H {
			t.Fatalf("root target covers first folder: root %+v, row %+v", root, row)
		}
	}
	startDrag()
	f.tester.Key(0, ui.KeyEscape)
	f.tester.CancelPress()
	f.settle()
	if row, _ := f.tester.Find("First folder"); row.Y != before.Y {
		t.Fatalf("cancel left extra space: before %+v, after %+v", before, row)
	}
	if _, ok := f.tester.Find(locales.ApiCollectionMoveToRoot()); ok {
		t.Fatal("cancel left the root target visible")
	}
	startDrag()
	x, y := f.center("First folder")
	f.tester.Move(x, y)
	f.tester.Frame()
	if f.view.drag.target != first || f.view.drag.root {
		t.Fatal("first folder cannot receive the drop")
	}
	f.tester.Release(x, y)
	f.settle()
	if f.data.Collection.ParentOf(id) != first || f.backend.moveCalls != 1 {
		t.Fatal("drop did not move into the first folder exactly once")
	}
}

func TestRenderNativeCollectionDrag(t *testing.T) {
	if !rendertest.Enabled() {
		return
	}
	f := newFixture(t)
	source := f.folder("", "Saved requests")
	f.folder(source, "Headers")
	f.request(source, "Get users", "https://example.test/users")
	f.folder("", "Authentication")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	rendertest.ShootThemes(t, f.tester, "collection-native-drag", func(dark bool) {
		f.tester.CancelPress()
		f.setDark(dark)
		x, y := f.grip("Headers", 1)
		f.tester.Press(x, y)
		f.tester.Move(x, y+8)
		f.tester.Frame()
		x, y = f.center("Authentication")
		f.tester.Move(x, y)
		f.tester.Frame()
	})
	f.tester.CancelPress()
}
