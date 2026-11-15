package editor

import (
	"testing"
	"time"
)

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
