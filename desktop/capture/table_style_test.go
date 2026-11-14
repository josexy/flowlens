package capture

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

func pixel(tester *ui.Tester, x, y float32) color.RGBA {
	return tester.Image().RGBAAt(int(x), int(y))
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 3 && d(a.G, b.G) <= 3 && d(a.B, b.B) <= 3
}

// closer reports whether p is nearer to a than to b.
func closer(p, a, b color.RGBA) bool {
	dist := func(x, y color.RGBA) int {
		dr, dg, db := int(x.R)-int(y.R), int(x.G)-int(y.G), int(x.B)-int(y.B)
		return dr*dr + dg*dg + db*db
	}
	return dist(p, a) < dist(p, b)
}

func rgba(c ui.Color) color.RGBA { return color.RGBA{c.R, c.G, c.B, 255} }

func (f *fixture) theme() nui.Theme {
	config := f.data.Settings.Theme().Config
	if f.data.Settings.Theme().Mode == "dark" {
		return config.Dark
	}
	return config.Light
}

// headerRect returns a header cell's box: the table labels the cell with its
// title.
func headerRect(t *testing.T, f *fixture, title string) ui.Rect {
	t.Helper()
	rect, ok := f.tester.Find(title)
	if !ok {
		t.Fatalf("no header %q", title)
	}
	return rect
}

func TestHeaderShowsNativeDividersAtRest(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newRenderFixture(t)
		f.setDark(dark)
		theme := f.theme()
		host := headerRect(t, f, locales.TrafficHost())
		y := host.Y + host.H/2
		f.tester.Move(1, 1)
		f.settle()
		boundary := host.X + host.W
		for x := boundary - 4; x <= boundary+4; x++ {
			want := rgba(theme.Elevated)
			if x == boundary-1 {
				want = rgba(theme.BorderAccented)
			}
			if got := pixel(f.tester, x, y); !near(got, want) {
				t.Fatalf("dark=%v: header pixel at x=%v is %v, want %v", dark, x, got, want)
			}
		}
	}
}

func TestTrafficTableHeaderDividerIsOnePixel(t *testing.T) {
	for _, name := range []string{"capture", "history"} {
		t.Run(name, func(t *testing.T) {
			f := newHistoryFixture(t, entry(1, "live.test")).fixture
			if name == "history" {
				f.click("session one")
				f.requireText("saved.test")
			}
			f.tester.Move(1, 1)
			for _, dark := range []bool{false, true} {
				f.setDark(dark)
				line := rgba(f.theme().BorderAccented)
				for _, scale := range []float32{1, 1.25, 1.5, 2} {
					f.tester.SetScale(scale)
					for _, zoom := range []float32{1, 1.19} {
						f.tester.SetZoom(zoom)
						host := headerRect(t, f, locales.TrafficHost())
						x := int((host.X + host.W/2) * scale)
						y := int(math.Round(float64((host.Y + host.H) * scale)))
						frame := f.tester.Image()
						if above, divider, below := frame.RGBAAt(x, y-1), frame.RGBAAt(x, y), frame.RGBAAt(x, y+1); !near(divider, line) || near(above, line) || near(below, line) {
							t.Fatalf("dark=%v scale=%v zoom=%v: divider and neighbors are %v, %v, %v, want %v on the middle pixel only", dark, scale, zoom, above, divider, below, line)
						}
					}
				}
			}
		})
	}
}

func TestHoveredResizeEdgeUsesOnlyNativeGrip(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newRenderFixture(t)
		f.setDark(dark)
		theme := f.theme()
		host := headerRect(t, f, locales.TrafficHost())
		edge := host.X + host.W
		y := host.Y + host.H/2
		f.tester.Move(edge-2, y)
		f.settle()
		for x := edge - 4; x < edge-1; x++ {
			if got, want := pixel(f.tester, x, y), rgba(headerBackground(theme, true)); !near(got, want) {
				t.Fatalf("dark=%v: extra resize strip at %v: %v, want header %v", dark, x, got, want)
			}
		}
		if got, want := pixel(f.tester, edge-1, y), rgba(theme.Primary); !near(got, want) {
			t.Fatalf("dark=%v: native grip pixel %v, want %v", dark, got, want)
		}
		if f.tester.Cursor() != ui.CursorResizeEW {
			t.Fatalf("cursor %v over the resize edge", f.tester.Cursor())
		}
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		rendertest.Shoot(t, f.tester, "table-native-resize-"+suffix)
	}
}

func TestSortedHeaderShowsAccentArrow(t *testing.T) {
	f := newRenderFixture(t)
	f.workspace.Table.state.Sort = ui.SortOrder{Column: string(logic.ColumnID), Descending: true}
	f.workspace.Table.lastSort = f.workspace.Table.state.Sort
	f.settle()
	arrow, ok := f.tester.Find("sort-arrow")
	if !ok {
		t.Fatal("no sort arrow")
	}
	id := headerRect(t, f, locales.TrafficId())
	if arrow.X < id.X+cellPaddingX+8 || arrow.X+arrow.W > id.X+id.W || arrow.Y < id.Y || arrow.Y > id.Y+id.H {
		t.Fatalf("arrow at %v beside title %v", arrow, id)
	}
	// The 12 px arrow is antialiased: its stroke is nearer the accent than
	// the band, and nearer the accent than the title text.
	found := false
	accent, band, text := rgba(f.theme().Primary), rgba(f.theme().Elevated), rgba(f.theme().Text)
	for x := arrow.X; x < arrow.X+arrow.W && !found; x++ {
		for y := arrow.Y; y < arrow.Y+arrow.H; y++ {
			if p := pixel(f.tester, x, y); closer(p, accent, band) && closer(p, accent, text) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("sort arrow has no accent pixels")
	}
}

func TestLongMethodsShowInFull(t *testing.T) {
	f := newRenderFixture(t)
	for _, method := range []string{"OPTIONS", "CONNECT", "DELETE"} {
		rect, ok := f.tester.Find(method)
		if !ok {
			t.Fatalf("no %s in %v", method, f.tester.Texts())
		}
		for _, text := range f.tester.Texts() {
			if strings.HasPrefix(text, method[:3]) && strings.HasSuffix(text, "…") {
				t.Fatalf("method cut to %q", text)
			}
		}
		column := headerRect(t, f, locales.TrafficMethod())
		// The pill pads its text by 8 px inside the cell's padding.
		if rect.X+rect.W+8 > column.X+column.W-cellPaddingX+1 {
			t.Fatalf("%s badge text %v runs past the Method column %v", method, rect, column)
		}
	}
}

func TestBadgeStaysOpaqueOnSelectedDarkRow(t *testing.T) {
	f := newRenderFixture(t)
	f.setDark(true)
	sample := func() color.RGBA {
		rect, _ := f.tester.Find("OPTIONS")
		// Inside the pill, left of the text.
		return pixel(f.tester, rect.X-4, rect.Y+rect.H/2)
	}
	f.data.Traffic.SetHighlight(72, "")
	f.settle()
	before := sample()
	for _, entry := range f.workspace.Table.Rows() {
		if entry.ID == 72 {
			f.workspace.Table.state.Selection.Clear()
			f.workspace.Table.state.Selection.Add(trafficRowID(72))
		}
	}
	f.settle()
	after := sample()
	if !near(before, after) {
		t.Fatalf("badge pixel %v on a selected row, %v unselected", after, before)
	}
	if want := rgba(methodTone("OPTIONS").background.Over(f.theme().Background)); !near(after, want) {
		t.Fatalf("badge pixel %v, want %v", after, want)
	}
}

func TestInspectedRowHasMarkerAndOutline(t *testing.T) {
	f := newRenderFixture(t)
	theme := f.theme()
	rect, ok := f.tester.Find("www.bilibili.com")
	if !ok {
		t.Fatal("no inspected row")
	}
	id := headerRect(t, f, locales.TrafficId())
	left := id.X
	y := rect.Y + rect.H/2
	if got := pixel(f.tester, left+1, y); !near(got, rgba(theme.Primary)) {
		t.Fatalf("marker pixel %v, want accent %v", got, rgba(theme.Primary))
	}
	// The outline runs along the row's top, across a middle cell.
	top := rect.Y + rect.H/2 - 16
	foundOutline := false
	for dy := float32(-2); dy <= 2; dy++ {
		got := pixel(f.tester, rect.X+20, top+dy)
		want := rgba(theme.Primary.Alpha(0.22).Over(selectedRowColor(theme)))
		wantPlain := rgba(theme.Primary.Alpha(0.22).Over(theme.Background))
		wantStripe := rgba(theme.Primary.Alpha(0.22).Over(theme.Elevated.Alpha(0.52).Over(theme.Background)))
		if near(got, want) || near(got, wantPlain) || near(got, wantStripe) {
			foundOutline = true
		}
	}
	if !foundOutline {
		t.Fatal("no outline along the inspected row's top")
	}
}

func TestDurationAndSizeUseTabularNumerals(t *testing.T) {
	f := newRenderFixture(t)
	a, _ := f.tester.Find("11 ms")
	b, _ := f.tester.Find("99 ms")
	if a.W == 0 || a.W != b.W {
		t.Fatalf("11 ms is %v wide, 99 ms %v", a.W, b.W)
	}
}

func TestRenderTrafficTable(t *testing.T) {
	f := newRenderFixture(t)
	f.workspace.Table.state.Sort = ui.SortOrder{Column: string(logic.ColumnID), Descending: true}
	f.settle()
	rendertest.ShootThemes(t, f.tester, "traffic-table", f.setDark)
}
