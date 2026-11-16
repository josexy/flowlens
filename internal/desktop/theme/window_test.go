package theme

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestWindowBordersRemainVisibleAcrossThemeChanges(t *testing.T) {
	for _, name := range neutralColors {
		t.Run(name, func(t *testing.T) {
			config := Resolve(Appearance{Mode: ModeAuto, NeutralColor: name}).Config
			var applied nui.Theme
			tester := ui.NewTester(func(c *ui.Context) {
				applied = Begin(c, config)
				ui.Column(c).Fill().Padding(10).Gap(10).Background(applied.Background).Children(func() {
					for _, surface := range []struct {
						name  string
						color ui.Color
					}{
						{"background", applied.Background},
						{"muted", applied.MutedBackground},
						{"elevated", applied.Elevated},
					} {
						ui.Box(c.Key(surface.name)).Size(120, 40).Border(1, applied.Border).Background(surface.color).Label(surface.name)
					}
				})
			}, 160, 180)
			for _, dark := range []bool{false, true, false} {
				tester.SetDark(dark)
				tester.Frame()
				if applied.Dark != dark {
					t.Fatalf("theme did not switch: dark=%v, want %v", applied.Dark, dark)
				}
				if !dark && applied.Border != nui.NewTheme(false, applied.Palettes).Border {
					t.Fatal("light border changed")
				}
				frame := tester.Image()
				border := applied.Border
				want := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
				for _, surface := range []string{"background", "muted", "elevated"} {
					rect, ok := tester.Find(surface)
					if !ok {
						t.Fatalf("missing %s surface", surface)
					}
					x, y := int(rect.X), int(rect.Y+rect.H/2)
					if got := frame.RGBAAt(x, y); got != want {
						t.Errorf("dark=%v %s border is %v, want %v", dark, surface, got, want)
					}
					if frame.RGBAAt(x, y) == frame.RGBAAt(x+2, y) {
						t.Errorf("dark=%v: border disappears into %s surface", dark, surface)
					}
				}
			}
		})
	}
}

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
