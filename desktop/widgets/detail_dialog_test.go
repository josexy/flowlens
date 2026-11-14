package widgets

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type detailProbe struct {
	open    bool
	wrap    DetailWrap
	saving  bool
	saves   int
	copies  int
	dialog  ui.Rect
	content ui.Rect
}

func detailView(p *detailProbe) func(*ui.Context) {
	return func(c *ui.Context) {
		theme := nui.App(c, nui.DefaultApp())
		ui.Column(c).Fill().Children(func() {
			ui.Button(c, "Open").OnClick(func() { p.open = true })
		})
		dialog := DetailDialog(c, theme, &p.open, DetailDialogOptions{
			Title:  "Message detail",
			Header: func(c *ui.Context, theme nui.Theme) { nui.Badge(c, theme, "#4", nui.BadgeOptions{Color: nui.Neutral}) },
			Wrap:   &p.wrap,
			Actions: []DetailAction{
				p.wrap.Action("Wrap"),
				{Key: "copy", Icon: lucide.Copy(), Label: "Copy", Run: func() { p.copies++ }},
				{Key: "save", Icon: lucide.Download(), Label: "Save", Disabled: p.saving, Run: func() { p.saves++ }},
			},
			Content: func(c *ui.Context, theme nui.Theme) {
				p.content = ui.Box(c).Fill().Bounds()
			},
		})
		if p.open {
			p.dialog = dialog.Bounds()
		}
	}
}

func openDetailDialog(t *testing.T, p *detailProbe, width, height int) *ui.Tester {
	t.Helper()
	tester := ui.NewTester(detailView(p), width, height)
	reopenDetailDialog(t, tester)
	return tester
}

// reopenDetailDialog opens the dialog and lets its opening transition end.
func reopenDetailDialog(t *testing.T, tester *ui.Tester) {
	t.Helper()
	if err := tester.Click("Open"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(time.Second)
	tester.Frame()
}

func TestDetailDialogSize(t *testing.T) {
	for _, tc := range []struct {
		width, height         int
		maxDialog, wantDialog float32
		content               float32
	}{
		{1600, 1000, 960, 960, 700},
		{900, 600, 828, 828, 420},
	} {
		dialog, content := DetailDialogSize(float32(tc.width), float32(tc.height))
		if dialog > tc.maxDialog || content != tc.content {
			t.Fatalf("%dx%d: size %v × %v", tc.width, tc.height, dialog, content)
		}
		p := &detailProbe{}
		openDetailDialog(t, p, tc.width, tc.height)
		if p.dialog.W > tc.maxDialog || (tc.wantDialog != 0 && p.dialog.W != tc.wantDialog) {
			t.Fatalf("%dx%d: the dialog is %v wide", tc.width, tc.height, p.dialog.W)
		}
		// The content area's 1 px border is inside its height.
		if p.content.H != tc.content-2 {
			t.Fatalf("%dx%d: the content area is %v tall inside its border, want %v", tc.width, tc.height, p.content.H, tc.content-2)
		}
	}
}

func TestDetailDialogContentHasSquareCorners(t *testing.T) {
	p := &detailProbe{}
	tester := openDetailDialog(t, p, 1600, 1000)
	for _, theme := range []nui.Theme{nui.Light(), nui.Dark()} {
		tester.SetDark(theme.Dark)
		tester.Frame()
		frame := tester.Image()
		// The renderer snaps the frame to device pixels. The child is inset by the border.
		left := int(math.Round(float64(p.content.X))) - 1
		right := int(math.Round(float64(p.content.X + p.content.W)))
		top := int(math.Round(float64(p.content.Y))) - 1
		bottom := int(math.Round(float64(p.content.Y + p.content.H)))
		for _, point := range [][2]int{{left, top}, {right, top}, {left, bottom}, {right, bottom}} {
			if got := frame.RGBAAt(point[0], point[1]); !sameColor(got, theme.Border) {
				t.Errorf("dark=%v: border at %v is %v, want %v", theme.Dark, point, got, theme.Border)
			}
		}
	}
}

func TestDetailDialogActionsShowTooltips(t *testing.T) {
	p := &detailProbe{}
	tester := openDetailDialog(t, p, 1600, 1000)
	for _, label := range []string{"Wrap", "Copy", "Save"} {
		rendertest.RequireTooltip(t, tester, label, label)
	}
}

func TestDetailDialogWrapIsPressedAndResetsOnOpen(t *testing.T) {
	p := &detailProbe{}
	tester := openDetailDialog(t, p, 1600, 1000)
	if info, ok := tester.Access("Wrap"); !ok || info.Checked {
		t.Fatalf("wrap starts %+v", info)
	}
	if err := tester.Click("Wrap"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if info, ok := tester.Access("Wrap"); !p.wrap.On() || !ok || !info.Checked {
		t.Fatalf("wrap on %v, shown %+v", p.wrap.On(), info)
	}
	p.open = false
	tester.Frame()
	reopenDetailDialog(t, tester)
	if info, ok := tester.Access("Wrap"); p.wrap.On() || !ok || info.Checked {
		t.Fatalf("after reopening, wrap on %v, shown %+v", p.wrap.On(), info)
	}
}

func TestDetailDialogSaveIsUnavailableWhileSaving(t *testing.T) {
	p := &detailProbe{saving: true}
	tester := openDetailDialog(t, p, 1600, 1000)
	if info, ok := tester.Access("Save"); !ok || !info.Disabled {
		t.Fatalf("save %+v", info)
	}
	_ = tester.Click("Save")
	tester.Frame()
	if p.saves != 0 {
		t.Fatal("a disabled Save ran")
	}
	p.saving = false
	tester.Frame()
	if err := tester.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if p.saves != 1 {
		t.Fatalf("saves %d", p.saves)
	}
}
