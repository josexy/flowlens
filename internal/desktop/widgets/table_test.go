package widgets

import (
	"fmt"
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestTableDividerFollowsNativeHeaderAtDifferentTextSizes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, fontSize := range []float32{14, 21} {
			t.Run(fmt.Sprintf("dark=%v/font=%v", dark, fontSize), func(t *testing.T) {
				config := nui.DefaultApp()
				palette := config.Resolve(dark, ui.Preferences{})
				palette.FontSize, palette.Density = fontSize, 0.8
				var state nui.TableState[string]
				state.SetRows([]string{"row"}, func(row string) string { return row })
				tester := ui.NewTester(func(c *ui.Context) {
					nui.App(c, config)
					Table(c, palette, &state, []nui.TableColumn[string]{{ID: "value", Title: "Value", Value: func(row string) string { return row }}}, nui.TableOptions{Label: "Records"})
				}, 400, 200)
				for _, scale := range []float32{1, 1.25, 1.5, 2} {
					tester.SetScale(scale)
					for _, zoom := range []float32{1, 1.19} {
						tester.SetZoom(zoom)
						header, ok := tester.AccessByRole(ui.RoleColumnHeader, "Value")
						if !ok {
							t.Fatal("missing header")
						}
						x := int((header.Bounds.X + header.Bounds.W/2) * scale)
						y := int(math.Round(float64((header.Bounds.Y + header.Bounds.H) * scale)))
						frame := tester.Image()
						border := palette.BorderAccented
						line := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
						if frame.RGBAAt(x, y) != line || frame.RGBAAt(x, y-1) == line || frame.RGBAAt(x, y+1) == line {
							t.Fatalf("scale=%v zoom=%v: divider is not exactly one physical pixel at y=%d", scale, zoom, y)
						}
					}
				}
			})
		}
	}
}
