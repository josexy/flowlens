package capture

import (
	"fmt"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
)

func TestHighlightSwatchesUse105Colors(t *testing.T) {
	want := []string{"#ef4444", "#f97316", "#eab308", "#22c55e", "#3b82f6", "#a855f7"}
	var got []string
	for _, option := range highlightColors {
		got = append(got, option.color)
		if widgets.SwatchMenuIcon(option.color) == nil {
			t.Fatalf("no swatch of %s", option.color)
		}
		center := widgets.SwatchSVG(option.color).Image(64, 64, color.Black).RGBAAt(32, 32)
		var r, g, b uint8
		if _, err := fmt.Sscanf(option.color, "#%02x%02x%02x", &r, &g, &b); err != nil {
			t.Fatal(err)
		}
		if center != (color.RGBA{r, g, b, 0xff}) {
			t.Fatalf("the swatch of %s is %v", option.color, center)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("highlight colors %v, want %v", got, want)
	}
}

func (f *fixture) requireMenu(want ...string) {
	f.t.Helper()
	if got := f.tester.Menu(); !slices.Equal(got, want) {
		f.t.Fatalf("menu %q, want %q", got, want)
	}
	f.tester.CloseMenu()
	f.settle()
}

func TestDecoratedMenusKeepTheirLabels(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "a.test"))
	openRowMenu(t, f.fixture, "a.test")
	f.requireMenu("Show", "Export", "-", "Copy as cURL", "-", "Copy", "-", "Edit", "Resend", "-", "Highlight", "-", "Delete")

	if err := f.tester.RightClick("Capture"); err != nil {
		t.Fatal(err)
	}
	f.settle()
	f.requireMenu(locales.TrafficExportTitle(), locales.CaptureSetAlias())

	if err := f.tester.RightClick("session one"); err != nil {
		t.Fatal(err)
	}
	f.settle()
	f.requireMenu(locales.TrafficExportTitle(), locales.HistoryDelete())

	if err := f.tester.RightClick(locales.TrafficMethod()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	var columns []string
	for _, column := range columnDefinitions {
		columns = append(columns, column.title())
	}
	f.requireMenu(append(columns, "-", locales.TrafficShowAllColumns())...)

	// The Highlight submenu, drawn in the window to open it.
	f.drawnMenus = true
	f.settle()
	openRowMenu(t, f.fixture, "a.test")
	openHighlightSubmenu(t, f.fixture)
	for _, option := range highlightColors {
		f.requireText(option.label())
	}
	f.requireText(locales.ContextMenuTrafficTableHighlightClear())
}

// openHighlightSubmenu rests the pointer on the open row menu's Highlight
// item until its submenu opens.
func openHighlightSubmenu(t *testing.T, f *fixture) {
	t.Helper()
	item, ok := f.tester.Find(locales.ContextMenuTrafficTableHighlight())
	if !ok {
		t.Fatalf("no Highlight item in %v", f.tester.Texts())
	}
	f.tester.Move(item.X+item.W/2, item.Y+item.H/2)
	f.tester.Advance(time.Second)
	f.settle()
}

func TestRenderDecoratedMenus(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "a.test"))
	f.drawnMenus = true
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	f.settle()
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		openRowMenu(t, f.fixture, "a.test")
		openHighlightSubmenu(t, f.fixture)
		rendertest.Shoot(t, f.tester, "menu-highlight"+suffix)
		f.tester.CloseMenu()
		f.settle()
		if err := f.tester.RightClick("session one"); err != nil {
			t.Fatal(err)
		}
		f.settle()
		rendertest.Shoot(t, f.tester, "menu-history"+suffix)
		f.tester.CloseMenu()
		f.settle()
	}
}
