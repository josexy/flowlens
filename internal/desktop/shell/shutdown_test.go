package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
)

func TestShutdownCardBlocksWorkbench(t *testing.T) {
	f := newFixture(t)
	reported := 0
	f.app.Host.ShutdownUIReady = func() { reported++ }
	f.tester.Frame()
	rewrite, ok := f.tester.Find(locales.MenuRewriteRules())
	if !ok {
		t.Fatal("no Rewrite Rules button")
	}
	f.data.ShuttingDown = true
	for range 4 {
		f.tester.Frame()
	}
	for _, text := range []string{locales.AppShutdownTitle(), locales.AppShutdownDescription()} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %q", text, f.tester.Texts())
		}
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); !ok {
		t.Fatal("no spinner")
	}
	f.tester.ClickAt(rewrite.X+rewrite.W/2, rewrite.Y+rewrite.H/2)
	f.tester.Frame()
	f.tester.Key(0, ui.KeyEscape)
	f.tester.Frame()
	f.tester.Frame()
	if f.data.Workbench.ActiveSection != store.WorkspaceCapture {
		t.Fatalf("workbench took a click while shutting down: %v", f.data.Workbench.ActiveSection)
	}
	if !f.tester.HasText(locales.AppShutdownDescription()) {
		t.Fatal("Escape dismissed the indicator")
	}
	if reported != 1 {
		t.Fatalf("ShutdownUIReady called %d times", reported)
	}
}
