package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/pkg/logger"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// HistoryDetailTransitionDelay bounds retention of the previous inspection view.
const HistoryDetailTransitionDelay = 180 * time.Millisecond

const (
	jobHistoryList   = "history:list"
	jobHistoryLoad   = "history:load"
	jobHistoryDetail = "history:detail"
	jobHistoryBody   = "history:body"
	jobHistoryAction = "history:action:"
	jobHistoryDelete = "history:delete"
	jobHistoryClear  = "history:clear"
	jobHistoryImport = "history:import"
)

// HistoryBackend is the history service as the stores use it.
type HistoryBackend interface {
	ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error)
	// ListHistoryEntries transfers exclusive ownership of its result backing
	// array on success. The backend retains no mutable access to the array;
	// published summary records are immutable.
	ListHistoryEntries(ctx context.Context, key string) ([]*logic.TrafficEntry, error)
	GetHistoryEntry(ctx context.Context, key string, id uint64) (*proxyservice.TrafficEntry, error)
	GetHistoryBody(ctx context.Context, key string, id uint64) (*proxyservice.TrafficBodyView, error)
	GetHistoryDetail(ctx context.Context, key string, id uint64) (historyservice.HistoryDetail, error)
	DeleteHistory(key string) error
	ClearHistories() error
	ImportHAR(ctx context.Context, request historyservice.HARImportRequest) (proxyservice.HARImportResult, error)
}

// HistoryTraffic holds the entries of the opened history.
type HistoryTraffic struct {
	rt      Runtime
	backend HistoryBackend

	Key             string
	Entries         []*logic.TrafficEntry
	EntriesVersion  uint64
	Selected        *logic.TrafficEntry
	DetailEntry     *logic.TrafficEntry
	DetailLoading   bool
	SelectedCount   int
	BodyView        *proxyservice.TrafficBodyView
	BodyViewVersion uint64
	BodyViewLoading bool
	ShowDetailPanel bool
	Highlights      map[uint64]string
	PendingFocusID  uint64
	// OnReset releases views of the previous history without waiting for a frame.
	OnReset func()
	// OnDetailReset releases caches when the detail body or selection is invalidated.
	OnDetailReset func()

	bodyToken     uint64
	bodySequence  uint64
	bodyRequested bool
	bodyAttempted bool
	clock         Clock
	retained      *HistoryDetailSnapshot
	stopRetention func()
}

func (t *HistoryTraffic) setBodyView(view *proxyservice.TrafficBodyView) {
	t.BodyView = view
	t.BodyViewVersion++
}

func (t *HistoryTraffic) clearBody() {
	t.stopRetaining()
	t.resetBody()
}

func (t *HistoryTraffic) resetBody() {
	if t.rt.Jobs != nil {
		t.rt.Jobs.Cancel(jobHistoryDetail)
		t.rt.Jobs.Cancel(jobHistoryBody)
	}
	t.DetailEntry = nil
	t.DetailLoading = false
	t.bodyRequested, t.bodyAttempted = false, false
	t.bodyToken++
	if t.BodyView != nil {
		t.setBodyView(nil)
	}
	t.BodyViewLoading = false
	if t.retained == nil && t.OnDetailReset != nil {
		t.OnDetailReset()
	}
}

func (t *HistoryTraffic) stopRetaining() {
	if t.stopRetention != nil {
		t.stopRetention()
		t.stopRetention = nil
	}
	t.retained = nil
}

func (t *HistoryTraffic) releaseRetained() {
	if t.retained == nil {
		return
	}
	t.stopRetaining()
	if t.OnDetailReset != nil {
		t.OnDetailReset()
	}
}

func (t *HistoryTraffic) retainDetail() {
	if t.retained != nil || t.DetailEntry == nil || !t.ShowDetailPanel {
		return
	}
	t.retained = &HistoryDetailSnapshot{Entry: t.DetailEntry, Body: t.BodyView, Version: t.BodyViewVersion, Loading: t.BodyViewLoading}
	if t.clock == nil {
		t.clock = dispatcherClock{dispatcher: t.rt.Dispatcher}
	}
	retained := t.retained
	t.stopRetention = t.clock.After(HistoryDetailTransitionDelay, func() {
		if t.retained == retained {
			t.releaseRetained()
		}
	})
}

// Select makes entry the selected one; nil closes the detail panel.
func (t *HistoryTraffic) Select(entry *logic.TrafficEntry) {
	var previous uint64
	if t.Selected != nil {
		previous = t.Selected.ID
	}
	t.Selected = entry
	if entry == nil {
		t.clearBody()
		t.ShowDetailPanel = false
		return
	}
	if previous != entry.ID {
		t.retainDetail()
		t.resetBody()
	}
	t.loadDetail()
}

// SetShowDetailPanel opens or closes the detail panel.
func (t *HistoryTraffic) SetShowDetailPanel(show bool) {
	t.ShowDetailPanel = show
	if !show {
		t.clearBody()
		return
	}
	t.loadDetail()
}

func (t *HistoryTraffic) loadDetail() {
	entry := t.Selected
	if entry == nil || !t.ShowDetailPanel || t.Key == "" {
		return
	}
	if t.DetailEntry != nil || t.DetailLoading {
		return
	}
	t.bodyToken++
	token, key, id := t.bodyToken, t.Key, entry.ID
	t.DetailLoading = true
	runtime.Run(t.rt.Jobs, jobHistoryDetail, func(ctx context.Context) (*proxyservice.TrafficEntry, error) {
		return t.backend.GetHistoryEntry(ctx, key, id)
	}, func(detail *proxyservice.TrafficEntry, err error) {
		if token != t.bodyToken {
			return
		}
		t.DetailLoading = false
		if err == nil && (detail == nil || detail.ID != id) {
			err = fmt.Errorf("history detail %d was not returned", id)
		}
		if err != nil {
			logger.G().Errorf("Failed to get history traffic entry: %v", err)
			t.releaseRetained()
			return
		}
		t.DetailEntry = detail
		if t.bodyRequested {
			t.RequestBody()
		}
	})
}

// RequestBody is a guarded demand signal from a body-dependent inspection pane.
// Redraws neither duplicate an in-flight request nor retry a failed read.
func (t *HistoryTraffic) RequestBody() {
	if t.Selected == nil || !t.ShowDetailPanel || t.Key == "" || t.Selected.Type == "tcp" {
		return
	}
	t.bodyRequested = true
	if t.DetailEntry == nil || t.BodyView != nil || t.bodyAttempted {
		return
	}
	t.bodyAttempted, t.BodyViewLoading = true, true
	token, key, id := t.bodyToken, t.Key, t.Selected.ID
	runtime.Run(t.rt.Jobs, jobHistoryBody, func(ctx context.Context) (*proxyservice.TrafficBodyView, error) {
		return t.backend.GetHistoryBody(ctx, key, id)
	}, func(body *proxyservice.TrafficBodyView, err error) {
		if token != t.bodyToken {
			return
		}
		t.BodyViewLoading = false
		if err == nil && body == nil {
			err = fmt.Errorf("history body %d was not returned", id)
		}
		if err != nil {
			logger.G().Errorf("Failed to get history traffic body view: %v", err)
			t.releaseRetained()
			return
		}
		t.setBodyView(body)
	})
}

// GetEntry loads complete action inputs without adding them to the row list.
func (t *HistoryTraffic) GetEntry(id uint64, done func(*logic.TrafficEntry, *proxyservice.TrafficBodyView)) {
	if t.Key == "" {
		done(nil, nil)
		return
	}
	key := t.Key
	t.bodySequence++
	runtime.Run(t.rt.Jobs, fmt.Sprintf("%s%d", jobHistoryAction, t.bodySequence), func(ctx context.Context) (historyservice.HistoryDetail, error) {
		return t.backend.GetHistoryDetail(ctx, key, id)
	}, func(detail historyservice.HistoryDetail, err error) {
		if err != nil {
			logger.G().Errorf("Failed to get history traffic body view: %v", err)
			done(nil, nil)
			return
		}
		done(detail.Entry, detail.Body)
	})
}

// Load replaces the entries.
func (t *HistoryTraffic) Load(key string, entries []*logic.TrafficEntry) {
	t.Reset()
	t.Key = key
	t.Entries = entries
	t.EntriesVersion++
}

// Reset empties the store.
func (t *HistoryTraffic) Reset() {
	if t.rt.Jobs != nil {
		t.rt.Jobs.CancelPrefix(jobHistoryAction)
	}
	t.clearBody()
	t.Key, t.Entries, t.Selected, t.SelectedCount, t.ShowDetailPanel, t.PendingFocusID = "", nil, nil, 0, false, 0
	t.EntriesVersion++
	t.Highlights = map[uint64]string{}
	if t.OnReset != nil {
		t.OnReset()
	}
}

// Delete removes entries from the opened history's view. The file itself is
// unchanged.
func (t *HistoryTraffic) Delete(ids []uint64) {
	remove := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		remove[id] = true
		delete(t.Highlights, id)
	}
	t.Entries = slices.DeleteFunc(slices.Clone(t.Entries), func(entry *logic.TrafficEntry) bool { return remove[entry.ID] })
	t.EntriesVersion++
	if t.Selected != nil && remove[t.Selected.ID] {
		t.Selected = nil
		t.clearBody()
		t.ShowDetailPanel = false
	}
}

// SetHighlight colors a row; an empty color removes the highlight.
func (t *HistoryTraffic) SetHighlight(id uint64, color string) {
	if color != "" {
		t.Highlights[id] = color
		return
	}
	delete(t.Highlights, id)
}

// HARImportSummary reports a finished import.
type HARImportSummary struct {
	Files, Imported, Skipped, Missing, Failed int
	FirstFailedFile, FirstFailure             string
	// NoEntries tells that the first failure was a file with nothing to import.
	NoEntries bool
	// First is the first history created.
	First *proxyservice.HistoryMetadata
}

// HistoryStore lists saved histories and holds the opened one.
type HistoryStore struct {
	rt      Runtime
	backend HistoryBackend

	Metadata []*proxyservice.HistoryMetadata
	// ListVersion changes whenever Metadata is replaced.
	ListVersion       uint64
	SelectedKey       string
	Loading           bool
	LoadingHistory    bool
	LoadingHistoryKey string
	Importing         bool
	CurrentFile       int
	TotalFiles        int

	Traffic *HistoryTraffic
	Filter  *FilterStore
}

// NewHistoryStore returns an empty store.
func NewHistoryStore(rt Runtime, backend HistoryBackend) *HistoryStore {
	traffic := &HistoryTraffic{rt: rt, backend: backend, Highlights: map[uint64]string{}}
	return &HistoryStore{
		rt: rt, backend: backend, Traffic: traffic,
		Filter: NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return traffic.Entries, traffic.EntriesVersion }, logic.TrafficSnapshot),
	}
}

func (s *HistoryStore) clearSelection() {
	s.rt.Jobs.Cancel(jobHistoryLoad)
	s.SelectedKey, s.LoadingHistory, s.LoadingHistoryKey = "", false, ""
	s.Traffic.Reset()
	s.Filter.ClearFilters()
	s.Filter.Release()
}

// CloseOpened releases the opened history: a load in progress, its entries,
// details, highlights, and filter results. The list of saved histories stays.
func (s *HistoryStore) CloseOpened() {
	s.clearSelection()
}

// LoadList refreshes the list, newest first. A selected history that no longer
// exists is closed.
func (s *HistoryStore) LoadList() {
	s.Loading = true
	runtime.Run(s.rt.Jobs, jobHistoryList, func(context.Context) ([]*proxyservice.HistoryMetadata, error) {
		return s.backend.ListHistoryKeys()
	}, func(list []*proxyservice.HistoryMetadata, err error) {
		s.Loading = false
		if err != nil {
			logger.G().Errorf("Failed to list history keys: %v", err)
			return
		}
		list = slices.DeleteFunc(slices.Clone(list), func(item *proxyservice.HistoryMetadata) bool { return item == nil })
		slices.SortStableFunc(list, func(a, b *proxyservice.HistoryMetadata) int { return cmp.Compare(b.CreatedAt, a.CreatedAt) })
		s.Metadata = list
		s.ListVersion++
		if s.SelectedKey != "" && !s.hasKey(s.SelectedKey) {
			s.clearSelection()
		}
	})
}

func (s *HistoryStore) hasKey(key string) bool {
	return slices.ContainsFunc(s.Metadata, func(item *proxyservice.HistoryMetadata) bool { return item.Key == key })
}

// MetadataFor returns the metadata of a history.
func (s *HistoryStore) MetadataFor(key string) *proxyservice.HistoryMetadata {
	index := slices.IndexFunc(s.Metadata, func(item *proxyservice.HistoryMetadata) bool { return item.Key == key })
	if index < 0 {
		return nil
	}
	return s.Metadata[index]
}

// Select opens a history. A failure keeps what was open.
func (s *HistoryStore) Select(key string) {
	if s.SelectedKey == key && (s.Traffic.Key == key || s.LoadingHistoryKey == key) {
		return
	}
	previous := s.Traffic.Key
	if previous == "" {
		previous = s.SelectedKey
	}
	s.SelectedKey, s.LoadingHistory, s.LoadingHistoryKey = key, true, key
	runtime.Run(s.rt.Jobs, jobHistoryLoad, func(ctx context.Context) ([]*logic.TrafficEntry, error) {
		entries, err := s.backend.ListHistoryEntries(ctx, key)
		if err != nil {
			return nil, err
		}
		// Decoded identities can differ from the file's index keys. Validate
		// once, before publication; never retain a second identity index.
		seen := make(map[uint64]struct{}, len(entries))
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			if _, duplicate := seen[entry.ID]; duplicate {
				return nil, fmt.Errorf("history contains duplicate row ID %d", entry.ID)
			}
			seen[entry.ID] = struct{}{}
		}
		return entries, ctx.Err()
	}, func(entries []*logic.TrafficEntry, err error) {
		s.LoadingHistory, s.LoadingHistoryKey = false, ""
		if err != nil {
			logger.G().Errorf("Failed to get history: %v", err)
			s.SelectedKey = previous
			if previous == "" {
				s.Traffic.Reset()
			}
			return
		}
		s.Filter.ClearSelectedHosts()
		s.Filter.ClearSelectedProcesses()
		// Drop the previous history's filter results before the new entries
		// replace it.
		s.Filter.Release()
		// Successful reads transfer their array. DeleteFunc compacts in place
		// and clears the removed tail without cloning the full pointer list.
		s.Traffic.Load(key, slices.DeleteFunc(entries, func(entry *logic.TrafficEntry) bool { return entry == nil }))
	})
}

// Delete removes a saved history.
func (s *HistoryStore) Delete(key string, done func(error)) {
	runtime.Run(s.rt.Jobs, jobHistoryDelete, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.DeleteHistory(key)
	}, func(_ struct{}, err error) {
		if err == nil {
			s.rt.Jobs.Cancel(jobHistoryList)
			s.Loading = false
			s.Metadata = slices.DeleteFunc(slices.Clone(s.Metadata), func(item *proxyservice.HistoryMetadata) bool { return item.Key == key })
			s.ListVersion++
			if s.SelectedKey == key {
				s.clearSelection()
			}
		}
		if done != nil {
			done(err)
		}
	})
}

// forgetAll empties the list and closes the opened history after every saved
// history was removed. A list load in progress is abandoned, so it cannot
// bring removed entries back.
func (s *HistoryStore) forgetAll() {
	s.rt.Jobs.Cancel(jobHistoryList)
	s.Metadata, s.Loading = nil, false
	s.ListVersion++
	s.clearSelection()
}

// ClearAll removes every saved history.
func (s *HistoryStore) ClearAll(done func(error)) {
	runtime.Run(s.rt.Jobs, jobHistoryClear, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.ClearHistories()
	}, func(_ struct{}, err error) {
		if err == nil {
			s.forgetAll()
		}
		if done != nil {
			done(err)
		}
	})
}

// ImportHAR imports files one after another, reporting progress.
func (s *HistoryStore) ImportHAR(paths []string, windows bool, done func(HARImportSummary)) bool {
	unique := logic.UniqueHARImportPaths(paths, windows)
	if s.Importing || len(unique) == 0 {
		return false
	}
	s.Importing, s.CurrentFile, s.TotalFiles = true, 0, len(unique)
	runtime.Run(s.rt.Jobs, jobHistoryImport, func(ctx context.Context) (HARImportSummary, error) {
		var summary HARImportSummary
		fail := func(path, message string, noEntries bool) {
			summary.Failed++
			if summary.FirstFailedFile == "" {
				summary.FirstFailedFile, summary.FirstFailure, summary.NoEntries = logic.HARImportFileName(path), message, noEntries
			}
		}
		for index, path := range unique {
			s.rt.Dispatcher.Post(func() { s.CurrentFile = index + 1 })
			result, err := s.backend.ImportHAR(ctx, historyservice.HARImportRequest{Path: path})
			if err != nil {
				logger.G().Errorf("Failed to import HAR file %s: %v", path, err)
				fail(path, err.Error(), false)
				continue
			}
			summary.Imported += result.Imported
			summary.Skipped += result.Skipped
			summary.Missing += result.MissingBodies
			if result.Metadata == nil {
				fail(path, "", true)
				continue
			}
			summary.Files++
			if summary.First == nil {
				summary.First = result.Metadata
			}
		}
		return summary, nil
	}, func(summary HARImportSummary, _ error) {
		s.Importing, s.CurrentFile, s.TotalFiles = false, 0, 0
		if done != nil {
			done(summary)
		}
	})
	return true
}

// HistoryDetailData shows the selection of the opened history in a detail panel.
type HistoryDetailData struct{ History *HistoryStore }

// HistoryDetailSnapshot is the previous complete view, never a current selection
// or an action input. Its lifetime is bounded by the owning HistoryTraffic.
type HistoryDetailSnapshot struct {
	Entry   *logic.TrafficEntry
	Body    *proxyservice.TrafficBodyView
	Version uint64
	// Loading records that Body was still being read when the view was
	// retained, so the view keeps showing loading rather than an empty body.
	Loading bool
}

// SelectedEntry returns the retained record.
func (d *HistoryDetailSnapshot) SelectedEntry() *logic.TrafficEntry { return d.Entry }

// BodyView returns the retained record's body, if it had been loaded.
func (d *HistoryDetailSnapshot) BodyView() *proxyservice.TrafficBodyView { return d.Body }

// BodyVersion identifies the retained body snapshot.
func (d *HistoryDetailSnapshot) BodyVersion() uint64 { return d.Version }

// BodyLoading reports whether the retained body was still loading. The read
// itself was canceled: a retained view never starts or accepts reads.
func (d *HistoryDetailSnapshot) BodyLoading() bool { return d.Loading }

// RetainedDetail supplies the disabled previous view during a brief row change.
func (d HistoryDetailData) RetainedDetail() *HistoryDetailSnapshot { return d.History.Traffic.retained }

// ReleaseRetainedDetail commits a ready replacement and drops the previous view.
func (d HistoryDetailData) ReleaseRetainedDetail() { d.History.Traffic.releaseRetained() }

// SelectedEntry returns the selected entry.
func (d HistoryDetailData) SelectedEntry() *logic.TrafficEntry { return d.History.Traffic.DetailEntry }

// PendingEntry identifies the selection for pending layout only, never for
// rendering its summary headers as complete details.
func (d HistoryDetailData) PendingEntry() *logic.TrafficEntry {
	if d.History.Traffic.DetailLoading {
		return d.History.Traffic.Selected
	}
	return nil
}

// RequestBody loads payloads only when an inspection pane needs them.
func (d HistoryDetailData) RequestBody() { d.History.Traffic.RequestBody() }

// BodyView returns the selected entry's bodies.
func (d HistoryDetailData) BodyView() *proxyservice.TrafficBodyView {
	return d.History.Traffic.BodyView
}

// BodyVersion changes whenever BodyView does.
func (d HistoryDetailData) BodyVersion() uint64 { return d.History.Traffic.BodyViewVersion }

// BodyLoading reports whether bodies are loading.
func (d HistoryDetailData) BodyLoading() bool { return d.History.Traffic.BodyViewLoading }
