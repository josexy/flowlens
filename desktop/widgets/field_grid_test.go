package widgets

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

type gridProbe struct {
	rows    []KeyValue
	trailed []int
	tester  *ui.Tester
}

func newGridProbe(t *testing.T, rows []KeyValue, width int) *gridProbe {
	t.Helper()
	p := &gridProbe{rows: rows}
	config := nui.DefaultApp()
	p.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(10).Children(func() {
			FieldGrid(c, theme, p.rows, FieldGridOptions{Label: "fields", ValueTrailing: func(row int) {
				if p.trailed != nil {
					nui.Button(c.Key("more"), theme, "more", nui.ButtonOptions{Size: nui.XS})
				}
			}})
			ui.Text(c, "outside")
		})
	}, width, 500)
	p.tester.Frame()
	p.tester.Frame()
	return p
}

func (p *gridProbe) rect(t *testing.T, text string) ui.Rect {
	t.Helper()
	rect, ok := p.tester.Find(text)
	if !ok {
		t.Fatalf("no %q in %v", text, p.tester.Texts())
	}
	return rect
}

func (p *gridProbe) click(t *testing.T, text string) {
	t.Helper()
	rect := p.rect(t, text)
	p.tester.ClickAt(rect.X+4, rect.Y+rect.H/2)
	p.tester.Frame()
	p.tester.Frame()
}

func TestFieldGridKeepsOrderDuplicatesCaseAndEmpty(t *testing.T) {
	rows := []KeyValue{{"Set-Cookie", "a=1"}, {"set-cookie", "b=2"}, {"X-Empty", ""}, {"Set-Cookie", "a=1"}}
	p := newGridProbe(t, rows, 800)
	var keys []string
	for _, text := range p.tester.Texts() {
		switch text {
		case "Set-Cookie", "set-cookie", "X-Empty":
			keys = append(keys, text)
		}
	}
	// Each name is the cell's label and its text.
	want := []string{"Set-Cookie", "Set-Cookie", "set-cookie", "set-cookie", "X-Empty", "X-Empty", "Set-Cookie", "Set-Cookie"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("names %v, want %v", keys, want)
	}
	first, second := p.rect(t, "a=1"), p.rect(t, "b=2")
	if second.Y <= first.Y {
		t.Fatal("rows out of order")
	}
}

func TestFieldGridRowsStayOneLine(t *testing.T) {
	long := strings.Repeat("Mozilla/5.0 (Windows NT 10.0; Win64; x64) ", 8)
	p := newGridProbe(t, []KeyValue{{"accept", "*/*"}, {"user-agent", long}, {"host", "a.test"}}, 700)
	// The first row also carries the grid's top border.
	accept, agent, host := p.rect(t, "accept"), p.rect(t, "user-agent"), p.rect(t, "host")
	if host.Y-agent.Y != FieldGridRowHeight || agent.Y-accept.Y > FieldGridRowHeight || agent.H != host.H {
		t.Fatalf("rows %v, %v, %v; want one-line rows %v apart", accept, agent, host, FieldGridRowHeight)
	}
}

func TestFieldGridClickOpensReadOnlyEditor(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 40)
	p := newGridProbe(t, []KeyValue{{"accept", "*/*"}, {"user-agent", long}, {"host", "a.test"}}, 700)
	before := p.rect(t, "host")
	p.click(t, "user-agent")
	p.click(t, long)
	editor, ok := p.tester.Access("cell-editor")
	if !ok {
		t.Fatal("no cell editor")
	}
	if editor.Bounds.H <= FieldGridRowHeight {
		t.Fatalf("editor %v does not grow past the row", editor.Bounds)
	}
	if !p.tester.Focused("cell-editor") {
		t.Fatal("editor has no focus")
	}
	if after := p.rect(t, "host"); after.Y != before.Y {
		t.Fatalf("rows moved from %v to %v", before.Y, after.Y)
	}
	// The whole value is selected: copying gives it back.
	p.tester.Key(ui.Ctrl, ui.KeyC)
	p.tester.Frame()
	if p.tester.Clipboard() != long {
		t.Fatalf("copied %q", p.tester.Clipboard())
	}
	// Typing changes nothing.
	p.tester.Type("x")
	p.tester.Frame()
	if p.rows[1].Value != long {
		t.Fatal("read-only value changed")
	}
	// Clicking elsewhere closes it.
	p.click(t, "outside")
	if _, ok := p.tester.Access("cell-editor"); ok {
		t.Fatal("editor stays open after losing the focus")
	}
}

func TestFieldGridEditorClosesWhenRowChanges(t *testing.T) {
	p := newGridProbe(t, []KeyValue{{"accept", "*/*"}}, 700)
	p.click(t, "*/*")
	if _, ok := p.tester.Access("cell-editor"); !ok {
		t.Fatal("no editor")
	}
	p.rows = []KeyValue{{"accept", "text/html"}}
	p.tester.Frame()
	p.tester.Frame()
	if _, ok := p.tester.Access("cell-editor"); ok {
		t.Fatal("editor outlived its row's data")
	}
}

func TestFieldGridResizeStaysWithinLimits(t *testing.T) {
	p := newGridProbe(t, []KeyValue{{"accept", "*/*"}, {"host", "a.test"}}, 700)
	handle := p.rect(t, "resize-name-column")
	value := p.rect(t, "*/*")
	drag := func(dx float32) {
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
		handle = p.rect(t, "resize-name-column")
	}
	drag(-100)
	moved := p.rect(t, "*/*")
	if moved.X >= value.X || value.X-moved.X < 90 {
		t.Fatalf("value moved from %v to %v", value.X, moved.X)
	}
	// Both rows follow.
	if a, b := p.rect(t, "*/*"), p.rect(t, "a.test"); a.X != b.X {
		t.Fatalf("rows split at %v and %v", a.X, b.X)
	}
	drag(-1000)
	accept := p.rect(t, "accept")
	if got := p.rect(t, "*/*").X - accept.X; got < FieldGridNameMinWidth-1 {
		t.Fatalf("name column %v narrower than %v", got, FieldGridNameMinWidth)
	}
	drag(2000)
	if right := p.rect(t, "*/*").X; right > 700-10-FieldGridValueMinWidth+fieldGridCellPaddingX+1 {
		t.Fatalf("value column starts at %v, leaving less than %v", right, FieldGridValueMinWidth)
	}
}

func TestFieldGridTrailingDoesNotOpenEditor(t *testing.T) {
	p := newGridProbe(t, []KeyValue{{"set-cookie", "a=1"}}, 700)
	p.trailed = []int{}
	p.tester.Frame()
	p.click(t, "more")
	if _, ok := p.tester.Access("cell-editor"); ok {
		t.Fatal("trailing control opened the editor")
	}
}

func TestFieldGridBuildsOnlyVisibleRows(t *testing.T) {
	rows := make([]KeyValue, 2000)
	for index := range rows {
		rows[index] = KeyValue{"name", "value"}
	}
	p := newGridProbe(t, rows, 700)
	count := 0
	for _, text := range p.tester.Texts() {
		if text == "value" {
			count++
		}
	}
	if count == 0 || count > 40 {
		t.Fatalf("built %d rows of 2000", count)
	}
}
