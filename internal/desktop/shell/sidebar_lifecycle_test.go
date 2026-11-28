package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestSidebarHiddenNotifiesEachTransitionOnce(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.app.Surfaces[store.WorkspaceCategory] = Surface{
		Sidebar:       func(c *ui.Context, _ nui.Theme) { ui.Text(c, "categories") },
		SidebarHidden: func() { calls++ },
	}
	for _, section := range store.WorkspaceSections {
		if section == store.WorkspaceCategory {
			continue
		}
		f.data.Workbench.ActiveSection = store.WorkspaceCategory
		f.data.Workbench.SecondarySidebarVisible = true
		f.settle()
		before := calls
		f.data.Workbench.ActiveSection = section
		for range 3 {
			f.settle()
		}
		if calls != before+1 {
			t.Fatalf("%s: hide called %d times", section, calls-before)
		}
	}
	f.data.Workbench.ActiveSection = store.WorkspaceCategory
	f.data.Workbench.SecondarySidebarVisible = true
	f.settle()
	before := calls
	f.data.Workbench.SecondarySidebarVisible = false
	for range 3 {
		f.settle()
	}
	if calls != before+1 {
		t.Fatal("collapse did not notify exactly once")
	}
	f.data.Workbench.SecondarySidebarVisible = true
	f.settle()
	if calls != before+1 {
		t.Fatal("reopen notified hidden")
	}
}
