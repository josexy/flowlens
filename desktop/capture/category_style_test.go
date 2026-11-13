package capture

import (
	"slices"
	"strconv"
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
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
	row, ok := f.tester.Find(locales.CategoryTitle() + ": " + label)
	if !ok {
		f.t.Fatalf("no row %q", label)
	}
	theme := f.theme()
	accent, text, background := rgba(theme.Primary), rgba(theme.Text), rgba(theme.Background)
	for x := row.X + row.W - 32; x < row.X+row.W-8; x++ {
		for y := row.Y + 4; y < row.Y+row.H-4; y++ {
			if p := pixel(f.tester, x, y); closer(p, accent, background) && closer(p, accent, text) {
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
	row, _ := f.tester.Find(locales.CategoryTitle() + ": api.test")
	if row.H != 32 {
		t.Fatalf("row %v", row)
	}
}

func TestSectionHeaderCollapsesWithEndChevron(t *testing.T) {
	f := categoryFixture(t)
	header, _ := f.tester.Find(locales.CategoryTitle() + ": " + locales.CategoryHost())
	chevron, ok := f.tester.Find("section-chevron")
	if !ok || chevron.X < header.X+header.W-40 {
		t.Fatalf("chevron %v not at the end of %v", chevron, header)
	}
	if header.H != 32 {
		t.Fatalf("header %v", header)
	}
	count := func() int {
		n := 0
		for _, text := range f.tester.Texts() {
			if text == "api.test" {
				n++
			}
		}
		return n
	}
	// The host is listed under Hosts and Structure and in the table.
	before := count()
	f.click(locales.CategoryHost())
	if count() != before-1 {
		t.Fatalf("%d api.test texts after collapsing, %d before", count(), before)
	}
	f.click(locales.CategoryHost())
	if count() != before {
		t.Fatal("expanding did not bring the hosts back")
	}
}

func TestRenderCategoryTree(t *testing.T) {
	f := newFixtureSized(t, rendertest.Traffic(), rendertest.Width, rendertest.Height)
	f.showCategories = true
	f.settle()
	f.click("www.bilibili.com")
	rendertest.ShootThemes(t, f.tester, "category-tree", f.setDark)
}
