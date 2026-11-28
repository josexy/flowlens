package editor

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
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

// requireTabInView fails unless the tab titled title shows whole within the
// strip.
func (f *fixture) requireTabInView(title string) {
	f.t.Helper()
	f.tester.Advance(time.Second)
	f.settle()
	rect, ok := f.tester.Find(title)
	if !ok {
		f.t.Fatalf("no tab %q", title)
	}
	// Find clips to the strip: a partly hidden tab is narrower than a tab.
	if rect.W < tabMinWidth-1 {
		f.t.Fatalf("tab %q shows %v, not whole", title, rect)
	}
	// The scroll buttons and new-tab button sit past the strip.
	if rect.X < 0 || rect.X+rect.W > 1400-30 {
		f.t.Fatalf("tab %q at %v is outside the strip", title, rect)
	}
}

func (f *fixture) manyTabs(n int) {
	for index := range n {
		tab := f.newHTTP()
		titled(tab, "tab "+strconv.Itoa(index))
	}
	f.data.Workspace.Activate(f.data.Workspace.Tabs[1].Key)
	f.settle()
	f.tester.Advance(time.Second)
	f.settle()
}

func TestOpeningSavedRequestRevealsItsTab(t *testing.T) {
	f := newFake(t)
	f.manyTabs(20)
	// The first request tab is active, far from the end of the strip.
	if f.editor.tabScroll.MaxX <= 0 || f.editor.tabScroll.X > f.editor.tabScroll.MaxX-400 {
		t.Fatalf("%+v", f.editor.tabScroll)
	}
	_, err := f.data.Workspace.OpenSavedAPI(&apicollectionservice.APICollectionRequest{
		ID: "saved-1", Name: "saved request", Type: apicollectionservice.APICollectionNodeTypeHTTP,
		HTTP: &apicollectionservice.SavedHTTPRequest{Method: "GET", URL: "http://example.com/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.settle()
	f.requireTabInView("saved request")
}

func TestShortcutToHiddenTabRevealsIt(t *testing.T) {
	f := newFake(t)
	f.manyTabs(20)
	// From the first request tab, Ctrl+Shift+Tab twice wraps past Capture to
	// the last tab, far past the visible strip.
	f.tester.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	f.settle()
	f.tester.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	f.settle()
	last := f.data.Workspace.Tabs[len(f.data.Workspace.Tabs)-1]
	if f.data.Workspace.ActiveKey != last.Key {
		t.Fatalf("active %s, want %s", f.data.Workspace.ActiveKey, last.Key)
	}
	f.requireTabInView(DisplayTitle(last))
	f.tester.Key(ui.Ctrl, ui.KeyTab)
	f.settle()
	if f.data.Workspace.ActiveKey != logic.CaptureTabKey {
		t.Fatal(f.data.Workspace.ActiveKey)
	}
	f.requireTabInView(DisplayTitle(f.data.Workspace.Tabs[0]))
}

func TestWheelScrollsOverflowingWorkspaceTabs(t *testing.T) {
	f := newFake(t)
	for range 20 {
		f.newHTTP()
	}
	f.data.Workspace.Activate(f.data.Workspace.Tabs[0].Key)
	f.settle()
	// Let the strip come to rest on the Capture tab.
	f.tester.Advance(3 * time.Second)
	f.settle()
	if f.editor.tabScroll.MaxX <= 0 || f.editor.tabScroll.X != 0 {
		t.Fatalf("tabs do not overflow, or the strip is not at its start: %+v", f.editor.tabScroll)
	}
	f.tester.Wheel(700, tabBarHeight/2, 0, 120)
	f.settle()
	f.tester.Advance(time.Second)
	f.settle()
	if f.editor.tabScroll.X != 120 {
		t.Fatalf("tab strip x = %v, want 120", f.editor.tabScroll.X)
	}
}

func TestWheelOverFewWorkspaceTabsDoesNotScroll(t *testing.T) {
	f := newFake(t)
	f.newHTTP()
	f.tester.Wheel(300, tabBarHeight/2, 0, 120)
	f.settle()
	if f.editor.tabScroll.X != 0 || f.editor.tabScroll.MaxX != 0 {
		t.Fatalf("%+v", f.editor.tabScroll)
	}
}
