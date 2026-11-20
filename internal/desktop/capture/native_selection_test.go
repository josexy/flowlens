package capture

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func TestNativeSelectionFollowsIdentityAndDisplayedRange(t *testing.T) {
	f := newFixture(t, entry(1, "d.test"), entry(2, "b.test"), entry(3, "a.test"), entry(4, "c.test"))
	f.clickRow(0, "b.test")
	table := f.workspace.Table
	table.state.Sort = ui.SortOrder{Column: string(logic.ColumnHost)}
	table.lastSort = table.state.Sort
	f.settle()
	f.requireSelection([]uint64{2}, 2, false)
	f.clickRow(ui.Shift, "d.test")
	f.requireSelection([]uint64{2, 4, 1}, 1, false)
	f.data.Filter.SearchText = "b.test"
	f.settle()
	f.requireSelection([]uint64{2}, 2, false)
	f.data.Filter.SearchText = "no-such-host"
	f.settle()
	f.requireSelection(nil, 0, false)
}

func TestNativeDoubleClickTogglesDetails(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			name := map[bool]string{false: "live", true: "history"}[history] + "/" + map[bool]string{false: "light", true: "dark"}[dark]
			t.Run(name, func(t *testing.T) {
				f := newHistoryFixture(t, manyEntries(3)...).fixture
				f.setDark(dark)
				table := f.workspace.Table
				var data inspect.Data = inspect.LiveData{Traffic: f.data.Traffic}
				if history {
					f.click("session one")
					table = f.workspace.HistoryTable
					data = store.HistoryDetailData{History: f.data.History}
				}
				panel := inspect.NewPanel(data, inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier})
				panel.OnClose = table.CloseDetails
				if history {
					f.workspace.HistoryDetail = panel.Build
				} else {
					f.workspace.Detail = panel.Build
				}
				f.settle()
				first, second := table.Rows()[0], table.Rows()[1]
				check := func(ids []uint64, focused uint64, visible bool) {
					t.Helper()
					entry := table.source.SelectedEntry()
					if got := selectedIDs(table); !slices.Equal(got, ids) || entry == nil || entry.ID != focused || table.source.ShowDetail() != visible {
						t.Fatalf("selected %v focused %+v detail %v, want %v %d %v", got, entry, table.source.ShowDetail(), ids, focused, visible)
					}
					if !f.tester.Focused(locales.TrafficColumns()) {
						t.Fatal("table lost keyboard focus")
					}
				}
				for _, visible := range []bool{true, false, true} {
					f.tester.Advance(time.Second)
					f.clickRow(0, first.Host)
					check([]uint64{first.ID}, first.ID, !visible)
					f.clickRow(0, first.Host)
					check([]uint64{first.ID}, first.ID, visible)
				}
				// Enter opens details even after a double-click; it never hides them.
				for range 2 {
					f.tester.Key(0, ui.KeyEnter)
					f.settle()
					check([]uint64{first.ID}, first.ID, true)
				}
				// Modifier clicks only change selection, even when repeated quickly.
				for _, visible := range []bool{true, false} {
					if !visible {
						table.CloseDetails()
						f.settle()
					}
					f.tester.Advance(time.Second)
					f.clickRow(ui.Cmd, second.Host)
					check([]uint64{first.ID, second.ID}, second.ID, visible)
					f.clickRow(ui.Cmd, second.Host)
					check([]uint64{first.ID}, first.ID, visible)
				}
				// Restore the native cursor from the deselected second row.
				f.tester.Advance(time.Second)
				f.clickRow(0, first.Host)
				check([]uint64{first.ID}, first.ID, false)
				f.tester.Key(0, ui.KeyEnter)
				f.settle()
				check([]uint64{first.ID}, first.ID, true)
				// The first click changes the inspected row; the second hides details.
				f.tester.Advance(time.Second)
				f.clickRow(0, second.Host)
				check([]uint64{second.ID}, second.ID, true)
				f.clickRow(0, second.Host)
				check([]uint64{second.ID}, second.ID, false)
			})
		}
	}
}

func TestDetailCloseRetainsSelectionAndReturnsFocus(t *testing.T) {
	for _, history := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "history"}[history], func(t *testing.T) {
			h := newHistoryFixture(t, entry(1, "a.test"), entry(2, "b.test"))
			f := h.fixture
			table := f.workspace.Table
			var data inspect.Data = inspect.LiveData{Traffic: f.data.Traffic}
			if history {
				f.click("session one")
				table = f.workspace.HistoryTable
				data = store.HistoryDetailData{History: f.data.History}
			}
			panel := inspect.NewPanel(data, inspect.Dependencies{Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier})
			panel.OnClose = table.CloseDetails
			if history {
				f.workspace.HistoryDetail = panel.Build
			} else {
				f.workspace.Detail = panel.Build
			}
			f.settle()
			// The history fixture uses its own traffic hosts.
			row := table.Rows()[0]
			f.openDetails(row.Host)
			keytest.TabTo(t, f.tester, locales.DetailClosePanel())
			f.tester.Key(0, ui.KeySpace)
			f.settle()
			if table.source.ShowDetail() || !table.state.Selection.Has(trafficRowID(row.ID)) || table.state.Selection.Len() != 1 || !f.tester.Focused(locales.TrafficColumns()) {
				t.Fatalf("close: visible %v selected %v focus %v", table.source.ShowDetail(), table.selectedIDs(), f.tester.Focused(locales.TrafficColumns()))
			}
			f.tester.Key(0, ui.KeyDown)
			f.settle()
			if table.source.ShowDetail() {
				t.Fatal("selection reopened details")
			}
		})
	}
}

func TestDetailEnterStaysWithinTheTable(t *testing.T) {
	f := newFixture(t, manyEntries(3)...)
	f.clickRow(0, "host1.test")
	f.tester.Key(ui.Cmd, ui.KeyF)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	f.requireSelection([]uint64{1}, 1, false)
	f.confirm.Ask(widgets.ConfirmRequest{Title: "blocking", Message: "modal", Confirm: widgets.ConfirmAction{Label: "okay"}})
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.data.Traffic.ShowDetailPanel {
		t.Fatal("Enter in a modal opened details behind it")
	}
}

func TestLateDetailBodyCannotReplaceTheCurrentView(t *testing.T) {
	for _, closePanel := range []bool{false, true} {
		t.Run(map[bool]string{false: "switch", true: "close"}[closePanel], func(t *testing.T) {
			f := newFixture(t, entry(1, "a.test"), entry(2, "b.test"))
			started, release := make(chan struct{}), make(chan struct{})
			f.backend.readBody = func(id uint64) (*proxyservice.TrafficBodyView, error) {
				if id == 1 {
					close(started)
					<-release
					return &proxyservice.TrafficBodyView{ResponseBody: "late-a"}, nil
				}
				return &proxyservice.TrafficBodyView{ResponseBody: "current-b"}, nil
			}
			f.clickRow(0, "a.test")
			f.tester.Key(0, ui.KeyEnter)
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("body request did not start")
			}
			if closePanel {
				f.workspace.Table.CloseDetails()
			} else if err := f.tester.Click("b.test"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			f.settle()
			body := f.data.Traffic.BodyView
			if closePanel && body != nil || !closePanel && (body == nil || body.ResponseBody != "current-b") {
				t.Fatalf("late body published: %+v", body)
			}
		})
	}
}
