package capture

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
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
				want = rgba(theme.Border)
			}
			if got := pixel(f.tester, x, y); !near(got, want) {
				t.Fatalf("dark=%v: header pixel at x=%v is %v, want %v", dark, x, got, want)
			}
		}
	}
}

func TestTrafficRowsAlternateInCaptureAndHistory(t *testing.T) {
	for _, source := range []struct {
		name string
		ids  [2]string
	}{
		{"capture", [2]string{"1", "2"}},
		{"history", [2]string{"11", "12"}},
	} {
		t.Run(source.name, func(t *testing.T) {
			f := newHistoryFixture(t, entry(1, "first.test"), entry(2, "second.test")).fixture
			if source.name == "history" {
				f.click("session one")
				f.requireText("saved.test")
			}
			f.tester.Move(1, 1)
			for _, dark := range []bool{false, true} {
				f.setDark(dark)
				rendertest.SettleTheme(f.tester)
				var colors [2]color.RGBA
				for index, id := range source.ids {
					f.tester.Move(1, 1)
					f.settle()
					row, ok := f.tester.AccessByRole(ui.RoleRow, id)
					if !ok {
						t.Fatalf("no row %s", id)
					}
					firstCell := headerRect(t, f, locales.TrafficId())
					x, y := firstCell.X+firstCell.W-6, row.Bounds.Y+row.Bounds.H/2
					colors[index] = pixel(f.tester, x, y)
					f.tester.Move(x, y)
					f.settle()
					if hovered := pixel(f.tester, x, y); hovered != colors[index] {
						t.Fatalf("dark=%v: hovering row %s changed its background from %v to %v", dark, id, colors[index], hovered)
					}
				}
				base := rgba(f.theme().Background)
				stripe := rgba(trafficStripeColor(f.theme()).Over(f.theme().Background))
				if colors[1] == base {
					colors[0], colors[1] = colors[1], colors[0]
				}
				if colors[0] != base || near(colors[1], base) || !near(colors[1], stripe) {
					t.Fatalf("dark=%v: row colors %v, want %v and %v", dark, colors, base, stripe)
				}
				suffix := "light"
				if dark {
					suffix = "dark"
				}
				rendertest.Shoot(t, f.tester, "traffic-row-hover-"+source.name+"-"+suffix)
			}
		})
	}
}

func TestHoveredResizeEdgeUsesOnlyNativeGrip(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newRenderFixture(t)
		f.setDark(dark)
		rendertest.SettleTheme(f.tester)
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
	frame := f.tester.Image()
	for x := arrow.X; x < arrow.X+arrow.W && !found; x++ {
		for y := arrow.Y; y < arrow.Y+arrow.H; y++ {
			if p := frame.RGBAAt(int(x), int(y)); closer(p, accent, band) && closer(p, accent, text) {
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
	for _, entry := range f.workspace.Table.rows {
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
		want := rgba(theme.Primary.Alpha(0.22).Over(selectionBackground(theme)))
		wantPlain := rgba(theme.Primary.Alpha(0.22).Over(theme.Background))
		wantStripe := rgba(theme.Primary.Alpha(0.22).Over(trafficStripeColor(theme).Over(theme.Background)))
		if near(got, want) || near(got, wantPlain) || near(got, wantStripe) {
			foundOutline = true
		}
	}
	if !foundOutline {
		t.Fatal("no outline along the inspected row's top")
	}
}

func TestClickedRowOutlineSpansAllCells(t *testing.T) {
	request := entry(101, "selected.test")
	request.StatusCode = 200
	f := newFixtureSized(t, []*logic.TrafficEntry{request, entry(102, "other.test")}, 2048, 640)
	f.click("selected.test")
	f.tester.Move(1, 1)
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		theme := f.theme()
		alpha := float32(0.22)
		if dark {
			alpha = 0.35
		}
		background := theme.Primary.Alpha(0.12).Over(theme.Background)
		line := rgba(theme.Primary.Alpha(alpha).Over(background))
		for _, scale := range []float32{1, 1.25, 1.5, 2} {
			f.tester.SetScale(scale)
			for _, zoom := range []float32{1, 1.19, 2} {
				f.tester.SetZoom(zoom)
				f.settle()
				row, ok := f.tester.AccessByRole(ui.RoleRow, "101")
				if !ok || !row.Selected {
					t.Fatalf("clicked row is missing or unselected: %+v", row)
				}
				pixelEdge := func(dip float32) int { return int(math.Round(float64(dip * scale))) }
				left, right := pixelEdge(row.Bounds.X), pixelEdge(row.Bounds.X+row.Bounds.W)
				top, bottom := pixelEdge(row.Bounds.Y), pixelEdge(row.Bounds.Y+row.Bounds.H)
				frame := f.tester.Image()
				// At larger zooms the row scrolls past the window's edge.
				right = min(right, frame.Bounds().Dx())
				idColumn := headerRect(t, f, locales.TrafficId())
				x := pixelEdge(idColumn.X + idColumn.W - 6*zoom)
				y := top + (bottom-top)/2
				if got, want := frame.RGBAAt(x, y), rgba(background); !near(got, want) {
					t.Fatalf("dark=%v scale=%v zoom=%v: selected background = %v, want %v", dark, scale, zoom, got, want)
				}
				// Skip the accent marker at the start of the row.
				for x := left + pixelEdge(4*zoom); x < right; x++ {
					for _, y := range []int{top, bottom - 1} {
						if got := frame.RGBAAt(x, y); !near(got, line) {
							t.Fatalf("dark=%v scale=%v zoom=%v: broken outline at (%d,%d): %v, want %v", dark, scale, zoom, x, y, got, line)
						}
					}
				}
				// Zoom is already part of the device scale: the outline stays
				// a 1 DIP border and the marker a 3 DIP bar, not zoomed again.
				device := scale * zoom
				thickness := 0
				for y := top; y < bottom && near(frame.RGBAAt(x, y), line); y++ {
					thickness++
				}
				if want := max(int(math.Round(float64(device))), 1); thickness != want {
					t.Fatalf("dark=%v scale=%v zoom=%v: outline is %d px thick, want %d", dark, scale, zoom, thickness, want)
				}
				marker := 0
				for x := left; x < right && near(frame.RGBAAt(x, y), rgba(theme.Primary)); x++ {
					marker++
				}
				if lo, hi := int(math.Floor(float64(3*device))), int(math.Ceil(float64(3*device))); marker < lo || marker > hi {
					t.Fatalf("dark=%v scale=%v zoom=%v: marker is %d px wide, want %d-%d", dark, scale, zoom, marker, lo, hi)
				}
			}
		}
		if rendertest.Enabled() {
			f.tester.SetScale(1)
			f.tester.SetZoom(1)
			f.settle()
			name := "traffic-row-outline-light"
			if dark {
				name = "traffic-row-outline-dark"
			}
			rendertest.Shoot(t, f.tester, name)
		}
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

func TestTrafficRowDecorationFollowsDisplayedColumns(t *testing.T) {
	f := newFixtureSized(t, []*logic.TrafficEntry{entry(1, "highlight.test"), entry(2, "plain.test")}, 2400, 640)
	table := f.workspace.Table
	f.data.Traffic.SetHighlight(1, "#ef4444")
	for _, layout := range []string{"default", "hidden-reordered", "fixed-path"} {
		if layout == "hidden-reordered" {
			f.data.Settings.SetTrafficColumnVisible(logic.ColumnID, false, nil)
			table.state.List.Columns.Order = append([]string{"host"}, slices.DeleteFunc(slices.Clone(table.fullOrder), func(key string) bool { return key == "host" })...)
		}
		if layout == "fixed-path" {
			table.state.List.Columns.Widths = map[string]float32{"path": 150}
		}
		for _, dark := range []bool{false, true} {
			f.setDark(dark)
			for _, scale := range []float32{1, 1.5, 2, 4} {
				f.tester.SetScale(scale)
				f.settle()
				row, _ := f.tester.AccessByRole(ui.RoleRow, "1")
				viewport, _ := f.tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
				frame := f.tester.Image()
				at := func(v float32) int { return int(math.Round(float64(v * scale))) }
				tint, alpha := float32(0.15), float32(0.25)
				if dark {
					tint, alpha = 0.35, 0.45
				}
				red := ui.Hex("#ef4444")
				background := red.Alpha(tint).Over(f.theme().Background)
				line := rgba(red.Alpha(alpha).Over(background))
				left, right := at(row.Bounds.X), at(viewport.Bounds.X+viewport.Bounds.W)
				top, bottom := at(row.Bounds.Y), at(row.Bounds.Y+row.Bounds.H)
				for x := left + at(4); x < right; x++ {
					for _, y := range []int{top, bottom - 1} {
						if got := frame.RGBAAt(x, y); !near(got, line) {
							t.Fatalf("%s dark=%v scale=%v: broken border at %d,%d: %v, want %v", layout, dark, scale, x, y, got, line)
						}
					}
				}
				if got := frame.RGBAAt(left+at(1), (top+bottom)/2); !near(got, rgba(red)) {
					t.Fatalf("%s: marker did not follow the first displayed column: %v", layout, got)
				}
				if got := frame.RGBAAt(right-1, (top+bottom)/2); !near(got, line) {
					t.Fatalf("%s: missing last-cell border: %v", layout, got)
				}
			}
		}
	}
}

// newRenderFixture shows the reference traffic at the reference size, with
// its highlights and the inspected row open in the detail panel.
func newRenderFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixtureSized(t, rendertest.Traffic(), rendertest.Width, rendertest.Height)
	for id, color := range rendertest.Highlighted {
		f.data.Traffic.SetHighlight(id, color)
	}
	for _, entry := range f.data.Traffic.Entries {
		if entry.ID == rendertest.Inspected {
			f.data.Traffic.Select(entry)
			f.data.Traffic.SetShowDetailPanel(true)
		}
	}
	f.settle()
	return f
}

// setDark switches the fixture's theme mode.
func (f *fixture) setDark(dark bool) {
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := f.data.Settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
}
