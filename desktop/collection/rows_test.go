package collection

import (
	"strings"
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/theme"
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
	if row.H != rowHeight {
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

func TestPressOnNameOpensWithoutDragging(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	request := f.request(folder, "Open Me", "http://a/")
	f.reload()
	f.data.Collection.Expanded[folder] = true
	f.settle()
	x, y := f.center("Open Me")
	// A press on the name that moves follows no drag.
	f.tester.Press(x, y)
	f.tester.Frame()
	f.tester.Move(x, y+40)
	f.tester.Frame()
	if f.view.drag.active {
		t.Fatal("a press on the name started a drag")
	}
	f.tester.Release(x, y+40)
	f.settle()
	if f.data.Collection.ParentOf(request) != folder {
		t.Fatal("moved")
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
}

func TestRenderCollectionTree(t *testing.T) {
	f := newFixture(t)
	api := f.folder("", "bilibili")
	f.request(api, "Home page", "http://a/")
	f.request(api, "Archive list with a long name that is cut", "http://b/")
	nested := f.folder(api, "nested")
	f.request(nested, "deep", "http://c/")
	f.folder("", "empty")
	f.reload()
	f.data.Collection.Expanded[api] = true
	f.settle()
	rendertest.ShootThemes(t, f.tester, "collection-tree", f.setDark)
}
