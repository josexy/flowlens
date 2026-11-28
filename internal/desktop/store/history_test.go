package store

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

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

type controlledHistory struct {
	*fakeHistory
	list   func(context.Context, string) ([]*logic.TrafficEntry, error)
	detail func(context.Context, string, uint64) (historyservice.HistoryDetail, error)
	entry  func(context.Context, string, uint64) (*proxyservice.TrafficEntry, error)
	body   func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error)
}

func (b *controlledHistory) ListHistoryEntries(ctx context.Context, key string) ([]*logic.TrafficEntry, error) {
	if b.list != nil {
		entries, err := b.list(ctx, key)
		return slices.Clone(entries), err
	}
	return b.fakeHistory.ListHistoryEntries(ctx, key)
}

func (b *controlledHistory) GetHistoryDetail(ctx context.Context, key string, id uint64) (historyservice.HistoryDetail, error) {
	if b.detail == nil {
		return b.fakeHistory.GetHistoryDetail(ctx, key, id)
	}
	return b.detail(ctx, key, id)
}

func (b *controlledHistory) GetHistoryEntry(ctx context.Context, key string, id uint64) (*proxyservice.TrafficEntry, error) {
	if b.entry != nil {
		return b.entry(ctx, key, id)
	}
	detail, err := b.GetHistoryDetail(ctx, key, id)
	return detail.Entry, err
}

func (b *controlledHistory) GetHistoryBody(ctx context.Context, key string, id uint64) (*proxyservice.TrafficBodyView, error) {
	if b.body != nil {
		return b.body(ctx, key, id)
	}
	detail, err := b.GetHistoryDetail(ctx, key, id)
	return detail.Body, err
}

func waitHistoryLoad(t *testing.T, started <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-started:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("history read did not start")
		return nil
	}
}

func TestHistoryDetailsStaySeparateFromSummaryAndFailures(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	full := trafficEntry(1)
	full.Request = &proxyservice.HTTPMessage{HeaderFields: []proxyservice.HTTPHeaderField{{Name: "Cookie", Value: "complete"}}}
	var detailErr error
	b := &controlledHistory{fakeHistory: backend, detail: func(context.Context, string, uint64) (historyservice.HistoryDetail, error) {
		if detailErr != nil {
			return historyservice.HistoryDetail{}, detailErr
		}
		return historyservice.HistoryDetail{Entry: full, Body: &proxyservice.TrafficBodyView{RequestBody: "body"}}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	summary := s.Traffic.Entries[0]
	s.Traffic.Select(summary)
	s.Traffic.SetShowDetailPanel(true)
	settle()
	if (HistoryDetailData{History: s}).SelectedEntry() != full || s.Traffic.Selected != summary || s.Traffic.Entries[0] != summary {
		t.Fatal("detail replaced summary or summary reached panel")
	}
	s.Traffic.SetShowDetailPanel(false)
	if s.Traffic.DetailEntry != nil || s.Traffic.BodyView != nil {
		t.Fatal("closed detail retained full record")
	}
	var actionEntry *logic.TrafficEntry
	s.Traffic.GetEntry(1, func(entry *logic.TrafficEntry, _ *proxyservice.TrafficBodyView) { actionEntry = entry })
	settle()
	if actionEntry != full || s.Traffic.DetailEntry != nil {
		t.Fatal("action did not resolve full entry independently")
	}
	detailErr = errors.New("unreadable history")
	s.Traffic.SetShowDetailPanel(true)
	settle()
	if s.Traffic.DetailEntry != nil || s.Traffic.BodyViewLoading {
		t.Fatal("failed read displayed a summary as complete")
	}
}

func TestHistoryReadCancellationDiscardsLateResults(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	started := make(chan context.Context, 4)
	b := &controlledHistory{fakeHistory: backend, detail: func(ctx context.Context, _ string, id uint64) (historyservice.HistoryDetail, error) {
		started <- ctx
		<-ctx.Done()
		// A backend ignoring cancellation at its final return must not publish.
		return historyservice.HistoryDetail{Entry: trafficEntry(id), Body: &proxyservice.TrafficBodyView{ResponseBody: "late"}}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	s.Traffic.Select(s.Traffic.Entries[0])
	s.Traffic.SetShowDetailPanel(true)
	first := waitHistoryLoad(t, started)
	s.Traffic.SetShowDetailPanel(true)
	s.Traffic.Select(trafficEntry(2))
	second := waitHistoryLoad(t, started)
	if first.Err() != context.Canceled {
		t.Fatal("superseded detail was not canceled")
	}
	s.CloseOpened()
	settle()
	if second.Err() != context.Canceled || s.Traffic.DetailEntry != nil || s.Traffic.BodyView != nil {
		t.Fatal("late detail restored closed history")
	}
	s.Select("new")
	settle()
	actionCalled := false
	s.Traffic.GetEntry(1, func(*logic.TrafficEntry, *proxyservice.TrafficBodyView) { actionCalled = true })
	action := waitHistoryLoad(t, started)
	s.CloseOpened()
	settle()
	if action.Err() != context.Canceled || actionCalled {
		t.Fatal("closed history left an action read running")
	}
	b.list = func(ctx context.Context, _ string) ([]*logic.TrafficEntry, error) {
		started <- ctx
		<-ctx.Done()
		return []*logic.TrafficEntry{trafficEntry(3)}, nil
	}
	s.Select("old")
	listing := waitHistoryLoad(t, started)
	s.CloseOpened()
	settle()
	if listing.Err() != context.Canceled || len(s.Traffic.Entries) != 0 {
		t.Fatal("late list restored history")
	}
}

type ownedHistoryResult struct {
	*fakeHistory
	result []*logic.TrafficEntry
}

func (b *ownedHistoryResult) ListHistoryEntries(context.Context, string) ([]*logic.TrafficEntry, error) {
	result := b.result
	b.result = nil // transfer, never reuse the returned backing array
	return result, nil
}

func TestHistoryLoadOwnsAndCompactsResult(t *testing.T) {
	first, last := trafficEntry(1), trafficEntry(2)
	for _, test := range []struct {
		name        string
		input, want []*logic.TrafficEntry
	}{
		{"nil", nil, nil},
		{"empty", []*logic.TrafficEntry{}, nil},
		{"all-nil", []*logic.TrafficEntry{nil, nil}, nil},
		{"partial", []*logic.TrafficEntry{nil, first, nil, last, nil}, []*logic.TrafficEntry{first, last}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			returned := slices.Clone(test.input)
			s.backend = &ownedHistoryResult{fakeHistory: backend, result: returned}
			s.Select("candidate")
			settle()
			if s.Traffic.Key != "candidate" || !slices.Equal(s.Traffic.Entries, test.want) {
				t.Fatalf("unexpected publication: %q %v", s.Traffic.Key, s.Traffic.Entries)
			}
			if len(test.want) != 0 && &returned[0] != &s.Traffic.Entries[0] {
				t.Fatal("successful read cloned its exclusively owned result")
			}
			for _, entry := range returned[len(test.want):] {
				if entry != nil {
					t.Fatal("compaction retained a removed tail pointer")
				}
			}
		})
	}
}

func TestHistoryBackendFixturesSurviveRepeatedLoads(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	fixture := []*logic.TrafficEntry{nil, trafficEntry(10), nil, trafficEntry(20)}
	backend.entries["candidate"] = slices.Clone(fixture)
	for range 3 {
		s.Select("candidate")
		settle()
		if !slices.Equal(backend.entries["candidate"], fixture) {
			t.Fatal("load modified fixture array")
		}
		if len(s.Traffic.Entries) != 2 {
			t.Fatal("repeated load lost rows")
		}
		s.CloseOpened()
	}
}

func TestHistoryDuplicateIdentityKeepsPreviousWithoutWrites(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	s.Select("new")
	settle()
	previous := s.Traffic.Entries[0]
	backend.entries["duplicate"] = []*logic.TrafficEntry{trafficEntry(42), nil, trafficEntry(42)}
	s.Select("duplicate")
	settle()
	if s.SelectedKey != "new" || s.Traffic.Key != "new" || s.Traffic.Entries[0] != previous || s.LoadingHistory {
		t.Fatal("duplicate candidate replaced the previous history")
	}
	if len(backend.deleted) != 0 || backend.cleared != 0 || len(backend.imports) != 0 {
		t.Fatal("invalid candidate wrote history files")
	}
	backend.entries["numeric"] = []*logic.TrafficEntry{trafficEntry(0), trafficEntry(^uint64(0))}
	s.Select("numeric")
	settle()
	if s.Traffic.Key != "numeric" || len(s.Traffic.Entries) != 2 {
		t.Fatal("identity validation imposed a new numeric ID restriction")
	}
}

func TestHistoryRapidReplacementAndShutdownRejectLateResults(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "A-B-A", true: "shutdown"}[shutdown], func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			s.Select("new")
			settle()
			previous := s.Traffic.Entries[0]
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			s.backend = &controlledHistory{fakeHistory: backend, list: func(ctx context.Context, key string) ([]*logic.TrafficEntry, error) {
				if key == "new" {
					return backend.entries[key], nil
				}
				started <- ctx
				<-release
				return []*logic.TrafficEntry{trafficEntry(99)}, nil
			}}
			s.Select("late")
			ctx := waitHistoryLoad(t, started)
			if shutdown {
				// Jobs.Close waits for workers, so let this worker finish only
				// after shutdown has canceled its context.
				go func() { <-ctx.Done(); close(release) }()
				s.rt.Close()
			} else {
				s.Select("new")
				close(release)
			}
			settle()
			if ctx.Err() != context.Canceled || s.Traffic.Key != "new" || s.Traffic.Entries[0] != previous {
				t.Fatal("obsolete load republished after replacement/shutdown")
			}
		})
	}
}

func TestHistoryBodyDemandDoesNotBlockMetadataOrDuplicateReads(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	backend.entries["new"] = append(backend.entries["new"], trafficEntry(2))
	started := make(chan context.Context, 2)
	var calls atomic.Int32
	b := &controlledHistory{fakeHistory: backend, body: func(ctx context.Context, _ string, _ uint64) (*proxyservice.TrafficBodyView, error) {
		calls.Add(1)
		started <- ctx
		<-ctx.Done()
		return &proxyservice.TrafficBodyView{ResponseBody: "obsolete body"}, nil
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	target := s.Traffic
	target.Select(target.Entries[0])
	target.SetShowDetailPanel(true)
	if (HistoryDetailData{History: s}).PendingEntry() != target.Selected {
		t.Fatal("pending selection identity missing")
	}
	settle()
	if target.DetailEntry == nil || target.BodyView != nil || calls.Load() != 0 {
		t.Fatal("metadata inspection started a body read")
	}
	target.RequestBody()
	first := waitHistoryLoad(t, started)
	for range 10 {
		target.RequestBody()
	}
	if !target.BodyViewLoading || target.DetailEntry == nil || calls.Load() != 1 {
		t.Fatal("body demand hid metadata or duplicated the read")
	}
	target.Select(target.Entries[1])
	settle()
	if first.Err() != context.Canceled || target.BodyView != nil || target.DetailEntry.ID != 2 {
		t.Fatal("selection change retained or published the previous body")
	}
	target.RequestBody()
	second := waitHistoryLoad(t, started)
	target.SetShowDetailPanel(false)
	settle()
	if second.Err() != context.Canceled || target.DetailEntry != nil || target.BodyView != nil || target.BodyViewLoading {
		t.Fatal("closing left a body read or detail alive")
	}
}

func TestHistoryFailedBodyDemandRetriesOnlyAfterReopen(t *testing.T) {
	s, backend, settle := newHistoryHarness(t)
	var calls atomic.Int32
	b := &controlledHistory{fakeHistory: backend, body: func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error) {
		calls.Add(1)
		return nil, errors.New("body unavailable")
	}}
	s.backend, s.Traffic.backend = b, b
	s.Select("new")
	settle()
	target := s.Traffic
	target.Select(target.Entries[0])
	target.SetShowDetailPanel(true)
	// Demand may arrive before metadata; it starts once that stage completes.
	target.RequestBody()
	settle()
	if target.DetailEntry == nil || target.BodyView != nil || target.BodyViewLoading || calls.Load() != 1 {
		t.Fatal("body failure discarded headers or failed to settle")
	}
	for range 5 {
		target.RequestBody()
		settle()
	}
	if calls.Load() != 1 {
		t.Fatal("redraw demand retried a failed body")
	}
	target.SetShowDetailPanel(false)
	target.SetShowDetailPanel(true)
	target.RequestBody()
	settle()
	if calls.Load() != 2 {
		t.Fatal("reopening did not permit a fresh body read")
	}
}

func TestHistoryTCPAndInvalidMetadataNeverRequestBodies(t *testing.T) {
	for _, kind := range []string{"tcp", "missing", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			s, backend, settle := newHistoryHarness(t)
			var calls atomic.Int32
			b := &controlledHistory{fakeHistory: backend,
				entry: func(context.Context, string, uint64) (*proxyservice.TrafficEntry, error) {
					switch kind {
					case "missing":
						return nil, nil
					case "mismatch":
						return trafficEntry(999), nil
					default:
						return &logic.TrafficEntry{ID: 1, Type: "tcp"}, nil
					}
				},
				body: func(context.Context, string, uint64) (*proxyservice.TrafficBodyView, error) {
					calls.Add(1)
					return &proxyservice.TrafficBodyView{}, nil
				},
			}
			s.backend, s.Traffic.backend = b, b
			s.Select("new")
			settle()
			if kind == "tcp" {
				s.Traffic.Entries[0].Type = "tcp"
			}
			s.Traffic.Select(s.Traffic.Entries[0])
			s.Traffic.SetShowDetailPanel(true)
			s.Traffic.RequestBody()
			settle()
			if calls.Load() != 0 || s.Traffic.DetailLoading || s.Traffic.BodyViewLoading {
				t.Fatal("unexpected body request or unfinished load")
			}
			if kind != "tcp" && s.Traffic.DetailEntry != nil {
				t.Fatal("invalid metadata reached the panel")
			}
		})
	}
}
