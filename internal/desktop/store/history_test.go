package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

type fakeHistory struct {
	list     []*proxyservice.HistoryMetadata
	entries  map[string][]*logic.TrafficEntry
	getErr   error
	deleted  []string
	cleared  int
	imports  []string
	failures map[string]error
	empty    map[string]bool
}

func (f *fakeHistory) ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error) {
	return slices.Clone(f.list), nil
}
func (f *fakeHistory) ListHistoryEntries(_ context.Context, key string) ([]*logic.TrafficEntry, error) {
	return slices.Clone(f.entries[key]), f.getErr
}
func (f *fakeHistory) GetHistoryDetail(_ context.Context, key string, id uint64) (historyservice.HistoryDetail, error) {
	for _, entry := range f.entries[key] {
		if entry != nil && entry.ID == id {
			return historyservice.HistoryDetail{Entry: entry, Body: &proxyservice.TrafficBodyView{ResponseBody: "stored"}}, nil
		}
	}
	return historyservice.HistoryDetail{}, errors.New("missing history entry")
}
func (f *fakeHistory) GetHistoryEntry(ctx context.Context, key string, id uint64) (*proxyservice.TrafficEntry, error) {
	detail, err := f.GetHistoryDetail(ctx, key, id)
	return detail.Entry, err
}
func (f *fakeHistory) GetHistoryBody(ctx context.Context, key string, id uint64) (*proxyservice.TrafficBodyView, error) {
	detail, err := f.GetHistoryDetail(ctx, key, id)
	return detail.Body, err
}
func (f *fakeHistory) DeleteHistory(key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}
func (f *fakeHistory) ClearHistories() error {
	f.cleared++
	return nil
}
func (f *fakeHistory) ImportHAR(_ context.Context, request historyservice.HARImportRequest) (proxyservice.HARImportResult, error) {
	f.imports = append(f.imports, request.Path)
	if err := f.failures[request.Path]; err != nil {
		return proxyservice.HARImportResult{}, err
	}
	if f.empty[request.Path] {
		return proxyservice.HARImportResult{}, nil
	}
	return proxyservice.HARImportResult{Imported: 2, Skipped: 1, MissingBodies: 1, Metadata: &proxyservice.HistoryMetadata{Key: request.Path}}, nil
}

func newHistoryHarness(t *testing.T) (*HistoryStore, *fakeHistory, func()) {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	backend := &fakeHistory{
		list:    []*proxyservice.HistoryMetadata{{Key: "old", CreatedAt: 1}, nil, {Key: "new", CreatedAt: 5}},
		entries: map[string][]*logic.TrafficEntry{"new": {trafficEntry(1), nil}}, failures: map[string]error{}, empty: map[string]bool{},
	}
	settle := func() {
		for range 6 {
			rt.Jobs.Wait()
			window.Drain()
		}
	}
	return NewHistoryStore(rt, backend), backend, settle
}

func TestHistoryListIsNewestFirst(t *testing.T) {
	store, _, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	if len(store.Metadata) != 2 || store.Metadata[0].Key != "new" || store.Loading {
		t.Fatalf("%+v", store.Metadata)
	}
}

func TestHistorySelectLoadsAndKeepsPreviousOnFailure(t *testing.T) {
	store, backend, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	store.Select("new")
	settle()
	if store.Traffic.Key != "new" || len(store.Traffic.Entries) != 1 || store.LoadingHistory {
		t.Fatalf("%+v", store.Traffic)
	}
	backend.getErr = errors.New("broken file")
	store.Select("old")
	settle()
	if store.SelectedKey != "new" || store.Traffic.Key != "new" {
		t.Fatalf("selected %q", store.SelectedKey)
	}
}

func TestHistoryDeleteClearsSelection(t *testing.T) {
	store, backend, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	store.Select("new")
	settle()
	var result error
	store.Delete("new", func(err error) { result = err })
	settle()
	if result != nil || store.SelectedKey != "" || store.Traffic.Key != "" || len(store.Metadata) != 1 || backend.deleted[0] != "new" {
		t.Fatalf("%q %+v", store.SelectedKey, store.Metadata)
	}
}

func TestHistoryClearAll(t *testing.T) {
	store, backend, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	store.ClearAll(nil)
	settle()
	if backend.cleared != 1 || len(store.Metadata) != 0 {
		t.Fatal("clear")
	}
}

func TestHistoryRemovedFromDiskClosesSelection(t *testing.T) {
	store, backend, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	store.Select("new")
	settle()
	backend.list = backend.list[:1]
	store.LoadList()
	settle()
	if store.SelectedKey != "" {
		t.Fatal(store.SelectedKey)
	}
}

func TestHistoryBodyLoadsOnlyWithPanelOpen(t *testing.T) {
	store, _, settle := newHistoryHarness(t)
	store.Select("new")
	settle()
	traffic := store.Traffic
	traffic.Select(traffic.Entries[0])
	settle()
	if traffic.BodyView != nil {
		t.Fatal("loaded without the panel")
	}
	traffic.SetShowDetailPanel(true)
	settle()
	if traffic.DetailEntry == nil || traffic.BodyView != nil {
		t.Fatal("opening details must load only metadata")
	}
	traffic.RequestBody()
	settle()
	if traffic.BodyView == nil || traffic.BodyView.ResponseBody != "stored" {
		t.Fatalf("%+v", traffic.BodyView)
	}
	traffic.Delete([]uint64{1})
	if traffic.Selected != nil || traffic.ShowDetailPanel || traffic.BodyView != nil || len(traffic.Entries) != 0 {
		t.Fatal("delete")
	}
}

func TestImportRunsSequentiallyAndSummarizes(t *testing.T) {
	store, backend, settle := newHistoryHarness(t)
	backend.failures["/b.har"] = errors.New("corrupt")
	backend.empty["/c.har"] = true
	var summary HARImportSummary
	if !store.ImportHAR([]string{"/a.har", "/b.har", "/a.har", "/c.har"}, false, func(result HARImportSummary) { summary = result }) {
		t.Fatal("start")
	}
	if !store.Importing || store.TotalFiles != 3 {
		t.Fatalf("%v %d", store.Importing, store.TotalFiles)
	}
	if store.ImportHAR([]string{"/x"}, false, nil) {
		t.Fatal("overlap")
	}
	settle()
	if !slices.Equal(backend.imports, []string{"/a.har", "/b.har", "/c.har"}) {
		t.Fatal(backend.imports)
	}
	if summary.Files != 1 || summary.Imported != 2 || summary.Skipped != 1 || summary.Missing != 1 || summary.Failed != 2 || summary.FirstFailedFile != "b.har" || summary.First.Key != "/a.har" {
		t.Fatalf("%+v", summary)
	}
	if store.Importing || store.TotalFiles != 0 {
		t.Fatal("finished")
	}
}

func TestImportWithNothingDoesNothing(t *testing.T) {
	store, _, _ := newHistoryHarness(t)
	if store.ImportHAR(nil, false, nil) || store.Importing {
		t.Fatal("empty")
	}
}

func TestCloseOpenedReleasesTheHistory(t *testing.T) {
	store, _, settle := newHistoryHarness(t)
	store.LoadList()
	settle()
	store.Select("new")
	settle()
	store.Filter.SearchText = "example"
	store.Traffic.Select(store.Traffic.Entries[0])
	store.Traffic.SetShowDetailPanel(true)
	store.Traffic.RequestBody()
	store.Traffic.SetHighlight(1, "red")
	settle()
	if len(store.Filter.Base()) == 0 || store.Traffic.BodyView == nil {
		t.Fatal("history was not opened")
	}

	store.CloseOpened()
	if store.Traffic.Key != "" || store.Traffic.Entries != nil || store.Traffic.Selected != nil ||
		store.Traffic.DetailEntry != nil || store.Traffic.DetailLoading || store.Traffic.BodyViewLoading ||
		store.Traffic.BodyView != nil || len(store.Traffic.Highlights) != 0 || store.SelectedKey != "" {
		t.Fatalf("opened history kept after close: %+v", store.Traffic)
	}
	if store.Filter.base != nil || store.Filter.filtered != nil ||
		len(store.Filter.matches) != 0 {
		t.Fatal("filter still references the closed history's entries")
	}
	if len(store.Filter.Base()) != 0 || len(store.Metadata) != 2 {
		t.Fatalf("base %d metadata %d", len(store.Filter.Base()), len(store.Metadata))
	}

	store.Select("new")
	settle()
	requireIDs(t, store.Traffic.Entries, []uint64{1})
}

func TestCloseOpenedDropsALoadInProgress(t *testing.T) {
	store, _, settle := newHistoryHarness(t)
	store.Select("new")
	store.CloseOpened()
	settle()
	if store.Traffic.Key != "" || store.Traffic.Entries != nil || store.LoadingHistory {
		t.Fatalf("a canceled load opened the history: %+v", store.Traffic)
	}
}
