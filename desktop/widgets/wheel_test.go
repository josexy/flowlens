package widgets

import (
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
)

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
