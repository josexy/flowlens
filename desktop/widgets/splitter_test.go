package widgets

import (
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestSplitterDrawsOneDevicePixel(t *testing.T) {
	for _, palette := range []nui.Theme{nui.Light(), nui.Dark()} {
		for _, stacked := range []bool{false, true} {
			split := float32(41)
			tester := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(palette.Native())
				pane := func() { ui.Box(c).Fill().Background(palette.Background) }
				Splitter(c, palette, &split, nui.SplitterOptions{Percent: true, Vertical: stacked}, pane, pane).Fill()
			}, 241, 201)
			for _, scale := range []float32{1, 1.25, 1.5, 2} {
				tester.SetScale(scale)
				for _, zoom := range []float32{1, 1.19} {
					tester.SetZoom(zoom)
					divider, ok := tester.AccessByRole(ui.RoleSplitter, "")
					if !ok {
						t.Fatal("no splitter")
					}
					center := divider.Bounds.X + divider.Bounds.W/2
					if stacked {
						center = divider.Bounds.Y + divider.Bounds.H/2
					}
					frame := tester.Image()
					middle := int(math.Round(float64(center*scale - .5)))
					for offset, want := range []ui.Color{palette.Background, palette.Border, palette.Background} {
						x, y := middle+offset-1, frame.Bounds().Dy()/2
						if stacked {
							x, y = frame.Bounds().Dx()/2, middle+offset-1
						}
						expected := color.RGBA{R: want.R, G: want.G, B: want.B, A: want.A}
						if got := frame.RGBAAt(x, y); got != expected {
							t.Fatalf("dark=%v stacked=%v scale=%v zoom=%v at (%d,%d): got %v, want %v", palette.Dark, stacked, scale, zoom, x, y, got, expected)
						}
					}
				}
			}
		}
	}
}

func TestSplitterResizeByDraggingAndKeyboard(t *testing.T) {
	palette := nui.Light()
	value, changed := float32(40), float32(0)
	tester := ui.NewTester(func(c *ui.Context) {
		Splitter(c, palette, &value, nui.SplitterOptions{
			Percent: true, Min: nui.Set[float32](20), Max: nui.Set[float32](80),
			OnChange: func(next float32) { changed = next },
		}, nil, nil).Fill()
	}, 400, 300)
	divider, ok := tester.AccessByRole(ui.RoleSplitter, "")
	if !ok {
		t.Fatal("no splitter")
	}
	// Start the drag beside the painted pixel, inside the wider hit area.
	x, y := divider.Bounds.X+3, divider.Bounds.Y+divider.Bounds.H/2
	tester.Move(x, y)
	if tester.Cursor() != ui.CursorResizeEW {
		t.Fatalf("cursor %v over the splitter", tester.Cursor())
	}
	tester.Press(x, y)
	tester.Move(x+40, y)
	tester.Release(x+40, y)
	if value <= 40 || changed != value {
		t.Fatalf("drag left value at %v and reported %v", value, changed)
	}
	tester.Key(0, ui.KeyEnd)
	if value != 80 || changed != value {
		t.Fatalf("End left value at %v and reported %v", value, changed)
	}
}
