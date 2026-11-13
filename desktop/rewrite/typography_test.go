package rewrite

import "testing"

func TestLargeFontDragFollowsVisibleRowPitch(t *testing.T) {
	f := newFixture(t)
	f.data.Settings.BeginEdit()
	f.data.Settings.Draft.CommonConfig.AppFontSize = 24
	f.data.Settings.PreviewDraftAppearance()
	f.settle()
	a, ok := f.tester.Find("a")
	if !ok {
		t.Fatal("first rule missing")
	}
	b, ok := f.tester.Find("b")
	if !ok || b.Y-a.Y <= ruleRowHeight {
		t.Fatalf("large font rows did not grow: %+v, %+v", a, b)
	}
	pitch := b.Y - a.Y
	// Less than half the enlarged row stays in place. Using the old fixed
	// pitch would incorrectly reorder this drag.
	dragGrip(f, "a", pitch*0.4)
	f.settle()
	if f.ids() != "a,b" {
		t.Fatalf("short drag crossed the wrong row: %s", f.ids())
	}
	dragGrip(f, "a", pitch*1.2)
	f.await("reordered large rows", func() bool { return f.ids() == "b,a" })
}
