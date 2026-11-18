package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/pkg/logger"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

const (
	jobHistoryList   = "history:list"
	jobHistoryLoad   = "history:load"
	jobHistoryDelete = "history:delete"
	jobHistoryClear  = "history:clear"
	jobHistoryImport = "history:import"
)

// HistoryBackend is the history service as the stores use it.
type HistoryBackend interface {
	ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error)
	GetHistory(key string) ([]*logic.TrafficEntry, error)
	GetHistoryTrafficBodyView(key string, id uint64) (*proxyservice.TrafficBodyView, error)
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
	SelectedCount   int
	BodyView        *proxyservice.TrafficBodyView
	BodyViewVersion uint64
	BodyViewLoading bool
	ShowDetailPanel bool
	Highlights      map[uint64]string
	PendingFocusID  uint64

	loadedBodyEntry uint64
	bodyToken       uint64
	bodySequence    uint64
}

func (t *HistoryTraffic) setBodyView(view *proxyservice.TrafficBodyView) {
	t.BodyView = view
	t.BodyViewVersion++
}

func (t *HistoryTraffic) clearBody() {
	t.bodyToken++
	if t.BodyView != nil {
		t.setBodyView(nil)
	}
	t.BodyViewLoading = false
	t.loadedBodyEntry = 0
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
		t.clearBody()
	}
	t.loadBody()
}

// SetShowDetailPanel opens or closes the detail panel.
func (t *HistoryTraffic) SetShowDetailPanel(show bool) {
	t.ShowDetailPanel = show
	if !show {
		t.clearBody()
		return
	}
	t.loadBody()
}

func (t *HistoryTraffic) loadBody() {
	entry := t.Selected
	if entry == nil || !t.ShowDetailPanel || t.Key == "" {
		return
	}
	if !logic.CapabilitiesOf(entry).CanLoadBody {
		t.clearBody()
		return
	}
	if t.BodyView != nil && t.loadedBodyEntry == entry.ID {
		return
	}
	t.bodyToken++
	token, key, id := t.bodyToken, t.Key, entry.ID
	t.BodyViewLoading = true
	t.bodySequence++
	runtime.Run(t.rt.Jobs, fmt.Sprintf("history:body:%d", t.bodySequence), func(context.Context) (*proxyservice.TrafficBodyView, error) {
		return t.backend.GetHistoryTrafficBodyView(key, id)
	}, func(view *proxyservice.TrafficBodyView, err error) {
		if token != t.bodyToken {
			return
		}
		t.BodyViewLoading = false
		if err != nil {
			logger.G().Errorf("Failed to get history traffic body view: %v", err)
			if t.BodyView != nil {
				t.setBodyView(nil)
			}
			t.loadedBodyEntry = 0
			return
		}
		t.setBodyView(view)
		t.loadedBodyEntry = id
	})
}

// GetBodyView loads the bodies of any entry. done receives nil for raw TCP
// entries and on failure.
func (t *HistoryTraffic) GetBodyView(id uint64, done func(*proxyservice.TrafficBodyView)) {
	index := slices.IndexFunc(t.Entries, func(entry *logic.TrafficEntry) bool { return entry.ID == id })
	if t.Key == "" || (index >= 0 && logic.IsRawTCP(t.Entries[index])) {
		done(nil)
		return
	}
	key := t.Key
	t.bodySequence++
	runtime.Run(t.rt.Jobs, fmt.Sprintf("history:body:%d", t.bodySequence), func(context.Context) (*proxyservice.TrafficBodyView, error) {
		return t.backend.GetHistoryTrafficBodyView(key, id)
	}, func(view *proxyservice.TrafficBodyView, err error) {
		if err != nil {
			logger.G().Errorf("Failed to get history traffic body view: %v", err)
			done(nil)
			return
		}
		done(view)
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
	t.clearBody()
	t.Key, t.Entries, t.Selected, t.SelectedCount, t.ShowDetailPanel, t.PendingFocusID = "", nil, nil, 0, false, 0
	t.EntriesVersion++
	t.Highlights = map[uint64]string{}
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
		Filter: NewFilterStore(func() ([]*logic.TrafficEntry, uint64) { return traffic.Entries, traffic.EntriesVersion }),
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
	runtime.Run(s.rt.Jobs, jobHistoryLoad, func(context.Context) ([]*logic.TrafficEntry, error) {
		return s.backend.GetHistory(key)
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
		s.Traffic.Load(key, slices.DeleteFunc(slices.Clone(entries), func(entry *logic.TrafficEntry) bool { return entry == nil }))
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

// ClearAll removes every saved history.
func (s *HistoryStore) ClearAll(done func(error)) {
	runtime.Run(s.rt.Jobs, jobHistoryClear, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.ClearHistories()
	}, func(_ struct{}, err error) {
		if err == nil {
			s.rt.Jobs.Cancel(jobHistoryList)
			s.Metadata, s.Loading = nil, false
			s.clearSelection()
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

// SelectedEntry returns the selected entry.
func (d HistoryDetailData) SelectedEntry() *logic.TrafficEntry { return d.History.Traffic.Selected }

// BodyView returns the selected entry's bodies.
func (d HistoryDetailData) BodyView() *proxyservice.TrafficBodyView {
	return d.History.Traffic.BodyView
}

// BodyVersion changes whenever BodyView does.
func (d HistoryDetailData) BodyVersion() uint64 { return d.History.Traffic.BodyViewVersion }

// BodyLoading reports whether bodies are loading.
func (d HistoryDetailData) BodyLoading() bool { return d.History.Traffic.BodyViewLoading }
