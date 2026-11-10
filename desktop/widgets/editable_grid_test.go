package widgets

import (
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

type gridRow struct {
	enabled    bool
	key, value string
}

type editProbe struct {
	rows   []gridRow
	tester *ui.Tester
	built  int
}

func newEditProbe(t *testing.T, rows []gridRow) *editProbe {
	t.Helper()
	p := &editProbe{rows: rows}
	config := nui.DefaultApp()
	p.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(10).Children(func() {
			EditableGrid(c, theme, EditableGridOptions{
				Label: "rows", Rows: len(p.rows), Resizable: true,
				Enabled:          func(i int) *bool { return &p.rows[i].enabled },
				Key:              func(i int) *string { return &p.rows[i].key },
				Value:            func(i int) *string { return &p.rows[i].value },
				KeyLabel:         func(i int) string { p.built++; return "Key " + strconv.Itoa(i+1) },
				ValueLabel:       func(i int) string { return "Value " + strconv.Itoa(i+1) },
				KeyPlaceholder:   "Header name",
				ValuePlaceholder: "Header value",
				KeyTrailing: func(i int) {
					if strings.Contains(p.rows[i].key, " ") {
						ui.Text(c, "invalid-mark")
					}
				},
			})
			ui.Text(c, "outside")
		})
	}, 800, 500)
	p.tester.Frame()
	p.tester.Frame()
	return p
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
	if !p.editorOpen() {
		t.Fatal("composition closed the editor")
	}
	p.tester.Type("你")
	p.tester.Frame()
	if p.rows[0].value != "你" {
		t.Fatalf("value %q", p.rows[0].value)
	}
}
