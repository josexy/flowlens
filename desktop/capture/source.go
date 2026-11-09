package capture

import (
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
)

// LiveControl holds a live table's window while it is being read.
type LiveControl interface {
	PauseLiveEviction()
	ResumeLiveEviction()
	PendingLiveEntryCount() int
}

// Source is the traffic a table shows, live or saved.
type Source interface {
	Filter() *store.FilterStore
	SelectedEntry() *logic.TrafficEntry
	SelectEntry(entry *logic.TrafficEntry)
	ShowDetail() bool
	SetShowDetail(show bool)
	Highlight(id uint64) string
	SetHighlight(id uint64, color string)
	Delete(ids []uint64)
	BodyView(id uint64, done func(*proxyservice.TrafficBodyView))
	PendingFocus() uint64
	ClearPendingFocus()
	SetSelectedCount(count int)
	// Live returns the live window's controls, or nil for saved traffic.
	Live() LiveControl
}

// LiveSource shows the current capture.
type LiveSource struct {
	Traffic *store.TrafficStore
	Filters *store.FilterStore
}

// Filter implements Source.
func (s LiveSource) Filter() *store.FilterStore { return s.Filters }

// SelectedEntry implements Source.
func (s LiveSource) SelectedEntry() *logic.TrafficEntry { return s.Traffic.Selected }

// SelectEntry implements Source.
func (s LiveSource) SelectEntry(entry *logic.TrafficEntry) { s.Traffic.Select(entry) }

// ShowDetail implements Source.
func (s LiveSource) ShowDetail() bool { return s.Traffic.ShowDetailPanel }

// SetShowDetail implements Source.
func (s LiveSource) SetShowDetail(show bool) { s.Traffic.SetShowDetailPanel(show) }

// Highlight implements Source.
func (s LiveSource) Highlight(id uint64) string { return s.Traffic.Highlights[id] }

// SetHighlight implements Source.
func (s LiveSource) SetHighlight(id uint64, color string) { s.Traffic.SetHighlight(id, color) }

// Delete implements Source.
func (s LiveSource) Delete(ids []uint64) { s.Traffic.Delete(ids) }

// BodyView implements Source.
func (s LiveSource) BodyView(id uint64, done func(*proxyservice.TrafficBodyView)) {
	s.Traffic.GetBodyView(id, done)
}

// PendingFocus implements Source.
func (s LiveSource) PendingFocus() uint64 { return s.Traffic.PendingFocusID }

// ClearPendingFocus implements Source.
func (s LiveSource) ClearPendingFocus() { s.Traffic.ClearPendingFocus() }

// SetSelectedCount implements Source.
func (s LiveSource) SetSelectedCount(count int) { s.Traffic.SelectedCount = count }

// Live implements Source.
func (s LiveSource) Live() LiveControl { return s.Traffic }
