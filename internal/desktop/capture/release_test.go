package capture

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
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
