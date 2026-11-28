package inspect

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
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

func TestDetailTabStripBorderSpansPane(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.tester.SetSize(1400, 700)
	f.settle()
	strip, ok := f.tester.Find("tab-strip")
	if !ok {
		t.Fatal("no tab strip")
	}
	body, _ := f.tester.Find("Body")
	if body.X+body.W > strip.X+strip.W/2 {
		t.Skipf("tabs %v fill the strip %v", body, strip)
	}
	theme := f.settings.Theme().Config.Light
	image := f.tester.Image()
	y := int(strip.Y + strip.H - 1)
	for _, x := range []int{int(strip.X) + 1, int(body.X+body.W) + 20, int(strip.X+strip.W) - 2} {
		got := image.RGBAAt(x, y)
		if got.R != theme.Border.R || got.G != theme.Border.G || got.B != theme.Border.B {
			t.Fatalf("x=%d on the strip's bottom is %v, want border %v (strip %v)", x, got, theme.Border, strip)
		}
	}
}

func TestWheelScrollsOverflowingDetailTabs(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	f.tester.SetSize(300, 900)
	f.settle()
	before, ok := f.tester.Find("Overview")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.tester.Wheel(before.X+before.W/2, before.Y+before.H/2, 0, 80)
	f.settle()
	// Find clips to the strip, so the tab's visible right edge shows the move.
	after, _ := f.tester.Find("Overview")
	if after.X+after.W >= before.X+before.W {
		t.Fatalf("overview tab at %v, was %v", after, before)
	}
}

func TestWheelOverFittingDetailTabsPassesOn(t *testing.T) {
	f := newFixture(t, httpEntry(), nil)
	before, _ := f.tester.Find("Overview")
	f.tester.Wheel(before.X+before.W/2, before.Y+before.H/2, 0, 80)
	f.settle()
	after, _ := f.tester.Find("Overview")
	if after != before {
		t.Fatalf("fitting strip moved from %v to %v", before.X, after.X)
	}
}

func TestSSEHeaderDividerIsOnePixel(t *testing.T) {
	f := sseFixture(t, "data: hello\n\n")
	f.tester.SetSize(800, 500)
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		palette := f.settings.Theme().Config.Resolve(dark, ui.Preferences{})
		border, background := palette.Border, palette.Background
		line := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
		blank := color.RGBA{R: background.R, G: background.G, B: background.B, A: background.A}
		for _, scale := range []float32{1, 1.25, 1.5, 2} {
			f.tester.SetScale(scale)
			for _, zoom := range []float32{1, 1.19} {
				f.tester.SetZoom(zoom)
				f.settle()
				header, ok := f.tester.AccessByRole(ui.RoleColumnHeader, locales.DetailSseId())
				if !ok {
					t.Fatal("SSE ID header is missing")
				}
				x := int((header.Bounds.X + header.Bounds.W/2) * scale)
				y := int(math.Round(float64((header.Bounds.Y + header.Bounds.H) * scale)))
				frame := f.tester.Image()
				if got := frame.RGBAAt(x, y); got != line {
					t.Fatalf("dark=%v scale=%v zoom=%v: divider is %v, want %v", dark, scale, zoom, got, line)
				}
				for _, adjacent := range []int{y - 1, y + 1} {
					if got := frame.RGBAAt(x, adjacent); got != blank {
						t.Fatalf("dark=%v scale=%v zoom=%v: pixel next to divider is %v, want %v", dark, scale, zoom, got, blank)
					}
				}
			}
		}
	}
}
