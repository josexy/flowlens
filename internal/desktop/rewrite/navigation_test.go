package rewrite

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/locales"
)

func TestKeyboardRuleNavigationPreservesDraft(t *testing.T) {
	f := newFixture(t)
	keytest.TabTo(t, f.tester, "a")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.edit("draft-a")
	keytest.TabTo(t, f.tester, "b")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.data.Rewrite.SelectedID != "b" {
		t.Fatal("keyboard did not select the second rule")
	}
	keytest.TabTo(t, f.tester, "draft-a")
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Rewrite.SelectedID != "a" || f.data.Rewrite.SelectedRule().Name != "draft-a" || !f.data.Rewrite.IsDirty("a") {
		t.Fatal("keyboard navigation lost the rule draft")
	}
	if len(f.backend.log()) != 0 {
		t.Fatal("navigation changed persisted rules", f.backend.log())
	}
	keytest.TabTo(t, f.tester, locales.RewriteRulesEnableRule("b"))
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.backend.count("rule-enabled") != 1 || f.data.Rewrite.SelectedID != "a" {
		t.Fatal("enable switch also activated the rule", f.backend.log())
	}
}
