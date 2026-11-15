package rendertest

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// RequireTooltip hovers the control labeled label until its tooltip opens
// and fails unless want is shown once more than before hovering. Counting
// matters because a control's accessible label is among the tester's texts
// too, and a tooltip usually repeats it.
func RequireTooltip(t testing.TB, tester *ui.Tester, label, want string) {
	t.Helper()
	tester.Frame()
	before := countText(tester, want)
	rect, ok := tester.Find(label)
	if !ok {
		t.Fatalf("no control %q in %v", label, tester.Texts())
	}
	tester.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tester.Advance(1500 * time.Millisecond)
	tester.Frame()
	if after := countText(tester, want); after <= before {
		t.Fatalf("hovering %q shows no tooltip %q (%d before, %d after) in %v", label, want, before, after, tester.Texts())
	}
	tester.Move(1, 1)
	tester.Advance(time.Second)
}

func countText(tester *ui.Tester, want string) int {
	count := 0
	for _, text := range tester.Texts() {
		if text == want {
			count++
		}
	}
	return count
}
