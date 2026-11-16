package rewrite

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

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
