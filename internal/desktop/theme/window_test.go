package theme

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestWindowAnimationPreference(t *testing.T) {
	for _, mode := range []string{ModeLight, ModeDark} {
		t.Run(mode, func(t *testing.T) {
			config := Resolve(Appearance{Mode: mode}).Config
			var goal, shown float32
			var applied nui.Theme
			open := false
			tester := ui.NewTester(func(c *ui.Context) {
				applied = Begin(c, config)
				box := ui.Box(c.Key("position")).Size(10, 10)
				shown = box.Animate("x", goal, time.Second)
				box.Translate(shown, 0)
				nui.ModalWithOptions(c.Key("modal"), applied, &open, nui.ModalOptions{Title: "Dialog"}, func(nui.Theme) {
					ui.Text(c, "Dialog content")
				})
			}, 600, 400)
			goal, open = 100, true
			tester.Frame()
			tester.Advance(50 * time.Millisecond)
			if shown <= 0 || shown >= goal {
				t.Fatalf("native animation did not start: %v", shown)
			}
			config.DisableAnimations, open, goal = true, false, 200
			tester.Frame()
			if !applied.Motion.Disabled || shown != goal || tester.HasText("Dialog content") || tester.FramePending() {
				t.Fatal("disabling animations did not finish native motion and dismiss the modal immediately")
			}
			open = true
			tester.Frame()
			if !tester.HasText("Dialog content") || tester.FramePending() {
				t.Fatal("modal did not reopen immediately with animation disabled")
			}
			open = false
			tester.Frame()
			config.DisableAnimations, goal = false, 300
			tester.Frame()
			tester.Advance(50 * time.Millisecond)
			if applied.Motion.Disabled || shown <= 200 || shown >= goal {
				t.Fatalf("native motion did not resume: %v", shown)
			}
			tester.Advance(time.Second)
			if shown != goal || tester.FramePending() {
				t.Fatal("resumed animation did not finish")
			}
			tester.SetPreferences(ui.Preferences{ReduceMotion: true})
			if !applied.Motion.Reduce || applied.Motion.Disabled {
				t.Fatal("system reduced motion must remain distinct from disabling animations")
			}
		})
	}
}

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
