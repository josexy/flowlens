package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestWindowTextScaleKeepsControlsAndGeometryTogether(t *testing.T) {
	config := Resolve(Appearance{}).Config
	var applied nui.Theme
	tester := ui.NewTester(func(c *ui.Context) {
		applied = Begin(c, config)
		ui.Column(c).Children(func() {
			ui.Box(c).Size(52, 38).Label("fixed geometry")
			nui.Button(c, applied, "control", nui.ButtonOptions{})
		})
	}, 800, 600)
	tester.SetPreferences(ui.Preferences{TextScale: 1, ReduceMotion: true})
	tester.Frame()
	baseline, _ := tester.Find("control")
	for _, dpi := range []float32{1, 1.5, 2} {
		tester.SetScale(dpi)
		for _, scale := range []float32{1.19, 1.5, 1} {
			tester.SetPreferences(ui.Preferences{TextScale: scale, ReduceMotion: true})
			for range 3 {
				tester.Frame()
			}
			fixed, _ := tester.Find("fixed geometry")
			control, _ := tester.Find("control")
			if math.Abs(float64(fixed.W-52*scale)) > 0.1 || math.Abs(float64(fixed.H-38*scale)) > 0.1 {
				t.Fatalf("DPI %v text scale %v: geometry %+v", dpi, scale, fixed)
			}
			if math.Abs(float64(control.H-baseline.H*scale)) > 1 {
				t.Fatalf("DPI %v text scale %v: control height %v, baseline %v", dpi, scale, control.H, baseline.H)
			}
			if applied.TextScale != 1 || !applied.Motion.Reduce {
				t.Fatalf("preferences were dropped or text scaled twice: %+v", applied)
			}
		}
	}
}
