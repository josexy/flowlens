package rendertest

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func view(c *ui.Context) {
	ui.Box(c).Fill().Background(ui.RGB(30, 60, 90)).Children(func() { ui.Text(c, "render") })
}

func TestShootIsOffWithoutDirectory(t *testing.T) {
	t.Setenv(EnvDir, "")
	tester := ui.NewTester(view, 200, 100)
	if path := Shoot(t, tester, "off"); path != "" {
		t.Fatalf("path = %q, want none", path)
	}
	if paths := ShootThemes(t, tester, "off", func(bool) { t.Fatal("setDark called while off") }); paths != nil {
		t.Fatalf("paths = %v", paths)
	}
}

func TestShootThemesWritesTwoImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	config := nui.DefaultApp()
	interval := "2s"
	var state nui.SelectState[string]
	state.SetOptions([]nui.Option[string]{{Value: "2s", Label: "2s"}})
	tester := ui.NewTester(func(c *ui.Context) {
		palette := nui.App(c, config)
		nui.Select(c, palette, &interval, &state, nui.SelectOptions{Label: "Interval"}).Width(140)
	}, 200, 100)
	var modes []bool
	paths := ShootThemes(t, tester, "probe/view", func(dark bool) {
		modes = append(modes, dark)
		tester.SetDark(dark)
	})
	if len(paths) != 2 || filepath.Base(paths[0]) != "probe-view-light.png" || filepath.Base(paths[1]) != "probe-view-dark.png" {
		t.Fatalf("paths = %v", paths)
	}
	for index, palette := range []nui.Theme{config.Light, config.Dark} {
		img, err := os.Open(paths[index])
		if err != nil {
			t.Fatal(err)
		}
		frame, err := png.Decode(img)
		_ = img.Close()
		if err != nil {
			t.Fatal(err)
		}
		background := palette.Background
		want := color.RGBA{R: background.R, G: background.G, B: background.B, A: background.A}
		// The middle of the Select falls between its label and chevron.
		if got := color.RGBAModel.Convert(frame.At(70, 16)); got != want {
			t.Errorf("%s: Select background %v, want %v", paths[index], got, want)
		}
	}
	if len(modes) != 3 || modes[0] || !modes[1] || modes[2] {
		t.Fatalf("modes = %v, want light, dark, then back to light", modes)
	}
}

func TestRequireTooltipHovers(t *testing.T) {
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(40).Children(func() {
			button := nui.Button(c, theme, "", nui.ButtonOptions{Square: true, Leading: lucide.Copy(), AccessibleLabel: "Copy it"})
			nui.Tooltip(c, theme, button, "Copy the thing", nui.TooltipOptions{})
		})
	}, 400, 300)
	if tester.HasText("Copy the thing") {
		t.Fatal("tooltip shown before hovering")
	}
	RequireTooltip(t, tester, "Copy it", "Copy the thing")
}

func TestRequireTooltipNeedsATooltip(t *testing.T) {
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(40).Children(func() {
			nui.Button(c, theme, "", nui.ButtonOptions{Square: true, Leading: lucide.Copy(), AccessibleLabel: "Copy it"})
		})
	}, 400, 300)
	probe := &failureProbe{TB: t}
	func() {
		defer func() { _ = recover() }()
		RequireTooltip(probe, tester, "Copy it", "Copy it")
	}()
	if !probe.failed {
		t.Fatal("a control without a tooltip passed")
	}
}

type failureProbe struct {
	testing.TB
	failed bool
}

func (p *failureProbe) Helper() {}
func (p *failureProbe) Fatalf(string, ...any) {
	p.failed = true
	panic("stop")
}
