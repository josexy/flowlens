package widgets

import (
	"fmt"
	"image"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type gridRow struct {
	enabled    bool
	key, value string
}

type editProbe struct {
	rows   []gridRow
	tester *ui.Tester
	built  int
	hidden bool
}

func newEditProbe(t *testing.T, rows []gridRow) *editProbe {
	t.Helper()
	p := &editProbe{rows: rows}
	config := nui.DefaultApp()
	p.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(10).Children(func() {
			if !p.hidden {
				EditableGrid(c, theme, EditableGridOptions{
					Label: "rows", Rows: len(p.rows), Resizable: true,
					Enabled:          func(i int) *bool { return &p.rows[i].enabled },
					Key:              func(i int) *string { return &p.rows[i].key },
					Value:            func(i int) *string { return &p.rows[i].value },
					KeyLabel:         func(i int) string { p.built++; return "Key " + strconv.Itoa(i+1) },
					ValueLabel:       func(i int) string { return "Value " + strconv.Itoa(i+1) },
					EnabledLabel:     func(i int) string { return "Enabled " + strconv.Itoa(i+1) },
					KeyPlaceholder:   "Header name",
					ValuePlaceholder: "Header value",
					KeyTrailing: func(i int) {
						if strings.Contains(p.rows[i].key, " ") {
							ui.Text(c, "invalid-mark")
						}
					},
				})
			}
			ui.ButtonBase(c.Key("outside")).Label("outside").Height(24).Children(func() { ui.Text(c, "outside") })
		})
	}, 800, 500)
	p.tester.Frame()
	p.tester.Frame()
	return p
}

func TestEditableGridKeyboardCommitCancelAndTabOrder(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true, key: "Accept", value: "a"}, {key: "accept", value: ""}})
	keytest.TabTo(t, p.tester, "Key 1")
	p.tester.Key(0, ui.KeyEnter)
	p.tester.Frame()
	p.tester.Type("changed")
	p.tester.Key(0, ui.KeyEscape)
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || p.rows[0].key != "Accept" || !p.tester.Focused("Key 1") {
		t.Fatal("Escape did not restore the original cell and focus")
	}
	p.tester.Key(0, ui.KeyTab)
	if !p.tester.Focused("Value 1") {
		t.Fatal("Tab did not reach the value")
	}
	p.tester.Key(0, ui.KeySpace)
	p.tester.Type("new")
	p.tester.Key(ui.Shift, ui.KeyTab)
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || p.rows[0].value != "new" || !p.tester.Focused("Key 1") {
		t.Fatal("Shift+Tab did not commit and focus the preceding control")
	}
	p.tester.Key(0, ui.KeySpace)
	p.tester.Type("Case-Preserved")
	p.tester.Key(0, ui.KeyTab)
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || !p.tester.Focused("Value 1") || p.rows[0].key != "Case-Preserved" || p.rows[1].enabled || p.rows[1].key != "accept" {
		t.Fatal("Tab did not commit or changed other ordered fields")
	}
	p.tester.Key(0, ui.KeyEnter)
	p.tester.Type("final")
	p.tester.Key(0, ui.KeyEnter)
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || !p.tester.Focused("Value 1") || p.rows[0].value != "final" {
		t.Fatal("Enter did not commit and restore focus")
	}
}

func TestEditableGridRemovedRowAndHiddenGridDoNotEditNeighbor(t *testing.T) {
	p := newEditProbe(t, []gridRow{{key: "first", value: "one"}, {key: "next", value: "two"}})
	p.open(t, "Value 1")
	p.tester.Type("draft")
	// The remaining row occupies index zero, but has a different identity.
	p.rows = p.rows[1:]
	p.tester.Frame()
	p.tester.Frame()
	p.tester.Type("unexpected")
	p.tester.Key(0, ui.KeyEscape)
	p.tester.Frame()
	if p.editorOpen() || p.rows[0].value != "two" {
		t.Fatal("removed row's editor wrote into its neighbor")
	}
	p.open(t, "Value 1")
	p.hidden = true
	p.tester.Frame()
	p.tester.Frame()
	p.tester.Type("hidden")
	p.hidden = false
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || p.rows[0].value != "two" {
		t.Fatal("hidden grid retained an active editor")
	}
	keytest.TabTo(t, p.tester, "outside")
}

func TestEditableGridBlurClosesBeforeIdle(t *testing.T) {
	for _, backward := range []bool{false, true} {
		p := newEditProbe(t, []gridRow{{key: "key", value: "value"}})
		p.open(t, "Value 1")
		mods := ui.Modifiers(0)
		if backward {
			mods = ui.Shift
		}
		p.tester.Key(mods, ui.KeyTab)
		// No extra Frame: a real idle window will not render one merely
		// because the view cleared its editing state during this build.
		if p.editorOpen() {
			t.Fatal("blur left the old editor visible until another event")
		}
	}
}

func (p *editProbe) open(t *testing.T, label string) {
	t.Helper()
	if err := p.tester.Click(label); err != nil {
		t.Fatal(err)
	}
	p.tester.Frame()
	p.tester.Frame()
}

func (p *editProbe) editorOpen() bool {
	_, ok := p.tester.Access("cell-editor")
	return ok
}

func TestEditableGridShowsPlaceholdersInEmptyRow(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true}})
	if !p.tester.HasText("Header name") || !p.tester.HasText("Header value") {
		t.Fatalf("%v", p.tester.Texts())
	}
	key, _ := p.tester.Find("Key 1")
	if key.H > EditableGridRowHeight {
		t.Fatalf("row %v", key)
	}
}

func TestEditableGridOpensFocusedWithTextSelected(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true, key: "accept", value: "*/*"}})
	p.open(t, "Value 1")
	if !p.editorOpen() || !p.tester.Focused("cell-editor") {
		t.Fatal("editor not open with the focus")
	}
	// Typing replaces the selected text.
	p.tester.Type("text/html")
	p.tester.Frame()
	if p.rows[0].value != "text/html" {
		t.Fatalf("value %q", p.rows[0].value)
	}
}

func TestEditableGridEnterCommitsWithoutLineBreak(t *testing.T) {
	for _, mods := range []ui.Modifiers{0, ui.Shift} {
		p := newEditProbe(t, []gridRow{{enabled: true, key: "accept", value: "a"}})
		p.open(t, "Value 1")
		p.tester.Key(0, ui.KeyEnd)
		p.tester.Type("bc")
		p.tester.Key(mods, ui.KeyEnter)
		p.tester.Frame()
		p.tester.Frame()
		if p.editorOpen() {
			t.Fatalf("mods %v: editor still open", mods)
		}
		if p.rows[0].value != "abc" {
			t.Fatalf("mods %v: value %q", mods, p.rows[0].value)
		}
	}
}

func TestEditableGridBlurCommits(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true, key: "a b", value: "1"}})
	if !p.tester.HasText("invalid-mark") {
		t.Fatal("invalid mark missing before editing")
	}
	p.open(t, "Key 1")
	if p.tester.HasText("invalid-mark") {
		t.Fatal("invalid mark shown while editing")
	}
	p.tester.Type("ab")
	p.tester.Frame()
	if err := p.tester.Click("outside"); err != nil {
		t.Fatal(err)
	}
	p.tester.Frame()
	p.tester.Frame()
	if p.editorOpen() || p.rows[0].key != "ab" || p.tester.HasText("invalid-mark") {
		t.Fatalf("open %v key %q", p.editorOpen(), p.rows[0].key)
	}
}

func TestEditableGridEditorGrowsForLongText(t *testing.T) {
	long := strings.Repeat("cookie=value; ", 25)
	p := newEditProbe(t, []gridRow{{enabled: true, key: "Cookie", value: long}, {enabled: true, key: "next", value: "1"}})
	next, _ := p.tester.Find("Key 2")
	p.open(t, "Value 1")
	editor, ok := p.tester.Access("cell-editor")
	if !ok || editor.Bounds.H <= EditableGridRowHeight {
		t.Fatalf("editor %v", editor.Bounds)
	}
	if after, _ := p.tester.Find("Key 2"); after.Y != next.Y {
		t.Fatal("rows moved under the editor")
	}
}

func TestEditableGridResizeBounds(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true, key: "accept", value: "*/*"}})
	drag := func(dx float32) ui.Rect {
		handle, _ := p.tester.Find("resize-key-column")
		x, y := handle.X+handle.W/2, handle.Y+handle.H/2
		p.tester.Press(x, y)
		p.tester.Frame()
		for step := float32(1); step <= 5; step++ {
			p.tester.Move(x+dx*step/5, y)
			p.tester.Frame()
		}
		p.tester.Release(x+dx, y)
		p.tester.Frame()
		p.tester.Frame()
		key, _ := p.tester.Find("Key 1")
		return key
	}
	if key := drag(-2000); key.W < EditableGridKeyMinWidth-1 || key.W > EditableGridKeyMinWidth+1 {
		t.Fatalf("key column %v, want %v", key.W, EditableGridKeyMinWidth)
	}
	if key := drag(4000); key.W > 780-2*EditableGridSideColumn-EditableGridValueMinWidth {
		t.Fatalf("key column %v leaves less than %v for values", key.W, EditableGridValueMinWidth)
	}
}

func TestEditableGridBuildsOnlyVisibleRows(t *testing.T) {
	rows := make([]gridRow, 1000)
	p := newEditProbe(t, rows)
	p.built = 0
	p.tester.Frame()
	if p.built == 0 || p.built > 40 {
		t.Fatalf("built %d of 1000 rows", p.built)
	}
}

func TestEditableGridCompositionStaysInEditor(t *testing.T) {
	p := newEditProbe(t, []gridRow{{enabled: true}})
	p.open(t, "Value 1")
	p.tester.Compose("ni", 2)
	p.tester.Frame()
	p.tester.Key(0, ui.KeyEnter)
	p.tester.Frame()
	if !p.editorOpen() {
		t.Fatal("composition closed the editor")
	}
	p.tester.Type("你")
	p.tester.Frame()
	if p.rows[0].value != "你" {
		t.Fatalf("value %q", p.rows[0].value)
	}
}

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
				tester.Key(ui.Cmd, ui.KeyC)
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
