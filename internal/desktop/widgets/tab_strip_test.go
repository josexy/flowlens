package widgets

import (
	"image/color"
	"strconv"
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

type wheelProbe struct {
	items   int
	strip   ui.ScrollState
	page    ui.ScrollState
	stripAt ui.Rect
}

func wheelView(p *wheelProbe) func(*ui.Context) {
	return func(c *ui.Context) {
		ui.Scroll(c.Key("page")).TrackScroll(&p.page).Fill().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				strip := ui.ScrollHorizontal(c.Key("strip")).FillWidth().Height(40).Scrollbars(false)
				HorizontalWheel(c, strip, &p.strip).Children(func() {
					ui.Row(c).FillHeight().Children(func() {
						for index := range p.items {
							ui.Box(c.Key(index)).Width(100).FillHeight().Shrink(0).Children(func() { ui.Text(c, "tab "+strconv.Itoa(index)) })
						}
					})
				})
				p.stripAt = strip.Bounds()
				ui.Box(c).FillWidth().Height(2000)
			})
		})
	}
}

func TestVerticalWheelScrollsOverflowingStrip(t *testing.T) {
	p := &wheelProbe{items: 20}
	tester := ui.NewTester(wheelView(p), 400, 300)
	tester.Frame()
	tester.Frame()
	if p.strip.MaxX <= 0 {
		t.Fatalf("strip does not overflow: %+v", p.strip)
	}
	tester.Wheel(200, p.stripAt.Y+20, 0, 120)
	tester.Frame()
	if p.strip.X != 120 || p.page.Y != 0 {
		t.Fatalf("strip x = %v, page y = %v", p.strip.X, p.page.Y)
	}
	tester.Wheel(200, p.stripAt.Y+20, 0, -60)
	tester.Frame()
	if p.strip.X != 60 {
		t.Fatalf("strip x = %v after scrolling back", p.strip.X)
	}
}

func TestCtrlWheelDoesNotScrollStrip(t *testing.T) {
	p := &wheelProbe{items: 20}
	tester := ui.NewTester(wheelView(p), 400, 300)
	tester.Frame()
	tester.Frame()
	tester.HoldModifiers(ui.Ctrl)
	tester.Frame()
	tester.Wheel(200, p.stripAt.Y+20, 0, 120)
	tester.HoldModifiers(0)
	tester.Frame()
	if p.strip.X != 0 {
		t.Fatalf("strip x = %v with Ctrl", p.strip.X)
	}
}

func TestWheelPassesOnWhenStripDoesNotOverflow(t *testing.T) {
	p := &wheelProbe{items: 2}
	tester := ui.NewTester(wheelView(p), 400, 300)
	tester.Frame()
	tester.Frame()
	tester.Scroll(200, p.stripAt.Y+20, 0, 120)
	tester.Frame()
	if p.strip.X != 0 || p.page.Y == 0 {
		t.Fatalf("strip x = %v, page y = %v; want the page to scroll", p.strip.X, p.page.Y)
	}
}

func TestWheelPassesOnAtStripEnd(t *testing.T) {
	p := &wheelProbe{items: 20}
	tester := ui.NewTester(wheelView(p), 400, 300)
	tester.Frame()
	tester.Frame()
	p.strip.X = p.strip.MaxX
	tester.Frame()
	tester.Scroll(200, p.stripAt.Y+20, 0, 120)
	tester.Frame()
	if p.page.Y == 0 {
		t.Fatal("wheel at the strip's end did not reach the page")
	}
}
