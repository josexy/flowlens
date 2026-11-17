package capture

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// menuLabels returns the labels of items, "-" for separators.
func menuLabels(items []nui.MenuItem) []string {
	labels := make([]string, len(items))
	for index, item := range items {
		labels[index] = item.Label
		if item.Kind == nui.MenuSeparator {
			labels[index] = "-"
		}
	}
	return labels
}

// requireMenu checks that the menu on show lists want, and closes it.
func (f *fixture) requireMenu(items []nui.MenuItem, want ...string) {
	f.t.Helper()
	if got := menuLabels(items); !slices.Equal(got, want) {
		f.t.Fatalf("menu %q, want %q", got, want)
	}
	for _, item := range items {
		if item.Kind != nui.MenuAction {
			continue
		}
		if _, ok := menutest.Item(f.tester, item.Label); !ok {
			f.t.Fatalf("no item %q on show in %v", item.Label, f.tester.Texts())
		}
	}
	menutest.Close(f.tester, f.settle)
}

func TestMenusAreDrawnInTheWindowWith105Items(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "a.test"))
	openRowMenu(t, f.fixture, "a.test")
	f.requireMenu(f.workspace.Table.menuItems(), "Show", "Export", "-", "Copy as cURL", "-", "Copy", "-", "Edit", "Resend", "-", "Highlight", "-", "Delete")

	if err := menutest.Open(f.tester, f.settle, "Capture"); err != nil {
		t.Fatal(err)
	}
	f.requireMenu(f.workspace.captureMenuItems(), locales.TrafficExportTitle(), locales.CaptureSetAlias())

	if err := menutest.Open(f.tester, f.settle, "session one"); err != nil {
		t.Fatal(err)
	}
	f.requireMenu(f.workspace.historyMenuItems(f.history.list[0]), locales.TrafficExportTitle(), locales.HistoryDelete())

	if err := menutest.Open(f.tester, f.settle, locales.TrafficMethod()); err != nil {
		t.Fatal(err)
	}
	columns := []string{locales.TrafficColumns()}
	for _, column := range columnDefinitions {
		columns = append(columns, column.title())
	}
	f.requireMenu(f.workspace.Table.menuItems(), append(columns, "-", locales.TrafficShowAllColumns())...)
}

func TestTrafficMenuLastItemIsFullyVisibleWithoutScrolling(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "a.test"))
	openRowMenu(t, f.fixture, "a.test")
	f.tester.Advance(time.Second)
	f.settle()
	menu, ok := f.tester.AccessByRole(ui.RoleMenu, "")
	if !ok {
		t.Fatal("no menu")
	}
	last, ok := menutest.Item(f.tester, locales.ContextMenuTrafficTableDelete())
	if !ok || last.Bounds.H == 0 || last.Bounds.Y+last.Bounds.H > menu.Bounds.Y+menu.Bounds.H {
		t.Fatalf("last item %+v is clipped by menu %+v", last.Bounds, menu.Bounds)
	}
	if err := f.tester.Click(locales.ContextMenuTrafficTableDelete()); err != nil {
		t.Fatalf("last item cannot be clicked without scrolling: %v", err)
	}
}

// openHighlightSubmenu opens the Highlight submenu of the open row menu.
func openHighlightSubmenu(t *testing.T, f *fixture) {
	t.Helper()
	if err := menutest.Choose(f.tester, f.settle, locales.ContextMenuTrafficTableHighlight()); err != nil {
		t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	// Menus pop in, growing to their size.
	f.tester.Advance(time.Second)
	f.settle()
}

func TestHighlightSwatchesUse105Colors(t *testing.T) {
	want := []string{"#ef4444", "#f97316", "#eab308", "#22c55e", "#3b82f6", "#a855f7"}
	var got []string
	for _, option := range highlightColors {
		got = append(got, option.color)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("highlight colors %v, want %v", got, want)
	}
	f := newFixture(t, entry(1, "a.test"))
	openRowMenu(t, f, "a.test")
	openHighlightSubmenu(t, f)
	image := f.tester.Image()
	for _, option := range highlightColors {
		if _, ok := menutest.Item(f.tester, option.label()); !ok {
			t.Fatalf("no %q in %v", option.label(), f.tester.Texts())
		}
		swatch, ok := f.tester.Find("swatch " + option.color)
		if !ok || swatch.W != 12 || swatch.H != 12 {
			t.Fatalf("swatch of %s at %v", option.color, swatch)
		}
		var r, g, b uint8
		if _, err := fmt.Sscanf(option.color, "#%02x%02x%02x", &r, &g, &b); err != nil {
			t.Fatal(err)
		}
		// The swatch is drawn in its own color, not the text's.
		center := image.RGBAAt(int(swatch.X+swatch.W/2), int(swatch.Y+swatch.H/2))
		if !sameRGB(center, color.RGBA{r, g, b, 0xff}) {
			t.Fatalf("the swatch of %s is %v", option.color, center)
		}
	}
	if _, ok := menutest.Item(f.tester, locales.ContextMenuTrafficTableHighlightClear()); !ok {
		t.Fatal("no Clear")
	}
}

func sameRGB(a, b color.RGBA) bool {
	near := func(x, y uint8) bool { return x-y < 3 || y-x < 3 }
	return near(a.R, b.R) && near(a.G, b.G) && near(a.B, b.B)
}

// As 1.0.5's: a right-click on a row not selected selects it alone and
// shows it in the detail panel, one on a selected row keeps the selection,
// and one past the rows opens no menu.
func TestRowMenuTargetsAsIn105(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"), entry(3, "c.test"))
	f.clickRow(0, "a.test")
	f.clickRow(ui.Shift, "b.test")
	openRowMenu(t, f, "b.test")
	if ids := f.workspace.Table.menuTarget.ids; !slices.Equal(ids, []uint64{1, 2}) {
		t.Fatalf("menu of %v", ids)
	}
	openRowMenu(t, f, "c.test")
	f.requireSelection([]uint64{3}, 3, false)
	if ids := f.workspace.Table.menuTarget.ids; !slices.Equal(ids, []uint64{3}) {
		t.Fatalf("menu of %v", ids)
	}
	menutest.Close(f.tester, f.settle)

	table, ok := f.tester.Access(locales.TrafficColumns())
	if !ok {
		t.Fatal("no table")
	}
	f.tester.RightClickAt(table.Bounds.X+table.Bounds.W/2, table.Bounds.Y+table.Bounds.H-20)
	f.settle()
	if menutest.Shown(f.tester) {
		t.Fatal("a right-click past the rows opened a menu")
	}
}

// The Menu key and Shift+F10 open the menu of the selection.
func TestRowMenuOpensFromTheKeyboard(t *testing.T) {
	f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
	f.clickRow(0, "b.test")
	f.tester.Key(ui.Shift, ui.KeyF10)
	f.settle()
	if !menutest.Shown(f.tester) || !slices.Equal(f.workspace.Table.menuTarget.ids, []uint64{2}) {
		t.Fatalf("menu %v of %v", menutest.Shown(f.tester), f.workspace.Table.menuTarget.ids)
	}
	f.choose("Copy", "Row")
	if got := f.tester.Clipboard(); !strings.HasPrefix(got, "2\tGET\tb.test") {
		t.Fatalf("clipboard %q", got)
	}
}

func TestHeaderMenuAfterScrollingKeepsSelection(t *testing.T) {
	for _, source := range []string{"capture", "history"} {
		t.Run(source, func(t *testing.T) {
			f := newHistoryFixture(t, manyEntries(200)...)
			table := f.workspace.Table
			if source == "history" {
				f.history.entries["k1"] = manyEntries(200)
				f.click("session one")
				table = f.workspace.HistoryTable
			}
			table.state.List.ScrollTo(40, ui.Start)
			f.settle()
			f.click("host41.test")
			for _, title := range []string{locales.TrafficHost(), locales.TrafficPath()} {
				header := headerRect(t, f.fixture, title)
				f.tester.RightClickAt(header.X+20, header.Y+header.H/2)
				f.settle()
				if !table.menuTarget.header || !menutest.Shown(f.tester) {
					t.Fatalf("header %q opened target %+v", title, table.menuTarget)
				}
				if _, ok := menutest.Item(f.tester, locales.TrafficShowAllColumns()); !ok {
					t.Fatal("header menu has no column visibility controls")
				}
				if !slices.Equal(table.selectedIDs(), []uint64{41}) || table.source.SelectedEntry().ID != 41 {
					t.Fatalf("header menu changed selection: %v", table.selectedIDs())
				}
				menutest.Close(f.tester, f.settle)
			}
		})
	}
}
