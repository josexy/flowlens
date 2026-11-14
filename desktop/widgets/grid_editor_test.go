package widgets

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestGridEditorStaysInsideViewport(t *testing.T) {
	for _, editable := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("editable=%t/dark=%t", editable, dark), func(t *testing.T) {
				rows := make([]gridRow, 80)
				fields := make([]KeyValue, len(rows))
				for i := range rows {
					rows[i] = gridRow{enabled: true, key: fmt.Sprintf("Header-%d", i), value: fmt.Sprintf("value-%d", i)}
				}
				const index = 4
				rows[index].value = strings.Repeat("image/avif,image/webp,image/apng; ", 12)
				for i, row := range rows {
					fields[i] = KeyValue{Key: row.key, Value: row.value}
				}
				config := nui.DefaultApp()
				tester := ui.NewTester(func(c *ui.Context) {
					theme := nui.App(c, config)
					ui.Column(c).Fill().Padding(20).Background(theme.Background).Children(func() {
						ui.Box(c).Height(60).Shrink(0)
						ui.Column(c).Height(240).Shrink(0).Children(func() {
							var grid ui.Element
							if editable {
								grid = EditableGrid(c, theme, EditableGridOptions{
									Label: "rows", Rows: len(rows),
									Enabled:    func(i int) *bool { return &rows[i].enabled },
									Key:        func(i int) *string { return &rows[i].key },
									Value:      func(i int) *string { return &rows[i].value },
									ValueLabel: func(i int) string { return fmt.Sprintf("Value %d", i) },
								})
							} else {
								grid = FieldGrid(c, theme, fields, FieldGridOptions{Label: "rows"})
							}
							grid.Label("grid-viewport")
						})
						ui.Text(c, "Outside the table").TextColor(theme.Text)
					})
				}, 700, 500)
				tester.SetDark(dark)
				tester.Frame()
				tester.Frame()
				viewport, _ := tester.Find("grid-viewport")
				before := tester.Image()
				label := rows[index].value
				if editable {
					label = fmt.Sprintf("Value %d", index)
				}
				if err := tester.Click(label); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				editor, ok := tester.Access("cell-editor")
				if !ok || !tester.Focused("cell-editor") || editor.Bounds.H <= 20 {
					t.Fatalf("expected a focused multiline editor: %+v", editor)
				}
				if editor.Bounds.Y+editor.Bounds.H > viewport.Y+viewport.H {
					t.Fatalf("long editor cannot scroll within the viewport: %v", editor.Bounds)
				}
				assertOutsideGridUnchanged(t, before, tester.Image(), viewport)
				// Texts lists the display labels, not the input's Value. The
				// original cell must stop painting under the inset editor.
				for _, text := range tester.Texts() {
					if text == rows[index].value {
						t.Fatal("original cell text is still painted under the editor")
					}
				}
				// The long value can scroll within its input without moving the
				// table, and copying still includes the nonvisible text.
				tester.Scroll(editor.Bounds.X+20, editor.Bounds.Y+10, 0, 60)
				inside, ok := tester.Access("cell-editor")
				if !ok || inside.Bounds != editor.Bounds {
					t.Fatalf("scrolling the textarea moved its cell: %v -> %v", editor.Bounds, inside.Bounds)
				}
				tester.Key(ui.Ctrl, ui.KeyC)
				if tester.Clipboard() != rows[index].value {
					t.Fatal("copy omitted text outside the editor's viewport")
				}
				rendertest.Shoot(t, tester, fmt.Sprintf("grid-editor-editable-%t-dark-%t", editable, dark))
				// Scroll over the other column so the list, not the textarea,
				// consumes it. Position must follow in the same settled event.
				tester.Scroll(viewport.X+12, viewport.Y+100, 0, 60)
				moved, ok := tester.Access("cell-editor")
				if !ok || moved.Bounds.Y != editor.Bounds.Y-60 {
					t.Fatalf("editor did not follow scrolling: before=%v after=%v", editor.Bounds, moved.Bounds)
				}
				assertOutsideGridUnchanged(t, before, tester.Image(), viewport)
				if editable {
					tester.Type("edited value")
				}
				tester.Scroll(viewport.X+12, viewport.Y+100, 0, 240)
				if _, ok := tester.Access("cell-editor"); ok {
					t.Fatal("editor survived after its row left the viewport")
				}
				tester.Scroll(viewport.X+12, viewport.Y+100, 0, -300)
				if _, ok := tester.Access("cell-editor"); ok {
					t.Fatal("scrolling back reopened the old editor")
				}
				if editable && rows[index].value != "edited value" {
					t.Fatal("scrolling lost the edited value")
				}
			})
		}
	}
}

func assertOutsideGridUnchanged(t *testing.T, before, after *image.RGBA, grid ui.Rect) {
	t.Helper()
	for y := range before.Bounds().Dy() {
		for x := range before.Bounds().Dx() {
			if float32(x) >= grid.X && float32(x) < grid.X+grid.W && float32(y) >= grid.Y && float32(y) < grid.Y+grid.H {
				continue
			}
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				t.Fatalf("editor painted outside the grid at (%d, %d), grid=%v", x, y, grid)
			}
		}
	}
}
