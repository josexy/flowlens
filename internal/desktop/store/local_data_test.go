package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

const testDraftCacheRoot = "/data/flowlens/request-draft-cache"

type fakeStorage struct {
	result proxyservice.LocalDataClearResult
	err    error
	calls  []proxyservice.LocalDataClearScope
	// before runs on the job goroutine before the result is returned.
	before func()
}

func (f *fakeStorage) clear(scope proxyservice.LocalDataClearScope) (proxyservice.LocalDataClearResult, error) {
	f.calls = append(f.calls, scope)
	if f.before != nil {
		f.before()
	}
	result := f.result
	result.Scope = scope
	return result, f.err
}

func (f *fakeStorage) ClearCacheFiles() (proxyservice.LocalDataClearResult, error) {
	return f.clear(proxyservice.LocalDataScopeCache)
}

func (f *fakeStorage) ClearCacheAndHistory() (proxyservice.LocalDataClearResult, error) {
	return f.clear(proxyservice.LocalDataScopeCacheAndHistory)
}

type gatedHistory struct {
	*fakeHistory
	gate    chan struct{}
	started chan struct{}
}

func (g *gatedHistory) ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error) {
	list := slices.Clone(g.list)
	if g.gate != nil {
		g.started <- struct{}{}
		<-g.gate
	}
	return list, nil
}

type localDataHarness struct {
	data    *Store
	storage *fakeStorage
	history *gatedHistory
	window  *runtimetest.Window
	icons   int
}

func newLocalDataHarness(t *testing.T) *localDataHarness {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	h := &localDataHarness{
		storage: &fakeStorage{result: proxyservice.LocalDataClearResult{Cleared: true, RequestDraftCacheRoot: testDraftCacheRoot}},
		history: &gatedHistory{fakeHistory: &fakeHistory{
			list:    []*proxyservice.HistoryMetadata{{Key: "old", CreatedAt: 1}, {Key: "new", CreatedAt: 5}},
			entries: map[string][]*logic.TrafficEntry{"new": {trafficEntry(1)}, "old": {trafficEntry(2)}},
		}},
		window: window,
	}
	h.data = New(rt, Dependencies{Storage: h.storage, History: h.history, Requests: &stubRequests{}})
	h.data.ClearProcessIcons = func() { h.icons++ }
	return h
}

func (h *localDataHarness) settle() {
	for range 6 {
		h.data.Runtime.Jobs.Wait()
		h.window.Drain()
	}
}

func (h *localDataHarness) openHistory(key string) {
	h.data.History.LoadList()
	h.settle()
	h.data.History.Select(key)
	h.settle()
	h.data.Workbench.SelectCaptureItem(CaptureItemHistoryPrefix + key)
}

func (h *localDataHarness) clear(scope proxyservice.LocalDataClearScope) error {
	var result error
	called := false
	h.data.ClearLocalData(scope, func(err error) { called, result = true, err })
	h.settle()
	if !called {
		panic("done was not called")
	}
	return result
}

func TestClearLocalDataCacheOnlyReloadsListAndKeepsOpenHistory(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("new")
	h.history.list = append(h.history.list, &proxyservice.HistoryMetadata{Key: "archived", CreatedAt: 9})

	if err := h.clear(proxyservice.LocalDataScopeCache); err != nil {
		t.Fatalf("clear: %v", err)
	}
	history := h.data.History
	if len(history.Metadata) != 3 || history.Metadata[0].Key != "archived" {
		t.Fatalf("list = %+v, want the archived capture first", history.Metadata)
	}
	if history.SelectedKey != "new" || history.Traffic.Key != "new" {
		t.Fatalf("open history = %q/%q, want new", history.SelectedKey, history.Traffic.Key)
	}
	if h.data.Workbench.CaptureSelection != CaptureItemHistoryPrefix+"new" {
		t.Fatalf("capture selection = %q", h.data.Workbench.CaptureSelection)
	}
	if got := h.storage.calls; len(got) != 1 || got[0] != proxyservice.LocalDataScopeCache {
		t.Fatalf("storage calls = %v", got)
	}
}

func TestClearLocalDataCacheOnlyClosesOpenHistoryThatIsGone(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("old")
	h.history.list = h.history.list[1:]

	if err := h.clear(proxyservice.LocalDataScopeCache); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if history := h.data.History; history.SelectedKey != "" || history.Traffic.Key != "" {
		t.Fatalf("open history = %q/%q, want closed", history.SelectedKey, history.Traffic.Key)
	}
}

func TestClearLocalDataHistoryClearedEmptiesListAndShowsLiveCapture(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("new")
	h.data.Workbench.ShowSection(WorkspaceRewriteRules)
	listVersion := h.data.History.ListVersion
	h.storage.result.HistoryCleared = true

	if err := h.clear(proxyservice.LocalDataScopeCacheAndHistory); err != nil {
		t.Fatalf("clear: %v", err)
	}
	history := h.data.History
	if len(history.Metadata) != 0 || history.Loading || history.ListVersion == listVersion {
		t.Fatalf("list = %+v loading=%t version=%d, want an empty new list", history.Metadata, history.Loading, history.ListVersion)
	}
	if history.SelectedKey != "" || history.Traffic.Key != "" || len(history.Traffic.Entries) != 0 {
		t.Fatalf("open history = %q/%q, want closed", history.SelectedKey, history.Traffic.Key)
	}
	workbench := h.data.Workbench
	if workbench.CaptureSelection != CaptureItemLive || workbench.ActiveSection != WorkspaceRewriteRules {
		t.Fatalf("workbench = %q in %q, want the live capture without leaving the section", workbench.CaptureSelection, workbench.ActiveSection)
	}
}

func TestClearLocalDataHistoryClearedCancelsListLoadInProgress(t *testing.T) {
	h := newLocalDataHarness(t)
	h.history.gate, h.history.started = make(chan struct{}), make(chan struct{}, 1)
	h.data.History.LoadList()
	<-h.history.started
	h.storage.result.HistoryCleared = true

	done := make(chan error, 1)
	h.data.ClearLocalData(proxyservice.LocalDataScopeCacheAndHistory, func(err error) { done <- err })
	deadline := time.After(5 * time.Second)
	for len(done) == 0 {
		select {
		case <-deadline:
			t.Fatal("clear did not finish")
		default:
		}
		h.window.Drain()
		time.Sleep(time.Millisecond)
	}
	close(h.history.gate)
	h.settle()
	if history := h.data.History; len(history.Metadata) != 0 || history.Loading {
		t.Fatalf("list = %+v loading=%t, want the stale load discarded", history.Metadata, history.Loading)
	}
}

func TestClearLocalDataDropsOnlyDraftCacheFiles(t *testing.T) {
	h := newLocalDataHarness(t)
	workspace := h.data.Workspace
	key := workspace.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := workspace.FindTab(key).HTTP
	state.RequestBodyType, state.RequestBodyText = logic.BodyTypeFile, "kept text"
	state.RequestBodyFile = &logic.RequestFileValue{Path: testDraftCacheRoot + "/recovered/body.bin"}
	state.RequestBodyFormData = []logic.FormDataItem{
		{ID: "cached", Enabled: true, ItemType: "file", File: &logic.RequestFileValue{Path: testDraftCacheRoot + "/multipart/a.bin"}},
		{ID: "outside", Enabled: true, ItemType: "file", File: &logic.RequestFileValue{Path: "/home/user/upload.bin"}},
	}

	if err := h.clear(proxyservice.LocalDataScopeCache); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if state.RequestBodyFile != nil || state.RequestBodyFormData[0].File != nil {
		t.Fatalf("draft cache files kept: %+v %+v", state.RequestBodyFile, state.RequestBodyFormData[0].File)
	}
	if state.RequestBodyFormData[1].File == nil || state.RequestBodyText != "kept text" || workspace.FindTab(key) == nil {
		t.Fatal("files outside the draft cache or the draft itself were dropped")
	}
}

func TestClearLocalDataClearsProcessIcons(t *testing.T) {
	h := newLocalDataHarness(t)
	if err := h.clear(proxyservice.LocalDataScopeCacheAndHistory); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if h.icons != 1 {
		t.Fatalf("icon clears = %d, want 1", h.icons)
	}
}

func TestClearLocalDataAppliesPartialFailureBeforeReportingIt(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("new")
	h.storage.err = errors.New("remove cache directory: busy")
	h.storage.result.HistoryCleared = true
	var order []string
	h.data.ClearProcessIcons = func() { order = append(order, "icons") }
	var reported error
	h.data.ClearLocalData(proxyservice.LocalDataScopeCacheAndHistory, func(err error) {
		order = append(order, "done")
		reported = err
		if h.data.History.SelectedKey != "" || len(h.data.History.Metadata) != 0 {
			t.Error("history was not refreshed before the error was reported")
		}
	})
	h.settle()
	if !errors.Is(reported, h.storage.err) || !slices.Equal(order, []string{"icons", "done"}) {
		t.Fatalf("reported %v in order %v", reported, order)
	}
}

func TestClearLocalDataWithNothingClearedOnlyReportsError(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("new")
	h.storage.result = proxyservice.LocalDataClearResult{}
	h.storage.err = errors.New("get base storage directory: missing")
	if err := h.clear(proxyservice.LocalDataScopeCacheAndHistory); !errors.Is(err, h.storage.err) {
		t.Fatalf("clear error = %v", err)
	}
	if h.icons != 0 || h.data.History.SelectedKey != "new" || len(h.data.History.Metadata) != 2 {
		t.Fatal("nothing was removed, but the interface was refreshed")
	}
}

func TestClearLocalDataAppliesAfterSettingsWindowCloses(t *testing.T) {
	h := newLocalDataHarness(t)
	h.openHistory("new")
	settings := &runtimetest.Window{}
	h.data.Runtime.Dispatcher.Register("settings", settings)
	h.storage.result.HistoryCleared = true
	h.storage.before = func() {
		// The Settings window closes while the cleanup runs.
		h.data.Runtime.Dispatcher.Unregister("settings")
		h.data.Runtime.Jobs.CancelPrefix("settings:")
	}
	if err := h.clear(proxyservice.LocalDataScopeCacheAndHistory); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if h.icons != 1 || h.data.History.SelectedKey != "" || len(h.data.History.Metadata) != 0 {
		t.Fatal("the cleanup was not applied after the Settings window closed")
	}
}
