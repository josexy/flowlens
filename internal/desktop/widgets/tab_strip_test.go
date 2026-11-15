package widgets

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func sameColor(got color.RGBA, want ui.Color) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= 3 && d(got.G, want.G) <= 3 && d(got.B, want.B) <= 3
}

func TestTabStripBorderSpansWidth(t *testing.T) {
	config := nui.DefaultApp()
	value := "a"
	var theme nui.Theme
	tester := ui.NewTester(func(c *ui.Context) {
		theme = nui.App(c, config)
		ui.Column(c).Fill().Children(func() {
			TabStrip(c, theme, &value, []nui.TabItem{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}, TabStripOptions{Label: "tabs", PaddingX: 10, Trailing: func() { ui.Text(c, "chip") }})
		})
	}, 600, 200)
	tester.Frame()
	tester.Frame()
	strip, ok := tester.Find("tab-strip")
	if !ok || strip.W != 600 {
		t.Fatalf("strip %v", strip)
	}
	beta, _ := tester.Find("Beta")
	chip, _ := tester.Find("chip")
	if chip.X+chip.W < 580 || chip.X < beta.X+beta.W {
		t.Fatalf("chip at %v is not at the end", chip)
	}
	y := int(strip.Y + strip.H - 1)
	image := tester.Image()
	for _, x := range []int{2, int(beta.X+beta.W) + 40, 300, 597} {
		if got := image.RGBAAt(x, y); !sameColor(got, theme.Border) {
			t.Fatalf("pixel at x=%d on the strip's bottom is %v, want the border %v", x, got, theme.Border)
		}
	}
	alpha, _ := tester.Find("Alpha")
	if got := image.RGBAAt(int(alpha.X+alpha.W/2), y); !sameColor(got, theme.Primary) {
		t.Fatalf("active tab underline pixel %v, want primary %v", got, theme.Primary)
	}
}
