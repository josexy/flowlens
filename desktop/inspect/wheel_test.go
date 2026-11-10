package inspect

import "testing"

func TestWheelScrollsOverflowingDetailTabs(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	f.tester.SetSize(300, 900)
	f.settle()
	before, ok := f.tester.Find("Overview")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.tester.Wheel(before.X+before.W/2, before.Y+before.H/2, 0, 80)
	f.settle()
	// Find clips to the strip, so the tab's visible right edge shows the move.
	after, _ := f.tester.Find("Overview")
	if after.X+after.W >= before.X+before.W {
		t.Fatalf("overview tab at %v, was %v", after, before)
	}
}

func TestWheelOverFittingDetailTabsPassesOn(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	before, _ := f.tester.Find("Overview")
	f.tester.Wheel(before.X+before.W/2, before.Y+before.H/2, 0, 80)
	f.settle()
	after, _ := f.tester.Find("Overview")
	if after != before {
		t.Fatalf("fitting strip moved from %v to %v", before.X, after.X)
	}
}
