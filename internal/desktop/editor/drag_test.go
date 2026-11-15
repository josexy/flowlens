package editor

import (
	"slices"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

// dragTab drags the tab titled title by dx in steps, holding at the end for
// hold frames before letting go.
func (f *fixture) dragTab(title string, dx float32, hold int) {
	f.t.Helper()
	x, y := f.center(title)
	f.tester.Press(x, y)
	f.tester.Frame()
	const steps = 12
	for step := 1; step <= steps; step++ {
		f.tester.Move(x+dx*float32(step)/steps, y)
		f.tester.Frame()
	}
	for range hold {
		f.tester.Advance(16 * time.Millisecond)
		f.tester.Frame()
	}
	f.tester.Release(x+dx, y)
	f.settle()
}

func TestDraggedTabFollowsPointerAndNeighborsSlide(t *testing.T) {
	f := newFake(t)
	a := titled(f.newHTTP(), "A-tab")
	titled(f.newHTTP(), "B-tab")
	f.tester.Advance(time.Second)
	f.settle()
	ax, ay := f.center("A-tab")
	bx, _ := f.center("B-tab")
	f.tester.Press(ax, ay)
	f.tester.Frame()
	for step := float32(1); step <= 6; step++ {
		f.tester.Move(ax+20*step, ay)
		f.tester.Frame()
	}
	movedA, _ := f.center("A-tab")
	movedB, _ := f.center("B-tab")
	if movedA < ax+100 {
		t.Fatalf("dragged tab at %v, pointer at %v", movedA, ax+120)
	}
	if movedB >= bx {
		t.Fatalf("neighbor at %v did not slide from %v", movedB, bx)
	}
	f.tester.Release(ax+120, ay)
	f.settle()
	if keys := f.tabKeys(); keys[2] != a.Key {
		t.Fatalf("%v", keys)
	}
}

func TestDragPastHiddenTabsScrollsTheStrip(t *testing.T) {
	f := newFake(t)
	f.manyTabs(20)
	f.data.Workspace.Activate(f.data.Workspace.Tabs[1].Key)
	f.settle()
	f.tester.Advance(2 * time.Second)
	f.settle()
	first := f.data.Workspace.Tabs[1]
	before := f.editor.tabScroll.X
	// Drag to the strip's right edge and hold there while it scrolls.
	x, _ := f.center("tab 0")
	f.dragTab("tab 0", 1330-x, 90)
	if f.editor.tabScroll.X <= before+100 {
		t.Fatalf("strip scrolled from %v to %v", before, f.editor.tabScroll.X)
	}
	at := slices.IndexFunc(f.data.Workspace.Tabs, func(tab *logic.WorkspaceTab) bool { return tab.Key == first.Key })
	// Without scrolling, the tab could pass at most the ten tabs in view.
	if at <= 11 {
		t.Fatalf("tab dropped at %d, not past hidden tabs", at)
	}
	if f.data.Workspace.ActiveKey != first.Key {
		t.Fatal("dragged active tab is no longer active")
	}
}

func TestCaptureStaysFirst(t *testing.T) {
	f := newFake(t)
	a := titled(f.newHTTP(), "A-tab")
	titled(f.newHTTP(), "B-tab")
	f.settle()
	// Dragging Capture moves nothing.
	f.dragTab("Capture", 300, 0)
	if keys := f.tabKeys(); keys[0] != logic.CaptureTabKey {
		t.Fatalf("%v", keys)
	}
	// Dragging a tab far before Capture stops after it.
	f.dragTab("B-tab", -900, 0)
	if keys := f.tabKeys(); keys[0] != logic.CaptureTabKey || keys[2] != a.Key {
		t.Fatalf("%v", keys)
	}
}

func TestPressWithoutMovingActivates(t *testing.T) {
	f := newFake(t)
	a := titled(f.newHTTP(), "A-tab")
	b := titled(f.newHTTP(), "B-tab")
	f.settle()
	if f.data.Workspace.ActiveKey != b.Key {
		t.Fatal(f.data.Workspace.ActiveKey)
	}
	f.click("A-tab")
	if f.data.Workspace.ActiveKey != a.Key || !slices.Equal(f.tabKeys(), []string{logic.CaptureTabKey, a.Key, b.Key}) {
		t.Fatalf("active %s, tabs %v", f.data.Workspace.ActiveKey, f.tabKeys())
	}
}
