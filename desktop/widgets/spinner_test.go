package widgets

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/theme"
)

func TestSpinnerFollowsAnimationPreference(t *testing.T) {
	for _, mode := range []string{theme.ModeLight, theme.ModeDark} {
		t.Run(mode, func(t *testing.T) {
			config := theme.Resolve(theme.Appearance{Mode: mode}).Config
			tester := ui.NewTesterWith(func(c *ui.Context) {
				palette := theme.Begin(c, config)
				Spinner(c, palette).Size(24, 24).TextColor(palette.Primary).Label("Loading")
			}, 100, 100, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: true}})
			before := slices.Clone(tester.Image().Pix)
			tester.Advance(110 * time.Millisecond)
			if bytes.Equal(before, tester.Image().Pix) {
				t.Fatal("reduced motion must retain the loading animation")
			}
			bounds, _ := tester.Find("Loading")
			config.DisableAnimations = true
			tester.Frame()
			if _, ok := tester.AccessByRole(ui.RoleProgress, "Loading"); !ok {
				t.Fatal("static loading status is missing from accessibility")
			}
			if staticBounds, _ := tester.Find("Loading"); staticBounds != bounds {
				t.Fatal("disabling motion changed the loading indicator's size")
			}
			before = slices.Clone(tester.Image().Pix)
			tester.Advance(time.Second)
			if !bytes.Equal(before, tester.Image().Pix) || tester.FramePending() {
				t.Fatal("disabled loading indicator still animates")
			}
			config.DisableAnimations = false
			tester.Frame()
			before = slices.Clone(tester.Image().Pix)
			tester.Advance(110 * time.Millisecond)
			if bytes.Equal(before, tester.Image().Pix) {
				t.Fatal("loading animation did not resume")
			}
		})
	}
}
