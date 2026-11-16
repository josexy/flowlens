package rewrite

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
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
	method, hasMethod := f.tester.AccessByRole(ui.RolePopUpButton, locales.RewriteRulesMethod())
	pattern, hasPattern := f.tester.AccessByRole(ui.RoleTextField, locales.RewriteRulesUrlPattern())
	menu, input := method.Bounds, pattern.Bounds
	if !hasMethod || !hasPattern || menu.Y+menu.H/2 != input.Y+input.H/2 || menu.X+menu.W > input.X {
		t.Fatalf("method and URL fields are not aligned: method %v, URL %v", menu, input)
	}
	rendertest.ShootThemes(t, f.tester, "rewrite", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
}

func TestRenderDeleteRuleDialog(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, dirty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dirty=%v", language, dirty), func(t *testing.T) {
				f := newFixture(t)
				i18n.SetLocaleFunc(func() string { return language })
				f.tester.SetLocale(language)
				if dirty {
					f.edit(locales.RewriteRulesNewName())
				}
				f.pump()
				f.click(locales.RewriteRulesDelete())
				f.tester.Advance(time.Second)
				cancel, ok := f.tester.AccessByRole(ui.RoleButton, locales.RewriteRulesCancel())
				if !ok {
					t.Fatal("missing cancel button")
				}
				var buttons []string
				for _, node := range f.tester.AccessAll() {
					if node.Role == ui.RoleButton && node.Bounds.Y == cancel.Bounds.Y {
						buttons = append(buttons, node.Name)
					}
				}
				if !slices.Equal(buttons, []string{locales.RewriteRulesCancel(), locales.RewriteRulesDeleteAction()}) {
					t.Fatalf("delete footer buttons: %v", buttons)
				}
				message := locales.RewriteRulesDeleteConfirm(f.view.remove.name)
				if dirty {
					message = locales.RewriteRulesDeleteDirty(f.view.remove.name)
				}
				if !f.has(message) {
					t.Fatal("missing deletion warning", f.tester.Texts())
				}
				rendertest.ShootThemes(t, f.tester, fmt.Sprintf("rewrite-delete-%s-dirty-%v", language, dirty), func(dark bool) {
					setRenderTheme(t, f.data, dark)
					f.settle()
					f.tester.Advance(time.Second)
				})
			})
		}
	}
}

func TestRenderRewritePreview(t *testing.T) {
	if !rendertest.Enabled() {
		return
	}
	f, rule := regexFixture(t)
	rule.Action.Body.Pattern, rule.Action.Body.Replacement = "POST", "X-POST-Y"
	f.form().preview.sample = `"method":"POST"`
	f.pump()
	f.click(locales.RewriteRulesRegexTestPreview())
	f.preview(0).respond(1, `"method":"X-POST-Y"`)
	f.waitIdle()
	rendertest.ShootThemes(t, f.tester, "rewrite-preview", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
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
