package rewrite

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
)

func setRenderTheme(t *testing.T, data *store.Store, dark bool) {
	t.Helper()
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := data.Settings.SetThemeMode(mode); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRewrite(t *testing.T) {
	f := newFixture(t)
	rendertest.ShootThemes(t, f.tester, "rewrite", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
}

func TestRenderRewriteDrag(t *testing.T) {
	if !rendertest.Enabled() {
		return
	}
	f := newFixture(t, store.RewriteState{Revision: 1, Rules: []store.RewriteRule{testRule("a", "Redirect API"), testRule("b", "Edit headers"), testRule("c", "Replace response")}})
	rendertest.ShootThemes(t, f.tester, "rewrite-drag", func(dark bool) {
		f.tester.CancelPress()
		setRenderTheme(t, f.data, dark)
		f.settle()
		x, y := f.center("drag Redirect API")
		f.tester.Press(x, y)
		f.tester.Move(x+32, y+ruleRowHeight*1.6)
		f.tester.Frame()
	})
	f.tester.Key(0, ui.KeyEscape)
	f.tester.CancelPress()
}
