package capture

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/editor"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/shell"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestWorkspaceTabsPreserveSidebarNavigation(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, appearance := range []string{"light", "dark"} {
			for _, section := range []store.WorkspaceSection{store.WorkspaceCapture, store.WorkspaceCategory, store.WorkspaceAPICollection} {
				t.Run(language+"/"+appearance+"/"+string(section), func(t *testing.T) {
					f := newHistoryFixture(t, entry(1, "live.test")).fixture
					f.click("session one")
					app := shell.New(f.data, shell.Host{})
					app.Surfaces[store.WorkspaceCapture] = shell.Surface{Sidebar: f.workspace.Sidebar, Content: f.workspace.Content}
					app.Surfaces[store.WorkspaceCategory] = shell.Surface{Sidebar: f.workspace.CategorySidebar, Content: f.workspace.Content, SidebarHidden: f.workspace.CategorySidebarHidden}
					app.Surfaces[store.WorkspaceAPICollection] = shell.Surface{
						Sidebar: func(c *ui.Context, _ nui.Theme) { ui.Text(c, "api-sidebar") }, Content: f.workspace.Content,
					}
					f.workspace.Dispatcher = app.Dispatcher
					ed := editor.New(f.data, editor.Host{}, app.Dispatcher, &app.Notifier, &app.Confirm)
					f.workspace.TabBar, f.workspace.RequestTab = ed.TabBar, ed.Build
					f.tester = ui.NewTester(app.View, 1400, 800)
					i18n.SetLocaleFunc(func() string { return language })
					t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return "en" }) })
					f.tester.SetLocale(language)
					f.setDark(appearance == "dark")
					workbench, workspace := f.data.Workbench, f.data.Workspace
					workbench.ShowSection(section)
					workbench.SecondarySidebarPercent = 27
					f.settle()

					clickTab := func(key string) {
						t.Helper()
						tab, ok := f.tester.AccessByRole(ui.RoleTab, editor.DisplayTitle(workspace.FindTab(key)))
						if !ok {
							t.Fatalf("missing tab %q", key)
						}
						f.tester.ClickAt(tab.Bounds.X+tab.Bounds.W/2, tab.Bounds.Y+tab.Bounds.H/2)
						f.settle()
					}
					check := func(active, selection string, visible bool) {
						t.Helper()
						f.settle()
						if workspace.ActiveKey != active || workbench.CaptureSelection != selection {
							t.Fatalf("active tab %q, source %q; want %q, %q", workspace.ActiveKey, workbench.CaptureSelection, active, selection)
						}
						if workbench.ActiveSection != section || workbench.SecondarySidebarVisible != visible || workbench.SecondarySidebarPercent != 27 || workbench.ActiveContent != store.ContentTraffic {
							t.Fatalf("tab changed sidebar navigation: %+v", workbench)
						}
						if section == store.WorkspaceCategory && visible {
							label := locales.CategorySourceCapture()
							if selection == historyItemPrefix+"k1" {
								label = locales.CategorySourceHistory("session one")
							}
							f.requireText(label)
						}
						if section == store.WorkspaceAPICollection && visible {
							f.requireText("api-sidebar")
						}
					}
					for _, visible := range []bool{true, false} {
						workbench.SecondarySidebarVisible = visible
						f.settle()
						clickTab(logic.CaptureTabKey)
						check(logic.CaptureTabKey, store.CaptureItemLive, visible)
						clickTab(logic.HistoryTabKey)
						check(logic.HistoryTabKey, historyItemPrefix+"k1", visible)
						f.tester.Key(ui.Ctrl, ui.KeyTab)
						check(logic.CaptureTabKey, store.CaptureItemLive, visible)
						f.tester.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
						check(logic.HistoryTabKey, historyItemPrefix+"k1", visible)
					}

					f.click(locales.WorkspaceTabContextCloseCurrent() + " session one")
					check(logic.CaptureTabKey, store.CaptureItemLive, false)
					if f.data.History.Traffic.Key != "" {
						t.Fatal("closing the active history kept its data")
					}
					f.workspace.openHistory("k1")
					f.settle()
					workbench.ShowSection(section)
					workbench.SecondarySidebarVisible = false
					key := workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
					check(key, historyItemPrefix+"k1", false)
					workspace.CloseTab(logic.HistoryTabKey)
					check(key, store.CaptureItemLive, false)
					if f.data.History.Traffic.Key != "" {
						t.Fatal("closing the inactive history kept its data")
					}

					// Explicit navigation still opens Capture and selects live traffic.
					f.click(locales.MenuCapture())
					if workbench.ActiveSection != store.WorkspaceCapture || !workbench.SecondarySidebarVisible {
						t.Fatalf("explicit Capture navigation failed: %+v", workbench)
					}
					f.workspace.selectItem(store.CaptureItemLive)
					f.settle()
					if workspace.ActiveKey != logic.CaptureTabKey {
						t.Fatal("explicit live capture selection did not activate its tab")
					}
				})
			}
		}
	}
}

func TestCaptureAndHistoryKeyboardNavigation(t *testing.T) {
	f := newHistoryFixture(t)
	f.tabToCategory(ui.RoleList, locales.HistoryTitle())
	f.tester.Key(0, ui.KeyHome)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.History.Traffic.Key != "k1" {
		t.Fatal("keyboard did not open history")
	}
	keytest.TabTo(t, f.tester, locales.MenuCapture())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Workbench.CaptureSelection != store.CaptureItemLive || len(f.backend.restarts) != 0 {
		t.Fatal("capture activation did not select capture independently")
	}
}

func TestCategoryKeyboardNavigationAndHiddenEntries(t *testing.T) {
	f := categoryFixture(t)
	f.tabToCategory(ui.RoleListItem, "api.test")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 1 || f.data.Filter.SelectedHosts[0] != "api.test" {
		t.Fatal("keyboard category activation did not filter")
	}
	f.tabToCategory(ui.RoleDisclosure, locales.CategoryHost())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if slices.Contains(f.workspace.category.sections, sectionHost) {
		t.Fatal("keyboard did not collapse the host group")
	}
	f.showCategories = false
	f.settle()
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if slices.Contains(f.workspace.category.sections, sectionHost) || len(f.data.Filter.SelectedHosts) != 1 {
		t.Fatal("hidden category handled a key")
	}
}

func TestCategoryListboxKeyboardFollowsVisibleItems(t *testing.T) {
	f := categoryFixture(t)
	f.tabToCategory(ui.RoleListItem, "api.test")
	f.workspace.category.search = "cdn.test"
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !slices.Equal(f.data.Filter.SelectedHosts, []string{"cdn.test"}) {
		t.Fatalf("keyboard selected a filtered-out host: %v", f.data.Filter.SelectedHosts)
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if len(f.data.Filter.SelectedHosts) != 0 {
		t.Fatal("Space did not clear the selected host")
	}
}

func (f *fixture) tabToCategory(role ui.Role, label string) {
	f.t.Helper()
	for range 64 {
		for _, item := range f.tester.AccessAll() {
			if item.Role == role && item.Name == label && item.Focused {
				return
			}
		}
		f.tester.Key(0, ui.KeyTab)
	}
	f.t.Fatalf("keyboard focus cannot reach %q", label)
}

func TestAliasDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.workspace.ShowAliasDialog()
	f.settle()
	f.tester.Key(ui.Cmd, ui.KeyA)
	f.tester.Type("session")
	f.settle()
	if f.workspace.alias.draft != "session" {
		t.Fatalf("draft = %q", f.workspace.alias.draft)
	}
}

func TestDomainFilterAddedRowTakesTyping(t *testing.T) {
	f := newFixture(t)
	f.click("Domain filters: 0")
	dialog := &f.workspace.toolbar.hosts
	dialog.add(&dialog.include)
	f.settle()
	f.tester.Type("a.test")
	f.settle()
	if len(dialog.include) != 1 || dialog.include[0].value != "a.test" {
		t.Fatalf("%+v", dialog.include)
	}
	dialog.add(&dialog.exclude)
	f.settle()
	f.tester.Type("b.test")
	f.settle()
	if dialog.include[0].value != "a.test" || dialog.exclude[0].value != "b.test" {
		t.Fatalf("%+v %+v", dialog.include, dialog.exclude)
	}
}
