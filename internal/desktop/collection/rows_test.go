package collection

import (
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/theme"
)

func TestFolderShowsChildCountPill(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Three")
	for _, name := range []string{"one", "two", "three"} {
		f.request(folder, name, "http://a/")
	}
	f.reload()
	if !f.has("child-count") || !f.has("3") {
		t.Fatalf("%v", f.tester.Texts())
	}
	row, _ := f.tester.Find("Three")
	pill, _ := f.tester.Find("child-count")
	if pill.H != 20 || pill.X < row.X || pill.X+pill.W > row.X+row.W {
		t.Fatalf("pill %v in row %v", pill, row)
	}
	if row.H < pill.H || row.H > 44 {
		t.Fatalf("row %v", row)
	}
}

func TestTruncatedNameHasTooltip(t *testing.T) {
	f := newFixture(t)
	name := strings.TrimSpace("A very long folder name " + strings.Repeat("that keeps going ", 6))
	f.folder("", name)
	f.reload()
	rendertest.RequireTooltip(t, f.tester, name, name)
}

func TestRowDragDoesNotOpenRequest(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	request := f.request(folder, "Open Me", "http://a/")
	f.reload()
	f.data.Collection.Expanded[folder] = true
	f.settle()
	x, y := f.center("Open Me")
	// A moving press on the row uses native dragging; releasing outside a
	// folder neither moves nor opens the request. A later click still opens it.
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+40)
	f.tester.Frame()
	if !f.view.drag.active {
		t.Fatal("a moving press on the row did not start a native drag")
	}
	f.tester.Release(x, y+40)
	f.settle()
	if f.data.Collection.ParentOf(request) != folder {
		t.Fatal("moved")
	}
	if f.view.drag.active || f.data.Workspace.FindTabByAPIID(request) != nil {
		t.Fatal("a missed drop left a drag active or opened the request")
	}
	f.click("Open Me")
	if tab := f.data.Workspace.FindTabByAPIID(request); tab == nil {
		t.Fatal("clicking the name did not open the request")
	}
}

func TestGripDragDimsSourceAndMoves(t *testing.T) {
	f := newFixture(t)
	source := f.folder("", "A-Source")
	target := f.folder("", "B-Target")
	request := f.request(source, "Moved", "http://a/")
	f.reload()
	f.data.Collection.Expanded[source] = true
	f.settle()
	startX, startY := f.grip("Moved", 1)
	x, y := f.center("B-Target")
	f.tester.Press(startX, startY)
	f.tester.Frame()
	for step := float32(1); step <= 6; step++ {
		f.tester.Move(startX, startY+(y-startY)*step/6)
		f.tester.Frame()
	}
	if !f.view.drag.active || f.view.drag.target != target {
		t.Fatalf("drag %+v", f.view.drag)
	}
	if access, ok := f.tester.Access("Moved"); !ok || access.Bounds.W == 0 {
		t.Fatal("source row missing while dragged")
	}
	f.tester.Release(x, y)
	f.settle()
	if f.data.Collection.ParentOf(request) != target {
		t.Fatal(f.data.Collection.ParentOf(request))
	}
}

func (f *fixture) setDark(dark bool) {
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := f.data.Settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
	f.tester.Advance(300 * time.Millisecond)
}

func TestRenderNarrowCollectionTree(t *testing.T) {
	f := newFixture(t)
	f.sidebarWidth = 240
	parent := f.folder("", "A long collection folder name")
	nested := f.folder(parent, "Nested folder")
	id := f.request(nested, "A long request name in a narrow sidebar", "https://example.test")
	f.reload()
	f.data.Collection.SelectNode(id)
	f.settle()
	for _, label := range []string{"A long collection folder name", "Nested folder", "A long request name in a narrow sidebar"} {
		row, ok := f.tester.Find(label)
		if !ok || row.W <= 0 || row.X+row.W > f.sidebarWidth {
			t.Fatalf("row %q overflows narrow sidebar: %+v", label, row)
		}
	}
	rendertest.ShootThemes(t, f.tester, "collection-tree-narrow", f.setDark)
	f.view.toggleMulti()
	f.settle()
	f.click("Nested folder")
	rendertest.ShootThemes(t, f.tester, "collection-tree-multi-narrow", f.setDark)
}
