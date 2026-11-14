package widgets

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

func TestWindowControlHoverReachesTitleBarBottom(t *testing.T) {
	closed := false
	frame := WindowFrame{Custom: true, Title: "FlowLens", Close: func() { closed = true }}
	theme := nui.NewTheme(true, nui.DefaultPalettes())
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			frame.Build(c, theme)
			ui.Box(c).FillWidth().Grow(1).Background(theme.MutedBackground).Label("content")
		})
	}, 360, 80)
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		tester.SetScale(scale)
		for _, zoom := range []float32{1, 1.19} {
			tester.SetZoom(zoom)
			content, ok := tester.Find("content")
			if !ok {
				t.Fatal("content missing")
			}
			y := int(math.Round(float64(content.Y*scale))) - 1
			frame := tester.Image()
			if got := frame.RGBAAt(4, y); !sameColor(got, theme.BorderAccented) {
				t.Fatalf("scale %v, zoom %v: bottom pixel %v, want divider %v", scale, zoom, got, theme.BorderAccented)
			}
			if got := frame.RGBAAt(4, y-1); !sameColor(got, theme.Background) {
				t.Fatalf("scale %v, zoom %v: pixel above divider %v, want background %v", scale, zoom, got, theme.Background)
			}
		}
	}
	const scale = 1.5
	tester.SetScale(scale)
	tester.SetZoom(1)
	button, ok := tester.Find("Close")
	if !ok {
		t.Fatal("close button missing")
	}
	content, ok := tester.Find("content")
	if !ok {
		t.Fatal("content missing")
	}
	x, bottom := button.X+4, content.Y
	px, py := int(x*scale), int(bottom*scale)-1
	tester.Move(x, button.Y+button.H/2)
	if got := tester.Image().RGBAAt(px, py); !sameColor(got, closeHoverColor()) {
		t.Fatalf("close hover bottom pixel is %v, want %v", got, closeHoverColor())
	}
	tester.ClickAt(x, bottom-0.5)
	if !closed {
		t.Fatal("click at the title bar bottom did not close the window")
	}
}
