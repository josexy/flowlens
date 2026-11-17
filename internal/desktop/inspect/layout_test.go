package inspect

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestHexColumnsFitAfterResize(t *testing.T) {
	if err := theme.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	viewer := NewBodyViewer(Dependencies{}, false)
	viewer.active = logic.BodyTabHex
	tester := ui.NewTester(func(c *ui.Context) {
		palette := nui.App(c, theme.Resolve(theme.Appearance{}).Config)
		viewer.Build(c, palette, BodyContent{Identity: 1, Body: strings.Repeat("A", 160), ContentType: "text/plain"})
	}, 525, 300)
	for _, width := range []int{525, 320, 900, 525} {
		tester.SetSize(width, 700)
		tester.Frame()
		for frame := 0; tester.FramePending() && frame < 8; frame++ {
			tester.Frame()
		}
		found := false
		for _, text := range tester.Texts() {
			if text == "" || strings.Trim(text, "A") != "" {
				continue
			}
			found = true
			bounds, _ := tester.Find(text)
			if bounds.X < 0 || bounds.X+bounds.W > float32(width) {
				t.Fatalf("width %v (measured %v): text column %q at %+v is outside viewport", width, viewer.hex.width, text, bounds)
			}
		}
		if !found {
			t.Fatalf("width %v: no text column in %v", width, tester.Texts())
		}
	}
}

func TestBodyTabsKeepWholeLabelsInNarrowPane(t *testing.T) {
	f := bodyFixture(t, "application/json", `{"ok":true}`, false)
	f.tester.SetSize(525, 700)
	f.settle()
	label, ok := f.tester.Find(locales.DetailFormatted())
	if !ok {
		t.Fatal("no formatted tab")
	}
	// A label must have room for its text, not merely be present in the tree.
	var textWidth float32
	measure := ui.NewTester(func(c *ui.Context) {
		palette := nui.App(c, f.settings.Theme().Config)
		textWidth, _ = c.MeasureText(0, ui.Span{Text: locales.DetailFormatted(), Font: palette.Font, Size: 12, Weight: 500})
	}, 525, 100)
	measure.Frame()
	if label.W < textWidth {
		t.Fatalf("Formatted width %v cannot fit %v", label.W, textWidth)
	}
	if err := f.tester.Click(locales.DetailFormatted()); err != nil {
		t.Fatal(err)
	}
	// Let the window become idle, without forcing extra frames that can hide
	// a delayed handoff between the tab strip and the body renderer.
	for frame := 0; f.tester.FramePending() && frame < 8; frame++ {
		f.tester.Frame()
	}
	if f.panel.responseBody.Active() != logic.BodyTabFormatted {
		t.Fatal("formatted tab did not activate")
	}
}

func TestBodyEditorFollowsCodeFontPreviewAndDiscard(t *testing.T) {
	f := bodyFixture(t, "text/plain", "sample", false)
	f.settings.BeginEdit()
	f.settings.Draft.CommonConfig.AppFontSize = 24
	f.settings.PreviewDraftAppearance()
	f.settle()
	if size := f.panel.responseBody.raw.State().Font.Size; size != 13 {
		t.Fatalf("interface size changed the independent code font: %v", size)
	}
	f.settings.Draft.CommonConfig.CodeFontSize = 24
	f.settings.PreviewDraftAppearance()
	f.settle()
	if size := f.panel.responseBody.raw.State().Font.Size; size != 24 {
		t.Fatalf("previewed code size = %v", size)
	}
	f.settings.EndEdit()
	f.settle()
	if size := f.panel.responseBody.raw.State().Font.Size; size != 13 {
		t.Fatalf("discarded code size = %v", size)
	}
}
