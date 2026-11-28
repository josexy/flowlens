package rewrite

import (
	"errors"
	"fmt"
	"slices"
	"sync"
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

func TestMutationFeedbackStaysOnActiveControl(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, kind := range []string{store.RewriteMutationEnabled, store.RewriteMutationRuleEnabled, store.RewriteMutationSave} {
			for _, fail := range []bool{false, true} {
				outcome := "success"
				if fail {
					outcome = "failure"
				}
				t.Run(mode+"/"+kind+"/"+outcome, func(t *testing.T) {
					f := newFixture(t)
					setRenderTheme(t, f.data, dark)
					f.edit("edited")
					f.settle()
					f.tester.Advance(time.Second)
					before := f.tester.Image()
					labels := []string{locales.RewriteRulesEnableRule("b"), locales.RewriteRulesRefresh(), locales.RewriteRulesNewRule(), locales.RewriteRulesRevert(), locales.RewriteRulesDelete(), "drag b"}
					if kind != store.RewriteMutationEnabled {
						labels = append(labels, locales.RewriteRulesEnableAll())
					}
					if kind != store.RewriteMutationRuleEnabled {
						labels = append(labels, locales.RewriteRulesEnableRule("edited"))
					}
					if kind != store.RewriteMutationSave {
						labels = append(labels, locales.RewriteRulesSave())
					}
					bounds := make(map[string]ui.Rect)
					for _, label := range labels {
						info, ok := f.tester.Access(label)
						if !ok || info.Disabled {
							t.Fatalf("control %q unavailable before write: %+v", label, info)
						}
						bounds[label] = info.Bounds
					}
					form := f.form()
					rule := f.data.Rewrite.SelectedRule()
					key := f.data.Rewrite.SelectedDraft().EditorKey
					release := make(chan struct{})
					finish := sync.OnceFunc(func() { close(release) })
					t.Cleanup(finish)
					wait := func() error {
						<-release
						if fail {
							return errors.New("disk full")
						}
						return nil
					}
					label := locales.RewriteRulesSave()
					switch kind {
					case store.RewriteMutationEnabled:
						label = locales.RewriteRulesEnableAll()
						f.backend.enableAll = func(enabled bool, _ int64) (store.RewriteState, error) {
							if err := wait(); err != nil {
								return store.RewriteState{}, err
							}
							return f.backend.apply("enabled", func(next *store.RewriteState) { next.Enabled = enabled })
						}
					case store.RewriteMutationRuleEnabled:
						label = locales.RewriteRulesEnableRule("edited")
						f.backend.enable = func(id string, enabled bool, _ int64) (store.RewriteState, error) {
							if err := wait(); err != nil {
								return store.RewriteState{}, err
							}
							return f.backend.applyEnable(id, enabled)
						}
					case store.RewriteMutationSave:
						f.backend.save = func(candidate store.RewriteRule, revision int64) (store.RewriteState, error) {
							if err := wait(); err != nil {
								return store.RewriteState{}, err
							}
							return f.backend.defaultSave(candidate, revision)
						}
					}
					f.click(label)
					if !f.data.Rewrite.Busy() {
						t.Fatal("write did not start")
					}
					f.tester.Move(1399, 1499)
					// Extra pending frames must keep the selected value and all unrelated
					// controls steady, while duplicate writes remain disabled.
					for range 3 {
						f.tester.Advance(100 * time.Millisecond)
						info, ok := f.tester.Access(label)
						if !ok || !info.Disabled || (kind != store.RewriteMutationSave && !info.Checked) {
							t.Errorf("pending control lost its selected state or write guard: %+v", info)
						}
						for _, other := range labels {
							info, ok := f.tester.Access(other)
							if !ok || !info.Disabled || info.Bounds != bounds[other] {
								t.Errorf("unrelated control %q moved or lost its write guard: %+v", other, info)
							}
							rendertest.RequireSamePixels(t, before, f.tester.Image(), bounds[other], other)
						}
						// The form below the toolbar must remain rendered in place.
						rendertest.RequireSamePixels(t, before, f.tester.Image(), ui.Rect{X: 320, Y: 48, W: 1080, H: 1400}, "form")
					}
					rendertest.Shoot(t, f.tester, "rewrite-pending-"+kind+"-"+mode)
					f.click(label)
					finish()
					f.idle()
					if f.form() != form || f.data.Rewrite.SelectedRule() != rule || f.data.Rewrite.SelectedDraft().EditorKey != key {
						t.Fatal("write replaced the active editor")
					}
					if fail {
						if f.data.Rewrite.Error != "disk full" || !f.data.Rewrite.IsDirty("a") {
							t.Fatal("failed write lost its error or draft")
						}
					} else if len(f.backend.log()) != 1 {
						t.Fatal("write repeated", f.backend.log())
					}
					if kind != store.RewriteMutationSave {
						info, ok := f.tester.Access(label)
						if !ok || info.Disabled || info.Checked == fail {
							t.Fatalf("switch did not settle to the persisted value: %+v", info)
						}
					}
				})
			}
		}
	}
}

func TestLargeFontDragFollowsVisibleRowPitch(t *testing.T) {
	f := newFixture(t)
	f.data.Settings.BeginEdit()
	f.data.Settings.Draft.CommonConfig.AppFontSize = 24
	f.data.Settings.PreviewDraftAppearance()
	f.settle()
	a, ok := f.tester.Find("a")
	if !ok {
		t.Fatal("first rule missing")
	}
	b, ok := f.tester.Find("b")
	if !ok || b.Y-a.Y <= ruleRowHeight {
		t.Fatalf("large font rows did not grow: %+v, %+v", a, b)
	}
	pitch := b.Y - a.Y
	// Less than half the enlarged row stays in place. Using the old fixed
	// pitch would incorrectly reorder this drag.
	dragGrip(f, "a", pitch*0.4)
	f.settle()
	if f.ids() != "a,b" {
		t.Fatalf("short drag crossed the wrong row: %s", f.ids())
	}
	dragGrip(f, "a", pitch*1.6)
	f.await("reordered large rows", func() bool { return f.ids() == "b,a" })
}
