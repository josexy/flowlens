package shell

import (
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
)

func setRenderTheme(t *testing.T, data *store.Store, dark bool) {
	t.Helper()
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := data.Settings.SetThemeMode(mode); err != nil {
		t.Fatal(err)
	}
}

func TestStatusBarDividerUsesOneDevicePixel(t *testing.T) {
	const height = 480
	f := newFixture(t)
	f.tester.SetSize(960, height)
	for _, dark := range []bool{false, true} {
		setRenderTheme(t, f.data, dark)
		palette := f.data.Settings.Theme().Config.Resolve(dark, ui.Preferences{})
		for _, scale := range []float32{1, 1.25, 1.5, 2} {
			f.tester.SetScale(scale)
			for _, zoom := range []float32{1, 1.19} {
				f.tester.SetPreferences(ui.Preferences{TextScale: zoom})
				f.tester.Frame()
				y := int(math.Round(float64((height - statusBarHeight*zoom) * scale)))
				frame := f.tester.Image()
				for offset, want := range []ui.Color{palette.Elevated, palette.Border, palette.Background} {
					pixel := color.RGBA{R: want.R, G: want.G, B: want.B, A: want.A}
					if got := frame.RGBAAt(4, y+offset-1); got != pixel {
						t.Fatalf("dark=%v scale=%v zoom=%v pixel %d: got %v, want %v", dark, scale, zoom, y+offset-1, got, pixel)
					}
				}
			}
		}
	}
}
