package editor

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
)

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
