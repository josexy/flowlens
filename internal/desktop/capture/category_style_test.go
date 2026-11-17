package capture

import (
	"slices"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/locales"
)

func TestSelectingTwoHostsChecksBoth(t *testing.T) {
	f := categoryFixture(t)
	f.click("api.test")
	f.click("cdn.test")
	for _, host := range []string{"api.test", "cdn.test"} {
		if !f.checkMark(host) {
			t.Fatalf("%s has no check mark", host)
		}
	}
	if got := f.data.Filter.SelectedHosts; !slices.Contains(got, "api.test") || !slices.Contains(got, "cdn.test") {
		t.Fatalf("%v", got)
	}
	if n := len(f.data.Filter.Filtered()); n != 2 {
		t.Fatalf("%d rows for either host", n)
	}
	f.click("cdn.test")
	if f.checkMark("cdn.test") {
		t.Fatal("unchosen host still checked")
	}
	if n := len(f.data.Filter.Filtered()); n != 1 {
		t.Fatalf("%d rows", n)
	}
}

// checkMark reports whether the category row of label shows an accent check
// mark at its end.
func (f *fixture) checkMark(label string) bool {
	f.t.Helper()
	item, ok := f.tester.AccessByRole(ui.RoleListItem, label)
	if !ok {
		f.t.Fatalf("no row %q", label)
	}
	row := item.Bounds
	theme := f.theme()
	accent, text, background := rgba(theme.Primary), rgba(theme.Text), rgba(theme.Background)
	frame := f.tester.Image()
	for x := row.X + row.W - 32; x < row.X+row.W-8; x++ {
		for y := row.Y + 4; y < row.Y+row.H-4; y++ {
			if p := frame.RGBAAt(int(x), int(y)); closer(p, accent, background) && closer(p, accent, text) {
				return true
			}
		}
	}
	return false
}

func TestCategoryRowsShowNoCounts(t *testing.T) {
	f := categoryFixture(t)
	// Each host has one entry; 1.0.5 showed no "1" beside it. The sidebar
	// is built first, so a count there would be the first "1".
	if one, ok := f.tester.Find(strconv.Itoa(1)); ok && one.X < 240 {
		t.Fatalf("a count is shown at %v", one)
	}
	item, _ := f.tester.AccessByRole(ui.RoleListItem, "api.test")
	row := item.Bounds
	if row.H != 32 {
		t.Fatalf("row %v", row)
	}
}

func TestCategoryAccordionCollapsesAndRestoresItems(t *testing.T) {
	f := categoryFixture(t)
	f.click(locales.CategoryHost())
	if _, ok := f.tester.Find(locales.CategoryTitle() + ": " + locales.CategoryHost()); ok {
		t.Fatal("collapsed host list is still mounted")
	}
	f.click(locales.CategoryHost())
	if _, ok := f.tester.Find(locales.CategoryTitle() + ": " + locales.CategoryHost()); !ok {
		t.Fatal("expanding did not bring the hosts back")
	}
}
