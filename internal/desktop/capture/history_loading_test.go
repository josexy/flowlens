package capture

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

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
				f.requireText(table.Rows()[0].Host)
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
			f.data.History.Traffic.PendingFocusID = table.Rows()[150].ID
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
