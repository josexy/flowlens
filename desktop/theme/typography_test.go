package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

func TestCustomTextMatchesControlsAcrossSizesAndDPI(t *testing.T) {
	appearance := Appearance{AppFont: "Arial", AppFontSize: 16}
	tester := ui.NewTester(func(c *ui.Context) {
		palette := Begin(c, Resolve(appearance).Config)
		ui.Column(c).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 14)).FontWeight(500).LineHeight(20.0 / 14).Label("custom body")
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 12)).Label("caption")
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 18)).Label("heading")
			nui.Button(c, palette, "Alphabet 123", nui.ButtonOptions{Style: func(_, label, _, _ ui.Element) {
				label.Label("control body")
			}})
		})
	}, 900, 700)
	for _, dpi := range []float32{1, 1.5, 2} {
		tester.SetScale(dpi)
		for _, accessibility := range []float32{1, 1.19} {
			tester.SetPreferences(ui.Preferences{TextScale: accessibility, ReduceMotion: true})
			var small ui.Rect
			for _, size := range []int{12, 16, 24, 16} {
				appearance.AppFontSize = size
				for range 3 {
					tester.Frame()
				}
				body, _ := tester.Find("custom body")
				control, _ := tester.Find("control body")
				caption, _ := tester.Find("caption")
				heading, _ := tester.Find("heading")
				if body.W <= 0 || math.Abs(float64(body.W-control.W)) > 1 || math.Abs(float64(body.H-control.H)) > 1 {
					t.Fatalf("size %v, DPI %v, accessibility %v: custom %+v, control %+v", size, dpi, accessibility, body, control)
				}
				if caption.H >= body.H || heading.H <= body.H {
					t.Fatalf("text hierarchy lost: caption %+v, body %+v, heading %+v", caption, body, heading)
				}
				if size == 12 {
					small = body
				} else if math.Abs(float64(body.W-small.W*float32(size)/12)) > 2 {
					t.Fatalf("text did not scale proportionally: size %v, small %+v, body %+v", size, small, body)
				}
			}
		}
	}
}
