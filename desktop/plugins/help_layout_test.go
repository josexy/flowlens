package plugins

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func helpLayoutTester(t *testing.T, locale, scope, mode string, width, height, fontSize int) (*Help, *ui.Tester) {
	t.Helper()
	if err := apptheme.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	workbench := store.NewWorkbenchStore()
	workbench.OpenPythonHelp(scope)
	notifier := &widgets.Notifier{}
	help := NewHelp(workbench, notifier, func() string { return locale })
	config := apptheme.Resolve(apptheme.Appearance{Mode: mode, AppFontSize: fontSize}).Config
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		help.Build(c, theme)
		notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{})
	}, width, height)
	tester.SetLocale(locale)
	tester.Advance(time.Second)
	return help, tester
}

func helpBounds(t *testing.T, tester *ui.Tester, text string) ui.Rect {
	t.Helper()
	bounds, ok := tester.Find(text)
	if !ok {
		t.Fatalf("missing %q in %v", text, tester.Texts())
	}
	return bounds
}

func TestHelpHeaderAndStepsAlign(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		for _, width := range []int{340, 680} {
			for _, fontSize := range []int{16, 24} {
				t.Run(fmt.Sprintf("%s/%d/%d", locale, width, fontSize), func(t *testing.T) {
					_, tester := helpLayoutTester(t, locale, pythonapi.ScopeGlobal, apptheme.ModeLight, width, 1200, fontSize)
					messages := pythonapi.MessagesFor(locale)
					tabs, ok := tester.AccessByRole(ui.RoleTabList, messages.Title)
					if !ok {
						t.Fatal("missing help tabs")
					}
					hint := helpBounds(t, tester, messages.Start.AIHint)
					if math.Abs(float64(tabs.Bounds.X-hint.X)) > 1 || math.Abs(float64(tabs.Bounds.W-hint.W)) > 1 {
						t.Fatalf("tabs %v do not align with body %v", tabs.Bounds, hint)
					}
					copyButton := helpBounds(t, tester, messages.CopyContext)
					if copyButton.X < hint.X || copyButton.X+copyButton.W > hint.X+hint.W+1 {
						t.Fatalf("copy button %v overflows content %v", copyButton, hint)
					}
					number := helpBounds(t, tester, "1")
					title := helpBounds(t, tester, messages.Start.Runtime.Title)
					if math.Abs(float64(number.Y-title.Y)) > 1 || number.X+number.W >= title.X {
						t.Fatalf("step number %v does not align with title %v", number, title)
					}
				})
			}
		}
	}
}

func TestHelpTemplateCopiesAndHeaderStaysVisibleWhenScrolling(t *testing.T) {
	for _, scope := range []string{pythonapi.ScopeGlobal, pythonapi.ScopeInline} {
		t.Run(scope, func(t *testing.T) {
			help, tester := helpLayoutTester(t, "zh-Hans", scope, apptheme.ModeLight, 420, 1100, 16)
			messages := pythonapi.MessagesFor("zh-Hans")
			if err := tester.Click(messages.MinimalTemplate); err != nil {
				t.Fatal(err)
			}
			tester.Advance(time.Second)
			if err := tester.Click(messages.CopyTemplate); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			if tester.Clipboard() != pythonapi.MinimalScript() {
				t.Fatal("template copy changed Python whitespace")
			}
			copyButton := helpBounds(t, tester, messages.CopyTemplate)
			script := helpBounds(t, tester, strings.TrimRight(pythonapi.MinimalScript(), "\n"))
			if copyButton.X < 250 || copyButton.Y+copyButton.H > script.Y {
				t.Fatalf("template toolbar %v overlaps or is not right-aligned above code %v", copyButton, script)
			}
			tester.SetSize(340, 500)
			titleBefore, ok := tester.AccessByRole(ui.RoleHeading, messages.Title)
			if !ok {
				t.Fatal("missing help heading")
			}
			tester.Scroll(40, 220, 0, 1500)
			tester.Frame()
			titleAfter, ok := tester.AccessByRole(ui.RoleHeading, messages.Title)
			bottom := helpBounds(t, tester, messages.RuntimeDetails)
			if !ok || titleBefore.Bounds != titleAfter.Bounds || bottom.Y+bottom.H > 500 {
				t.Fatalf("header moved or bottom content is unreachable: before %v, after %v, bottom %v", titleBefore.Bounds, titleAfter.Bounds, bottom)
			}
			if err := tester.Click(messages.Close); err != nil {
				t.Fatal(err)
			}
			tester.Advance(time.Second)
			if help.Visible() {
				t.Fatal("custom header close button did not close help")
			}
		})
	}
}

func TestHelpReferenceLongSignaturesFitPanel(t *testing.T) {
	help, tester := helpLayoutTester(t, "en", pythonapi.ScopeInline, apptheme.ModeDark, 340, 900, 16)
	messages := pythonapi.MessagesFor("en")
	if err := tester.Click(messages.Reference); err != nil {
		t.Fatal(err)
	}
	help.query = "onRequest"
	tester.Frame()
	tester.Advance(time.Second)
	signature := helpBounds(t, tester, "def onRequest(context: Context, request: Request) -> Request | None")
	if signature.X < 16 || signature.X+signature.W > 324 || signature.H < 30 {
		t.Fatalf("long signature does not wrap inside the panel: %v", signature)
	}
	tester.Key(0, ui.KeyEscape)
	tester.Advance(time.Second)
	if help.Visible() {
		t.Fatal("Escape did not close reference")
	}
}

func TestHelpTemplateScrollsHorizontallyAtLargerFonts(t *testing.T) {
	help, tester := helpLayoutTester(t, "zh-Hans", pythonapi.ScopeGlobal, apptheme.ModeLight, 340, 1800, 24)
	help.details = []string{"template"}
	tester.Frame()
	tester.Advance(time.Second)
	script := strings.TrimRight(pythonapi.MinimalScript(), "\n")
	before := helpBounds(t, tester, script)
	if before.H > 270 {
		t.Fatalf("code wrapped instead of overflowing horizontally: %v", before)
	}
	beforeImage := tester.Image()
	pixels := bytes.Clone(beforeImage.Pix)
	tester.Scroll(180, before.Y+60, 180, 0)
	tester.Frame()
	after := helpBounds(t, tester, script)
	// Find reports the clipped viewport, so compare the code pixels inside it,
	// excluding the scrollbar and border, to check that text actually moved.
	afterImage := tester.Image()
	moved := false
	for y := int(before.Y) + 20; y < int(before.Y+before.H)-20; y++ {
		start, end := y*beforeImage.Stride+40*4, y*beforeImage.Stride+280*4
		moved = moved || !bytes.Equal(pixels[start:end], afterImage.Pix[start:end])
	}
	if !moved || after.Y != before.Y {
		t.Fatalf("code did not scroll horizontally: before %v, after %v", before, after)
	}
}

func TestRenderPythonHelpLayout(t *testing.T) {
	if !rendertest.Enabled() {
		return
	}
	for _, locale := range []string{"zh-Hans", "en"} {
		for _, mode := range []string{apptheme.ModeLight, apptheme.ModeDark} {
			help, tester := helpLayoutTester(t, locale, pythonapi.ScopeGlobal, mode, 460, 1050, 16)
			help.details = []string{"template"}
			tester.Frame()
			tester.Advance(time.Second)
			rendertest.Shoot(t, tester, "python-help-"+locale+"-"+mode)
			help.tab, help.query = "reference", "write_file"
			tester.Frame()
			tester.Advance(time.Second)
			rendertest.Shoot(t, tester, "python-help-reference-"+locale+"-"+mode)
		}
	}
}
