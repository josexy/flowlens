package capture

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func historySidebarFixture(t *testing.T, count int) *fixture {
	t.Helper()
	backend := &fakeHistoryBackend{entries: map[string][]*logic.TrafficEntry{}}
	for i := range count {
		key := fmt.Sprintf("session-%04d", i)
		backend.list = append(backend.list, &proxyservice.HistoryMetadata{Key: key, Alias: key, CreatedAt: int64(count - i), Total: 1234})
	}
	return newFixtureSized(t, nil, 1400, 800, backend)
}

func TestHistorySidebarVirtualizesAndReachesLastRow(t *testing.T) {
	for _, count := range []int{100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := historySidebarFixture(t, count)
			f.workspace.historyRowBuilds = 0
			f.tester.Frame()
			if rows := f.workspace.historyRowBuilds; rows == 0 || rows > 30 {
				t.Fatalf("unbounded row construction: %d", rows)
			}
			f.click("session-0000")
			f.tester.Key(0, ui.KeyEnd)
			f.settle()
			want := fmt.Sprintf("session-%04d", count-1)
			if f.workspace.historyCursorKey != want || f.data.History.Traffic.Key == want {
				t.Fatal("navigation activated unexpectedly or lost identity")
			}
			f.tester.Key(0, ui.KeyEnter)
			f.settle()
			if f.data.History.Traffic.Key != want {
				t.Fatal("keyboard activation did not open last session")
			}
			for _, height := range []int{450, 950} {
				f.tester.SetSize(1400, height)
				f.tester.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.5})
				f.workspace.historyList.ScrollTo(count-1, ui.End)
				f.settle()
				row, ok := f.tester.AccessByRole(ui.RoleListItem, want)
				list, _ := f.tester.AccessByRole(ui.RoleList, locales.HistoryTitle())
				if !ok || row.Offscreen || row.Bounds.H < 38 || row.Bounds.Y+row.Bounds.H > list.Bounds.Y+list.Bounds.H+1 {
					t.Fatalf("last row clipped: %+v in %+v", row, list)
				}
			}
		})
	}
}

func TestHistorySidebarRefreshKeepsIdentityAndMenuTarget(t *testing.T) {
	f := historySidebarFixture(t, 1000)
	f.click("session-0000")
	slices.Reverse(f.data.History.Metadata)
	f.data.History.ListVersion++
	f.settle()
	if f.workspace.historyCursorKey != "session-0000" || f.data.History.Traffic.Key != "session-0000" || f.workspace.historyCursor != 999 {
		t.Fatal("reorder retargeted focused/open session")
	}
	f.workspace.historyList.ScrollTo(999, ui.End)
	f.settle()
	f.tester.Key(ui.Shift, ui.KeyF10)
	f.settle()
	if !menutest.Shown(f.tester) || f.workspace.historyMenuTarget == nil || f.workspace.historyMenuTarget.Key != "session-0000" {
		t.Fatal("keyboard menu targets wrong session")
	}
	f.workspace.historyList.ScrollTo(0, ui.Start)
	f.settle()
	if f.workspace.historyMenu.Open || f.workspace.historyMenuTarget != nil || menutest.Shown(f.tester) {
		t.Fatal("offscreen menu retained its old anchor")
	}
	if err := menutest.Open(f.tester, f.settle, "session-0999"); err != nil {
		t.Fatal(err)
	}
	if target := f.workspace.historyMenuTarget; target == nil || target.Key != "session-0999" {
		t.Fatal("pointer menu targets wrong session")
	}
	f.workspace.HistorySidebarHidden()
	f.showCategories = true
	f.settle()
	if f.workspace.historyMenuTarget != nil || f.workspace.historyMenu.Open {
		t.Fatal("hidden sidebar retained menu")
	}
	f.showCategories = false
	f.data.History.Metadata = f.data.History.Metadata[:999]
	f.data.History.ListVersion++
	f.settle()
	if f.workspace.historyCursor != -1 || f.data.History.Traffic.Key != "" {
		t.Fatalf("cursor removal: cursor=%d key=%q open=%q", f.workspace.historyCursor, f.workspace.historyCursorKey, f.data.History.Traffic.Key)
	}
}

func TestHistorySidebarEmptyAndLoadingBuildNoRows(t *testing.T) {
	f := historySidebarFixture(t, 0)
	for _, loading := range []bool{false, true} {
		f.data.History.Loading = loading
		f.workspace.historyRowBuilds = 0
		f.settle()
		if f.workspace.historyRowBuilds != 0 {
			t.Fatal("empty/loading list built rows")
		}
	}
}

func TestHistorySidebarNarrowScaledRowsStayReadable(t *testing.T) {
	f := historySidebarFixture(t, 100)
	for _, item := range f.data.History.Metadata {
		item.Total = 100000
	}
	f.tester = ui.NewTester(func(c *ui.Context) {
		palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{AppFontSize: 24, DisableAnimations: true}).Config)
		ui.Column(c).Fill().Children(func() { f.workspace.historySidebar(c, palette) })
	}, 170, 714)
	f.settle()
	label := historyTime(f.data.History.Metadata[0])
	clock, ok := f.tester.AccessByRole(ui.RoleText, label)
	if !ok || clock.Bounds.H > 32 {
		t.Fatalf("narrow time label wrapped vertically: %+v", clock)
	}
	header, ok := f.tester.AccessByRole(ui.RoleText, locales.HistoryTitle())
	if !ok || header.Bounds.H > 40 {
		t.Fatalf("narrow header wrapped vertically: %+v", header)
	}
}

func TestRenderHistoryAndCategorySidebars(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, mode := range []string{"light", "dark"} {
			for _, size := range []int{16, 24} {
				t.Run(fmt.Sprintf("%s-%s-%d", language, mode, size), func(t *testing.T) {
					f := historySidebarFixture(t, 1000)
					i18n.SetLocaleFunc(func() string { return language })
					f.tester = ui.NewTester(func(c *ui.Context) {
						palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode, AppFontSize: size, DisableAnimations: true}).Config)
						ui.Column(c).Fill().Background(palette.Background).Children(func() {
							if f.showCategories {
								f.workspace.CategorySidebar(c, palette)
							} else {
								f.workspace.historySidebar(c, palette)
							}
						})
					}, 320, 714)
					f.tester.SetLocale(language)
					f.settle()
					rendertest.Shoot(t, f.tester, "history-sidebar-"+t.Name())
					f.data.History.Traffic.Load("categories", rangeOfEntries(1000))
					f.data.Workbench.SelectCaptureItem(historyItemPrefix + "categories")
					f.showCategories = true
					f.workspace.HistorySidebarHidden()
					f.settle()
					rendertest.Shoot(t, f.tester, "category-sidebar-"+t.Name())
					f.workspace.CategorySidebarHidden()
				})
			}
		}
	}
}
