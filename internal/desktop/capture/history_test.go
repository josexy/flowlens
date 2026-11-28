package capture

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestHistoryRowSwitchRetainsDetailsUntilLoaded(t *testing.T) {
	f := newHistoryFixture(t)
	data := store.HistoryDetailData{History: f.data.History}
	panel := inspect.NewPanel(data, inspect.Dependencies{
		Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier,
	})
	panel.OnClose = f.workspace.HistoryTable.CloseDetails
	f.workspace.HistoryDetail, f.workspace.ReleaseHistoryDetail = panel.Build, panel.Release
	f.history.details = make(map[uint64]historyservice.HistoryDetail)
	for _, summary := range f.history.entries["k1"] {
		full := *summary
		full.Response = &proxyservice.HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Record", Value: "detail-" + summary.Host}}}
		f.history.details[summary.ID] = historyservice.HistoryDetail{Entry: &full, Body: &proxyservice.TrafficBodyView{ResponseBody: "body-" + summary.Host}}
	}
	first, second := f.history.entries["k1"][0], f.history.entries["k1"][1]
	f.click("session one")
	f.openDetails(first.Host)
	f.requireText("detail-" + first.Host)
	if err := f.tester.Click(second.Host); err != nil {
		t.Fatal(err)
	}
	f.data.Runtime.Jobs.Wait() // Keep the completed read queued, before publication.
	f.tester.Frame()
	if data.SelectedEntry() != nil || data.RetainedDetail() == nil || f.data.History.Traffic.Selected.ID != second.ID {
		t.Fatal("switch mixed the new selection with the previous detail")
	}
	f.requireText("detail-" + first.Host)
	f.requireNoText("detail-" + second.Host)
	if copy, ok := f.tester.Access(locales.DetailCopyResponseHeadersText()); !ok || !copy.Disabled {
		t.Fatal("previous history headers remain actionable")
	}
	f.settle()
	f.requireText("detail-" + second.Host)
	f.requireNoText("detail-" + first.Host)
	if data.RetainedDetail() != nil || data.SelectedEntry().ID != second.ID {
		t.Fatal("completed detail did not release the old view")
	}
	// Both panes call their body tab "Body"; choose the response tab last.
	var bodyTab ui.AccessInfo
	for _, info := range f.tester.AccessAll() {
		if info.Role == ui.RoleTab && info.Name == locales.DetailResponseBody() {
			bodyTab = info
		}
	}
	f.tester.ClickAt(bodyTab.Bounds.X+bodyTab.Bounds.W/2, bodyTab.Bounds.Y+bodyTab.Bounds.H/2)
	f.settle()
	editor, ok := f.tester.AccessByRole(ui.RoleTextField, "Code editor")
	if !ok {
		t.Fatal("history body editor missing")
	}
	f.tester.ClickAt(editor.Bounds.X+30, editor.Bounds.Y+10)
	if err := f.tester.Click(first.Host); err != nil {
		t.Fatal(err)
	}
	if !f.tester.Focused(locales.TrafficColumns()) {
		t.Fatal("retained editor redirected focus away from the history table")
	}
	if err := f.tester.Click(locales.DetailClosePanel()); err != nil {
		t.Fatal(err)
	}
	if data.RetainedDetail() != nil || data.SelectedEntry() != nil || f.data.History.Traffic.ShowDetailPanel {
		t.Fatal("closing a transition did not immediately release both details")
	}
	f.settle()
	if data.SelectedEntry() != nil {
		t.Fatal("a late read restored closed details")
	}
}

// Hold the completed load in the dispatcher so frames and input exercise the
// real loading state before settle publishes the result on the main thread.
func beginHistoryLoad(f *historyFixture, key string) {
	f.workspace.openHistory(key)
	f.data.Runtime.Jobs.Wait()
	for range 4 {
		f.tester.Frame()
	}
}

func requireHistoryLoadingMask(t *testing.T, f *historyFixture) ui.Rect {
	t.Helper()
	mask, ok := f.tester.AccessByRole(ui.RoleStatus, locales.AppLoading())
	if !ok || !mask.Busy || mask.Bounds.W <= 0 || mask.Bounds.H <= 0 {
		t.Fatalf("loading mask = %+v, found = %v", mask, ok)
	}
	table, ok := f.tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
	if !ok || !table.Disabled || mask.Bounds != table.Bounds {
		t.Fatalf("mask must cover only the disabled table: mask = %+v, table = %+v", mask, table)
	}
	search, ok := f.tester.AccessByRole(ui.RoleTextField, locales.FilterSearchPlaceholder())
	if !ok || search.Disabled || search.Bounds.Y+search.Bounds.H > mask.Bounds.Y {
		t.Fatalf("search must remain usable above the mask: %+v", search)
	}
	return mask.Bounds
}

func TestHistoryOpensAtTop(t *testing.T) {
	for _, descending := range []bool{false, true} {
		t.Run(fmt.Sprintf("descending=%v", descending), func(t *testing.T) {
			f := newHistoryFixture(t)
			for _, key := range []string{"k1", "k2"} {
				entries := make([]*logic.TrafficEntry, 200)
				for index := range entries {
					entries[index] = entry(uint64(index+1), fmt.Sprintf("%s-%d.test", key, index+1))
				}
				f.history.entries[key] = entries
			}
			table := f.workspace.HistoryTable
			if descending {
				table.state.Sort = ui.SortOrder{Column: string(logic.ColumnID), Descending: true}
			}
			requireTop := func() {
				t.Helper()
				if first, _ := table.state.List.Visible(); first != 0 {
					t.Fatalf("opened history starts at row %d, want 0", first)
				}
				f.requireText(table.rows[0].Host)
			}
			for _, key := range []string{"k1", "k2", "k1"} {
				before, _ := table.state.List.Visible()
				beginHistoryLoad(f, key)
				if first, _ := table.state.List.Visible(); f.data.History.Traffic.Key != "" && first != before {
					t.Fatalf("loading moved the previous history from row %d to %d", before, first)
				}
				f.settle()
				requireTop()
				table.state.List.ScrollTo(100, ui.Start)
				f.settle()
				if first, _ := table.state.List.Visible(); first != 100 {
					t.Fatalf("reading history starts at row %d, want 100", first)
				}
			}
			// Reopening the loaded session also starts at the top.
			f.data.Workspace.CloseTab(logic.HistoryTabKey)
			f.settle()
			f.click("session one")
			requireTop()
			// Explicit navigation can still reveal an entry away from the top.
			f.data.History.Traffic.PendingFocusID = table.rows[150].ID
			f.settle()
			if first, _ := table.state.List.Visible(); first != 150 {
				t.Fatalf("focused history starts at row %d, want 150", first)
			}
		})
	}
}

func TestHistoryInitialLoadingKeepsFiltersAndHeader(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			f := newHistoryFixture(t)
			f.setDark(dark)
			rendertest.SettleTheme(f.tester)
			beginHistoryLoad(f, "k1")
			bounds := requireHistoryLoadingMask(t, f)
			f.requireText(locales.TrafficHost())
			f.requireNoText(locales.HistorySelectHint())
			rendertest.Shoot(t, f.tester, fmt.Sprintf("history-initial-loading-dark-%v", dark))
			if err := f.tester.Click(locales.FilterHttp()); err != nil {
				t.Fatal(err)
			}
			if err := f.tester.Click(locales.FilterSearchPlaceholder()); err != nil {
				t.Fatal(err)
			}
			f.tester.Type("saved.test")
			f.tester.Frame()
			if filter := f.data.History.Filter; filter.ActiveTab != logic.TabHTTP || filter.SearchText != "saved.test" {
				t.Fatalf("filters did not accept input while loading: %+v", filter)
			}
			f.settle()
			f.requireNoText(locales.AppLoading())
			f.requireText("saved.test")
			f.requireNoText("other.test")
			table, ok := f.tester.AccessByRole(ui.RoleTable, locales.TrafficColumns())
			if !ok || table.Disabled || table.Bounds != bounds {
				t.Fatalf("loaded table changed layout or stayed disabled: %+v", table)
			}
			f.click("saved.test")
			if selected := f.data.History.Traffic.Selected; selected == nil || selected.ID != 11 {
				t.Fatal("loaded table did not accept selection")
			}
		})
	}
}

func TestHistoryLoadingKeepsRowsAndDetail(t *testing.T) {
	for _, horizontal := range []bool{false, true} {
		t.Run(fmt.Sprintf("horizontal=%v", horizontal), func(t *testing.T) {
			f := newHistoryFixture(t)
			if horizontal {
				f.data.Settings.ToggleTrafficDetailLayout(nil)
			}
			f.setDark(horizontal)
			rendertest.SettleTheme(f.tester)
			detailClicks := 0
			f.workspace.HistoryDetail = func(c *ui.Context, theme nui.Theme) {
				nui.Button(c, theme, "history-detail", nui.ButtonOptions{}).OnClick(func() { detailClicks++ })
			}
			f.click("session one")
			f.click("saved.test")
			f.tester.Key(0, ui.KeyEnter)
			f.settle()
			detailBefore, ok := f.tester.Find("history-detail")
			if !ok {
				t.Fatal("detail did not open")
			}
			f.history.entries["k2"] = []*logic.TrafficEntry{entry(21, "next.test")}
			beginHistoryLoad(f, "k2")
			requireHistoryLoadingMask(t, f)
			f.requireText("saved.test")
			f.requireText("other.test")
			detailDuring, ok := f.tester.Find("history-detail")
			if !ok || detailDuring != detailBefore {
				t.Fatalf("detail moved during loading: before = %+v, during = %+v", detailBefore, detailDuring)
			}
			rendertest.Shoot(t, f.tester, fmt.Sprintf("history-loading-detail-horizontal-%v", horizontal))
			f.tester.Key(0, ui.KeyDown)
			f.tester.Key(0, ui.KeyEnter)
			for _, label := range []string{"other.test", locales.TrafficHost(), "history-detail"} {
				bounds, ok := f.tester.Find(label)
				if !ok {
					t.Fatalf("missing %q", label)
				}
				f.tester.ClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
			}
			f.tester.Frame()
			if f.data.History.Traffic.Selected.ID != 11 || f.workspace.HistoryTable.state.Sort.Column != "" || detailClicks != 1 {
				t.Fatal("mask did not isolate table input from the detail panel")
			}
			f.settle()
			f.requireNoText(locales.AppLoading())
			f.requireText("next.test")
			f.requireNoText("saved.test")
			f.requireNoText("history-detail")
		})
	}
}

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

func TestHistoryRowActionsResolveCompleteRecords(t *testing.T) {
	f := newHistoryFixture(t)
	summary := f.history.entries["k1"][0]
	full := *summary
	full.Request = &proxyservice.HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Repeat", Value: "one"}, {Name: "x-repeat", Value: "two"}, {Name: "X-Empty", Value: ""}}}
	body := &proxyservice.TrafficBodyView{RequestBody: "payload"}
	f.history.details = map[uint64]historyservice.HistoryDetail{summary.ID: {Entry: &full, Body: body}}
	f.click("session one")
	table := f.workspace.HistoryTable
	table.copyCurl([]*logic.TrafficEntry{summary})
	f.settle()
	copied := f.tester.Clipboard()
	if !strings.Contains(copied, "X-Repeat: one") || !strings.Contains(copied, "x-repeat: two") || !strings.Contains(copied, "X-Empty: ") || !strings.Contains(copied, "payload") {
		t.Fatalf("cURL lost stored data: %s", copied)
	}
	var opened *logic.TrafficEntry
	table.deps.OpenEditor = func(entry *logic.TrafficEntry, got *proxyservice.TrafficBodyView, _ bool, key string, done func(error)) {
		opened = entry
		if got != body || key != "k1" {
			t.Fatal("editor lost body or source")
		}
		done(nil)
	}
	table.editEntries([]*logic.TrafficEntry{summary})
	f.settle()
	if opened != &full || summary.Request != nil {
		t.Fatal("action used or modified lightweight row")
	}
}

func TestRenderHistoryMemoryMatrix(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, mode := range []string{"light", "dark"} {
			for _, size := range []int{16, 24} {
				t.Run(fmt.Sprintf("%s-%s-%d", language, mode, size), func(t *testing.T) {
					f := newRenderFixture(t)
					i18n.SetLocaleFunc(func() string { return language })
					table := f.workspace.Table
					tester := ui.NewTester(func(c *ui.Context) {
						palette := apptheme.Begin(c, apptheme.Resolve(apptheme.Appearance{Mode: mode, AppFontSize: size, DisableAnimations: true}).Config)
						table.Build(c, palette)
					}, 1160, 714)
					tester.SetLocale(language)
					for range 6 {
						tester.Frame()
					}
					if _, ok := tester.AccessByRole(ui.RoleTable, locales.TrafficColumns()); !ok {
						t.Fatal("native table role missing")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-table")
					table.state.List.Columns.Widths = map[string]float32{"path": 160}
					table.state.List.Columns.Order = []string{"host", "path", "id", "method"}
					for range 4 {
						tester.Frame()
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-fixed-reordered")
					f.data.Filter.SearchText = "no matching row"
					for range 4 {
						tester.Frame()
					}
					if len(table.rows) != 0 {
						t.Fatal("empty state retained rows")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-empty")
					f.data.Filter.SearchText = ""
					// Test the fill-only path directly; the settings menu normally
					// prevents hiding the last data column.
					definitions := columnDefinitions
					columnDefinitions = nil
					defer func() { columnDefinitions = definitions }()
					for range 4 {
						tester.Frame()
					}
					if len(table.visibleColumns()) != 0 {
						t.Fatal("fill-only fixture still has data columns")
					}
					rendertest.Shoot(t, tester, "history-memory-"+t.Name()+"-fill-only")
				})
			}
		}
	}
}

func TestHARImportFailureStaysEightSeconds(t *testing.T) {
	f := newHistoryFixture(t)
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		if path == "/bad.har" {
			return proxyservice.HARImportResult{}, errors.New("corrupt")
		}
		return proxyservice.HARImportResult{Imported: 3, Metadata: &proxyservice.HistoryMetadata{Key: "k1"}}, nil
	}
	f.workspace.ImportHAR([]string{"/bad.har", "/good.har"})
	f.settle()
	f.requireText("corrupt")
	f.tester.Advance(7 * time.Second)
	f.requireText("corrupt")
	f.tester.Advance(2 * time.Second)
	f.requireNoText("corrupt")
}

func TestHARImportResultStaysFiveSeconds(t *testing.T) {
	f := newHistoryFixture(t)
	f.history.import_ = func(path string) (proxyservice.HARImportResult, error) {
		return proxyservice.HARImportResult{Imported: 1, Metadata: &proxyservice.HistoryMetadata{Key: "k1"}}, nil
	}
	f.workspace.ImportHAR([]string{"/good.har"})
	f.settle()
	f.requireText("Imported 1 files")
	f.tester.Advance(4 * time.Second)
	f.requireText("Imported 1 files")
	f.tester.Advance(2 * time.Second)
	f.requireNoText("Imported 1 files")
}
