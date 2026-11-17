package inspect

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	desktoptheme "github.com/josexy/flowlens/internal/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestHexDragAcrossRows(t *testing.T) {
	if err := desktoptheme.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	for _, column := range []logic.HexColumn{logic.HexColumnHex, logic.HexColumnText} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("column=%d/reverse=%v", column, reverse), func(t *testing.T) {
				h := NewHexView(Dependencies{})
				var char float32
				tester := ui.NewTester(func(c *ui.Context) {
					palette := nui.App(c, desktoptheme.Resolve(desktoptheme.Appearance{}).Config)
					char = charWidth(c, palette)
					h.Build(c, palette, 1, strings.Repeat("A", 512), false)
				}, 525, 300)
				for range 4 {
					tester.Frame()
				}
				layout := logic.NewHexLayout(h.width, char, h.source.Len(), hexGutter)
				x := 8 + layout.HexStart + 3.5*char
				if column == logic.HexColumnText {
					x = 8 + layout.TextStart + 1.5*char
				}
				startRow, targets := 0, []int{1, 3, 7, 5}
				if reverse {
					startRow, targets = 7, []int{6, 4, 0, 2}
				}
				anchor := int64(startRow*layout.BytesPerRow + 1)
				tester.Press(x, float32(startRow)*hexRowHeight+10)
				tester.Frame()
				check := func(row int) {
					t.Helper()
					focus := int64(row*layout.BytesPerRow + 1)
					start, end, ok := h.SelectionRange()
					if !ok || start != min(anchor, focus) || end != max(anchor, focus)+1 {
						t.Fatalf("row %d: selection [%d,%d), want [%d,%d)", row, start, end, min(anchor, focus), max(anchor, focus)+1)
					}
				}
				for _, row := range targets {
					tester.Move(x, float32(row)*hexRowHeight+10)
					tester.Frame()
					check(row)
				}
				// The final move can arrive with release, without an intervening frame.
				tester.Release(x, 3*hexRowHeight+10)
				tester.Frame()
				check(3)
				tester.Move(x, 10*hexRowHeight+10)
				tester.Frame()
				check(3)
				// Shift extends the byte anchor after a completed drag.
				tester.ClickAtWith(ui.Shift, x, 8*hexRowHeight+10)
				check(8)
				rendertest.Shoot(t, tester, fmt.Sprintf("hex-selection-column-%d-reverse-%v", column, reverse))
				// Leaving the view must clamp to the body, and cancellation
				// must end the capture so a later press can start again.
				tester.Press(x, 2*hexRowHeight+10)
				tester.Move(x, -hexRowHeight)
				if start, _, ok := h.SelectionRange(); !ok || start != 0 {
					t.Fatalf("drag above body starts at %d", start)
				}
				tester.CancelPress()
				tester.Frame()
				tester.Press(x, 4*hexRowHeight+10)
				tester.Release(x, 6*hexRowHeight+10)
				anchor = int64(4*layout.BytesPerRow + 1)
				check(6)
			})
		}
	}
}
