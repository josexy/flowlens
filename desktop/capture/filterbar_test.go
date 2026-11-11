package capture

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// wheelOverFilterTabs turns a vertical wheel over the filter type tabs at
// width and returns how far the Binary tab and the table's first visible row
// moved.
func wheelOverFilterTabs(t *testing.T, width int) (tabMoved float32, firstBefore, firstAfter int) {
	t.Helper()
	f := newFixtureSized(t, manyEntries(300), width, 800)
	table := f.workspace.Table
	table.list.ScrollTo(100, ui.Start)
	f.settle()
	firstBefore, _ = table.list.Visible()
	before, ok := f.tester.Find("Binary")
	if !ok {
		t.Fatal("Binary tab")
	}
	over, ok := f.tester.Find("JSON")
	if !ok {
		t.Fatal("JSON tab")
	}
	f.tester.Wheel(over.X+over.W/2, over.Y+over.H/2, 0, 120)
	f.settle()
	f.tester.Advance(time.Second)
	f.settle()
	firstAfter, _ = table.list.Visible()
	after, _ := f.tester.Find("Binary")
	return before.X - after.X, firstBefore, firstAfter
}

func TestWheelScrollsOverflowingFilterTabs(t *testing.T) {
	moved, before, after := wheelOverFilterTabs(t, 900)
	if moved != 120 {
		t.Fatalf("the tabs moved %v, want 120", moved)
	}
	if before != after {
		t.Fatalf("the table scrolled from row %d to %d", before, after)
	}
}

func TestWheelLeavesFittingFilterTabs(t *testing.T) {
	// The tabs fit: the strip leaves the wheel to the content around it
	// (widgets.TestWheelPassesOnWhenStripDoesNotOverflow), and the filter bar
	// is not inside the table, so neither moves.
	moved, before, after := wheelOverFilterTabs(t, 3000)
	if moved != 0 || before != after {
		t.Fatalf("the tabs moved %v and the table from row %d to %d", moved, before, after)
	}
}
