package capture

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func TestCaptureResetReleasesHiddenViewsBeforeAnotherFrame(t *testing.T) {
	f := newFixture(t, entry(1, "old.test"), entry(2, "other.test"))
	f.showCategories = true
	f.settle()
	if len(f.workspace.category.entries) == 0 || len(f.workspace.Table.rows) == 0 {
		t.Fatal("views were not populated")
	}
	f.showCategories = false
	f.settle()
	f.workspace.category.expanded["old.test/old/path"] = true
	f.data.Traffic.Select(f.data.Traffic.Entries[0])
	f.data.Traffic.SetShowDetailPanel(true)
	f.settle()
	var released int
	f.workspace.ReleaseDetail = func() { released++ }
	f.data.Traffic.ResetState()
	if released != 1 || len(f.workspace.Table.rows) != 0 || f.workspace.Table.state.Len() != 0 {
		t.Fatal("reset retained a table or failed to release its hidden detail")
	}
	view := &f.workspace.category
	if view.entries != nil || view.roots != nil || view.treeItems != nil || view.visible != nil || len(view.expanded) != 0 {
		t.Fatal("hidden categories kept the discarded capture")
	}
	if len(f.data.Filter.Filtered()) != 0 {
		t.Fatal("reset filter returned old entries")
	}
	f.settle()
	if released != 1 {
		t.Fatal("empty frames repeatedly reset the detail")
	}
	f.data.Traffic.AddOrUpdate(entry(1, "new.test"))
	f.data.Traffic.FlushPending()
	f.settle()
	if rows := f.workspace.Table.rows; len(rows) != 1 || rows[0].Host != "new.test" {
		t.Fatal("next session could not reuse an entry ID")
	}
}

func TestDeletingSelectedCaptureReleasesDetailBeforeAnotherFrame(t *testing.T) {
	f := newFixture(t, entry(1, "old.test"), entry(2, "kept.test"))
	f.data.Traffic.Select(f.data.Traffic.Entries[0])
	f.data.Traffic.SetShowDetailPanel(true)
	f.settle()
	var released int
	f.workspace.ReleaseDetail = func() { released++ }
	f.data.Traffic.Delete([]uint64{1})
	if released != 1 {
		t.Fatal("deleting selected capture retained details")
	}
	f.settle()
	if rows := f.workspace.Table.rows; len(rows) != 1 || rows[0].Host != "kept.test" {
		t.Fatal("delete affected other traffic")
	}
}

func TestHistoryResetReleasesHiddenViewsBeforeAnotherFrame(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "live.test"))
	f.click("session one")
	f.showCategories = true
	f.settle()
	if len(f.workspace.category.entries) == 0 || len(f.workspace.HistoryTable.rows) == 0 {
		t.Fatal("history views were not populated")
	}
	var released int
	f.workspace.ReleaseHistoryDetail = func() { released++ }
	f.data.History.CloseOpened()
	if released != 1 || len(f.workspace.HistoryTable.rows) != 0 || f.workspace.category.entries != nil {
		t.Fatal("closing history retained hidden view state")
	}
	if len(f.data.Traffic.Entries) != 1 {
		t.Fatal("history reset affected live capture")
	}
	f.data.Workspace.CloseTab(logic.HistoryTabKey)
	f.settle()
}

func TestRepeatedHistoryCloseReleasesRowsAndDrawing(t *testing.T) {
	f := newHistoryFixture(t, entry(1, "live.test"))
	panel := inspect.NewPanel(store.HistoryDetailData{History: f.data.History}, inspect.Dependencies{
		Runtime: f.data.Runtime, Settings: f.data.Settings, Notifier: f.notifier,
	})
	f.workspace.HistoryDetail, f.workspace.ReleaseHistoryDetail = panel.Build, panel.Release
	for cycle := range 10 {
		oldRow, oldBody := func() (weak.Pointer[logic.TrafficEntry], weak.Pointer[proxyservice.TrafficBodyView]) {
			record := entry(11, fmt.Sprintf("cycle%d.test", cycle))
			record.Response = &proxyservice.HTTPMessage{HeaderFields: []proxyservice.HTTPHeaderField{{Name: "Content-Type", Value: "text/plain"}}}
			body := &proxyservice.TrafficBodyView{ResponseBody: strings.Repeat("saved body line\n", 65536)}
			f.history.entries["k1"] = []*logic.TrafficEntry{record}
			f.history.details = map[uint64]historyservice.HistoryDetail{11: {Entry: record, Body: body}}
			f.click("session one")
			f.workspace.HistoryTable.source.SetHighlight(11, "#ef4444")
			f.openDetails(record.Host)
			f.click(locales.DetailResponseBody())
			if f.data.History.Traffic.BodyView == nil || f.data.History.Traffic.BodyView.ResponseBody != body.ResponseBody {
				t.Fatal("history body was not viewed")
			}
			rowRef, bodyRef := weak.Make(record), weak.Make(body)
			f.data.Workspace.CloseTab(logic.HistoryTabKey)
			f.settle()
			// The fake disk source must not itself own the discarded objects.
			f.history.entries["k1"], f.history.details = nil, nil
			return rowRef, bodyRef
		}()
		f.tester.Advance(time.Second) // Let removed detail/Router transitions finish.
		for range 3 {
			f.settle()
		}
		runtime.GC()
		runtime.GC()
		if oldRow.Value() != nil || oldBody.Value() != nil {
			t.Fatalf("cycle %d: closed history retained row=%t body=%t through UI callbacks", cycle+1, oldRow.Value() != nil, oldBody.Value() != nil)
		}
		if len(f.workspace.HistoryTable.rows) != 0 || f.workspace.HistoryTable.state.Len() != 0 {
			t.Fatalf("cycle %d: closed table still owns rows", cycle+1)
		}
	}
	runtime.KeepAlive(f)
}
