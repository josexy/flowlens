package widgets

import (
	"image/color"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestRequiresPlainText(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want bool
	}{
		{"short", "short\ntext", false},
		{"below long line", strings.Repeat("a", 20_000), false},
		{"line just under", strings.Repeat("a", LongLineThreshold-1) + "\nshort", false},
		{"line at threshold", strings.Repeat("a", LongLineThreshold), true},
		{"crlf not counted", strings.Repeat("a", LongLineThreshold-1) + "\r\nshort", false},
		{"document just under", strings.Repeat("a\n", LargeTextThreshold/2-1), false},
		{"document at threshold", strings.Repeat("a\n", LargeTextThreshold/2), true},
	} {
		if got := RequiresPlainText(test.text); got != test.want {
			t.Errorf("%s: got %v", test.name, got)
		}
	}
}

func TestChineseTextCountsCharacters(t *testing.T) {
	text := strings.Repeat("中", LongLineThreshold-1)
	if RequiresPlainText(text) {
		t.Fatal("characters, not bytes, set the limit")
	}
	if !RequiresPlainText(text + "中") {
		t.Fatal("at the limit")
	}
}

func codeViewTester(view *CodeView) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		view.Build(c, ui.Font{Size: 13}).Fill().AutoFocus()
	}, 400, 300)
}

func TestCodeViewFrameSurroundsTheNativeEditor(t *testing.T) {
	for _, theme := range []nui.Theme{nui.Light(), nui.Dark()} {
		for _, readOnly := range []bool{false, true} {
			view := NewCodeView("json", readOnly)
			view.SetText(`{"sample":true}`)
			palette := theme.Native()
			tester := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(palette)
				ui.Box(c).Fill().Padding(10).Children(func() {
					view.Build(c, ui.Font{Size: 13})
				})
			}, 400, 300)
			frame := tester.Image()
			border := palette.Border
			want := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
			for _, point := range [][2]int{
				{10, 150}, {389, 150}, {200, 10}, {200, 289},
				{10, 10}, {389, 10}, {10, 289}, {389, 289},
			} {
				if got := frame.RGBAAt(point[0], point[1]); got != want {
					t.Errorf("dark=%v readOnly=%v: border at %v is %v, want %v", theme.Dark, readOnly, point, got, want)
				}
			}
		}
	}
}

func TestLargeReadOnlyTextIsPlain(t *testing.T) {
	view := NewCodeView("json", true)
	view.SetText(`{"a":1}`)
	codeViewTester(view).Frame()
	if view.PlainText() || view.State().Buffer.Language() == nil {
		t.Fatal("small text should be highlighted")
	}
	view.SetText(strings.Repeat("a", LongLineThreshold))
	if !view.PlainText() || view.State().Buffer.Language() != nil || !view.State().NoFolding {
		t.Fatal("large text should be plain")
	}
	view.SetText(`{"a":1}`)
	if view.PlainText() || view.State().Buffer.Language() == nil || view.State().NoFolding {
		t.Fatal("smaller text should be highlighted again")
	}
}

func TestLargeEditableTextStaysHighlighted(t *testing.T) {
	view := NewCodeView("json", false)
	view.SetText(strings.Repeat("a", LongLineThreshold))
	if view.PlainText() || view.State().Buffer.Language() == nil {
		t.Fatal("editable text keeps its language")
	}
}

func TestAppendingCanCrossTheLimit(t *testing.T) {
	view := NewCodeView("json", true)
	view.SetText("a")
	view.AppendText(strings.Repeat("b", LongLineThreshold))
	if !view.PlainText() || !strings.HasPrefix(view.Text(), "ab") {
		t.Fatal("appended text should switch to plain")
	}
}

func TestWordWrapToggles(t *testing.T) {
	view := NewCodeView("", true)
	view.SetWordWrap(true)
	if !view.WordWrap() || !view.State().SoftWrap {
		t.Fatal("wrap on")
	}
	view.SetWordWrap(false)
	if view.WordWrap() {
		t.Fatal("wrap off")
	}
}

func TestEditingReportsChanges(t *testing.T) {
	view := NewCodeView("", false)
	var got string
	calls := 0
	view.OnChange = func(text string) { got = text; calls++ }
	view.SetText("seed")
	tester := codeViewTester(view)
	tester.Frame()
	if calls != 0 {
		t.Fatalf("SetText reported %d changes", calls)
	}
	tester.Type("x")
	tester.Frame()
	if calls == 0 || !strings.Contains(got, "x") {
		t.Fatalf("calls %d, got %q", calls, got)
	}
}

func TestReadOnlyTextCannotBeEdited(t *testing.T) {
	view := NewCodeView("", true)
	view.SetText("seed")
	calls := 0
	view.OnChange = func(string) { calls++ }
	tester := codeViewTester(view)
	tester.Frame()
	tester.Type("x")
	tester.Frame()
	if view.Text() != "seed" || calls != 0 {
		t.Fatalf("text %q, calls %d", view.Text(), calls)
	}
}

func TestLanguageByID(t *testing.T) {
	for _, id := range []string{"json", "xml", "html", "javascript", "css", "python"} {
		if languageByID(id) == nil {
			t.Errorf("no language for %q", id)
		}
	}
	if languageByID("plaintext") != nil {
		t.Error("plain text has no language")
	}
}

func TestScrollingStopsAtTheLastLine(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		view := NewCodeView("text", readOnly)
		lines := make([]string, 100)
		for index := range lines {
			lines[index] = "line"
		}
		view.SetText(strings.Join(lines, "\n"))
		tester := codeViewTester(view)
		tester.Frame()
		for range 40 {
			tester.Scroll(200, 150, 0, 400)
			tester.Frame()
		}
		state := view.State()
		// The last line's bottom, where it shows in the editor.
		bottom := state.LineTop(len(lines)) - state.ScrollY()
		if viewport := state.ViewportHeight(); bottom < viewport-1 || bottom > viewport+1 {
			t.Fatalf("read-only=%v: last line ends at %v in a %v-high view", readOnly, bottom, viewport)
		}
	}
}

func TestReadOnlyViewsDrawNoWhitespace(t *testing.T) {
	if got := NewCodeView("json", true).State().Whitespace; got != editor.WhitespaceNone {
		t.Fatalf("read-only whitespace %v", got)
	}
	view := NewCodeView("json", false)
	if view.State().Whitespace != editor.WhitespaceSelection {
		t.Fatal("editable views show whitespace in selections")
	}
	view.SetReadOnly(true)
	if view.State().Whitespace != editor.WhitespaceNone {
		t.Fatal("a view made read-only keeps whitespace markers")
	}
}
