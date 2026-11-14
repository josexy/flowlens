package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/keytest"
	"github.com/josexy/flowlens/desktop/store"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestHiddenSidebarCannotKeepKeyboardActivation(t *testing.T) {
	f := newFixture(t)
	activated := 0
	f.app.Surfaces[store.WorkspaceCapture] = Surface{Sidebar: func(c *ui.Context, _ nui.Theme) {
		ui.ButtonBase(c).Label("sidebar-entry").Size(100, 40).OnClick(func() { activated++ })
	}}
	f.settle()
	keytest.TabTo(t, f.tester, "sidebar-entry")
	f.data.Workbench.ActivateSection(store.WorkspaceCapture)
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	if activated != 0 || f.tester.Focused("sidebar-entry") {
		t.Fatal("collapsed sidebar kept keyboard activation")
	}
	f.data.Workbench.ActivateSection(store.WorkspaceCapture)
	f.settle()
	keytest.TabTo(t, f.tester, "sidebar-entry")
	f.data.Workbench.ActivateSection(store.WorkspaceRewriteRules)
	f.settle()
	f.tester.Key(0, ui.KeySpace)
	if activated != 0 || f.tester.Focused("sidebar-entry") {
		t.Fatal("inactive page kept keyboard activation")
	}
}
