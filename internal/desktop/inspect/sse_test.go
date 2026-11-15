package inspect

import (
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/locales"
)

func TestSSEHeaderDividerIsOnePixel(t *testing.T) {
	f := sseFixture(t, "data: hello\n\n")
	f.tester.SetSize(800, 500)
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		palette := f.settings.Theme().Config.Resolve(dark, ui.Preferences{})
		border, background := palette.BorderAccented, palette.Background
		line := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
		blank := color.RGBA{R: background.R, G: background.G, B: background.B, A: background.A}
		for _, scale := range []float32{1, 1.25, 1.5, 2} {
			f.tester.SetScale(scale)
			for _, zoom := range []float32{1, 1.19} {
				f.tester.SetZoom(zoom)
				f.settle()
				header, ok := f.tester.AccessByRole(ui.RoleColumnHeader, locales.DetailSseId())
				if !ok {
					t.Fatal("SSE ID header is missing")
				}
				x := int((header.Bounds.X + header.Bounds.W/2) * scale)
				y := int(math.Round(float64((header.Bounds.Y + header.Bounds.H) * scale)))
				frame := f.tester.Image()
				if got := frame.RGBAAt(x, y); got != line {
					t.Fatalf("dark=%v scale=%v zoom=%v: divider is %v, want %v", dark, scale, zoom, got, line)
				}
				for _, adjacent := range []int{y - 1, y + 1} {
					if got := frame.RGBAAt(x, adjacent); got != blank {
						t.Fatalf("dark=%v scale=%v zoom=%v: pixel next to divider is %v, want %v", dark, scale, zoom, got, blank)
					}
				}
			}
		}
	}
}
