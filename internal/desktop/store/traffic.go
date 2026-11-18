package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/pkg/logger"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// TrafficEntryCap is how many requests the interface keeps.
const TrafficEntryCap = 5000

const (
	trafficEntryBatchInterval  = 100 * time.Millisecond
	liveUpdateBatchInterval    = 100 * time.Millisecond
	statisticsDebounceInterval = 300 * time.Millisecond
	trafficPatchBufferPerEntry = 16

	eventTrafficEntry      = "traffic:entry"
	eventTrafficPatch      = "traffic:patch"
	eventTrafficLiveUpdate = "traffic:live-update"
	eventTrafficReset      = "traffic:reset"

	jobTrafficInitial    = "traffic:initial"
	jobTrafficRecover    = "traffic:recover"
	jobTrafficStatistics = "traffic:statistics"
	jobTrafficClear      = "traffic:clear"
)

// TrafficBackend is the capture service as the traffic store uses it.
type TrafficBackend interface {
	GetTraffic() []*logic.TrafficEntry
	GetStatistics() proxyservice.TrafficStatistics
	ClearTraffic() error
	DeleteTraffic(ids []int64)
	SetLiveTrafficDetail(id uint64)
	GetTrafficBodyView(id uint64) (*proxyservice.TrafficBodyView, error)
}

type initialTrafficBuffer struct {
	active     bool
	entries    *orderedMap[*logic.TrafficEntry]
	patches    *orderedMap[[]*logic.TrafficPatch]
	deletedIDs *orderedMap[struct{}]
	evicted    uint64
}

func (b *initialTrafficBuffer) clear() {
	b.entries.Clear()
	b.patches.Clear()
	b.deletedIDs.Clear()
	b.evicted = 0
}

// utf8StreamDecoder decodes text arriving in arbitrary chunks. A character
// split across chunks is held until its remaining bytes arrive, a byte order
// mark at the start is removed, and invalid bytes become U+FFFD.
type utf8StreamDecoder struct {
	pending []byte
	started bool
}

func (d *utf8StreamDecoder) decode(chunk []byte) string {
	data := chunk
	if len(d.pending) > 0 {
		data = append(d.pending, chunk...)
		d.pending = nil
	}
	if !d.started {
		if len(data) < 3 && len(data) > 0 && strings.HasPrefix("\xef\xbb\xbf", string(data)) {
			d.pending = append([]byte(nil), data...)
			return ""
		}
		d.started = len(data) > 0
		data = []byte(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	}
	end := len(data)
	for back := 1; back < utf8.UTFMax && back <= len(data); back++ {
		position := len(data) - back
		if utf8.RuneStart(data[position]) {
			if !utf8.FullRune(data[position:]) {
				end = position
			}
			break
		}
	}
	if end < len(data) {
		d.pending = append([]byte(nil), data[end:]...)
	}
	return strings.ToValidUTF8(string(data[:end]), "�")
}

// TrafficStore holds the live capture: at most TrafficEntryCap entries in
// ascending ID order. Newer events and backend snapshots are reconciled by
// revision, and a lost event leads to recovery from a backend snapshot.
type TrafficStore struct {
	rt      Runtime
	backend TrafficBackend
	clock   Clock
	router  *EventRouter

	// Entries is in ascending ID order. Treat it as read-only.
	Entries []*logic.TrafficEntry
	// EntriesVersion changes whenever Entries does.
	EntriesVersion uint64
	// EvictionVersion changes whenever old entries leave the window.
	EvictionVersion uint64

	Selected        *logic.TrafficEntry
	SelectedCount   int
	BodyView        *proxyservice.TrafficBodyView
	BodyViewLoading bool
	// BodyViewVersion changes whenever BodyView does.
	BodyViewVersion uint64
	ShowDetailPanel bool
	Statistics      proxyservice.TrafficStatistics
	Highlights      map[uint64]string
	// PendingFocusID asks the table to reveal an entry; zero means none.
	PendingFocusID uint64
	// OnReset releases views derived from a discarded capture, even when hidden.
	OnReset func()
	// OnDetailReset releases viewer caches when their selection becomes invalid.
	OnDetailReset func()

	index          map[uint64]int
	surfaceActive  bool
	paused         bool
	pausedEntries  *orderedMap[*logic.TrafficEntry]
	pending        *orderedMap[*logic.TrafficEntry]
	stopBatch      func()
	evictedThrough uint64

	generation      uint64
	snapshotEpoch   uint64
	recoveryEpoch   uint64
	initial         *initialTrafficBuffer
	offs            []func()
	resyncRequested bool
	resyncInFlight  bool

	loadedBodyEntry uint64
	bodyToken       uint64
	bodySequence    uint64
	deleteSequence  uint64
	terminalRefresh logic.TerminalBodyRefreshQueue
	stopStats       func()

	pendingLive     []proxyservice.TrafficLiveUpdate
	stopLive        func()
	liveCursorEntry uint64
	liveSSEOffset   int64
	liveSSEDecoder  *utf8StreamDecoder
	liveSSEBody     *strings.Builder // the published live SSE body, appended per chunk
	liveSSEReady    bool
	liveWSCount     int
	liveRecovering  bool
	liveReady       bool
	desiredLive     uint64
	appliedLive     uint64

	watched      bool
	watchEntry   uint64
	watchPanel   bool
	watchSurface bool
}

// NewTrafficStore returns an empty store.
func NewTrafficStore(rt Runtime, backend TrafficBackend, router *EventRouter, clock Clock) *TrafficStore {
	if clock == nil {
		clock = dispatcherClock{dispatcher: rt.Dispatcher}
	}
	s := &TrafficStore{
		rt: rt, backend: backend, clock: clock, router: router,
		Highlights: map[uint64]string{}, index: map[uint64]int{}, surfaceActive: true,
		pausedEntries: newOrderedMap[*logic.TrafficEntry](), pending: newOrderedMap[*logic.TrafficEntry](),
		liveSSEDecoder: &utf8StreamDecoder{},
	}
	s.syncDetail()
	return s
}

// PendingLiveEntryCount returns how many new requests wait while reading.
func (s *TrafficStore) PendingLiveEntryCount() int { return s.pausedEntries.Len() }

// LiveEvictionPaused reports whether the window is held for reading.
func (s *TrafficStore) LiveEvictionPaused() bool { return s.paused }

func (s *TrafficStore) selectedID() uint64 {
	if s.Selected == nil {
		return 0
	}
	return s.Selected.ID
}

func (s *TrafficStore) setBodyView(view *proxyservice.TrafficBodyView) {
	s.BodyView = view
	s.BodyViewVersion++
}

func (s *TrafficStore) entriesChanged() { s.EntriesVersion++ }

// syncDetail reacts to a change of the selected entry, the detail panel, or
// whether the capture is on screen.
func (s *TrafficStore) syncDetail() {
	id := s.selectedID()
	first := !s.watched
	previousSurface := s.watchSurface
	if !first && id == s.watchEntry && s.ShowDetailPanel == s.watchPanel && s.surfaceActive == s.watchSurface {
		return
	}
	if (id != s.watchEntry || !s.ShowDetailPanel) && s.OnDetailReset != nil {
		s.OnDetailReset()
	}
	s.watched, s.watchEntry, s.watchPanel, s.watchSurface = true, id, s.ShowDetailPanel, s.surfaceActive

	canShowLive := logic.CapabilitiesOf(s.Selected).CanSubscribeLiveDetail
	if id != 0 && s.ShowDetailPanel && s.surfaceActive && canShowLive {
		s.setDesiredLive(id)
	} else {
		s.setDesiredLive(0)
	}
	if id == 0 {
		s.terminalRefresh.Cancel()
		return
	}
	if !canShowLive || !s.ShowDetailPanel {
		s.terminalRefresh.Cancel()
		s.bodyToken++
		if s.BodyView != nil {
			s.setBodyView(nil)
		}
		s.BodyViewLoading = false
		s.loadedBodyEntry = 0
		s.resetLiveReconciliation()
		return
	}
	if !s.surfaceActive {
		s.terminalRefresh.Cancel()
		return
	}
	s.terminalRefresh.Activate(id)
	s.ensureBodyLoaded(!first && !previousSurface, nil)
}

func (s *TrafficStore) setDesiredLive(id uint64) {
	s.desiredLive = id
	if !s.liveReady || s.appliedLive == id {
		return
	}
	s.appliedLive = id
	s.backend.SetLiveTrafficDetail(id)
}

// SetShowDetailPanel opens or closes the detail panel.
func (s *TrafficStore) SetShowDetailPanel(show bool) {
	s.ShowDetailPanel = show
	s.syncDetail()
}

// Select makes entry the selected one; nil clears the selection and closes the
// detail panel.
func (s *TrafficStore) Select(entry *logic.TrafficEntry) {
	previous := s.selectedID()
	s.Selected = entry
	if entry == nil {
		s.terminalRefresh.Cancel()
		s.bodyToken++
		if s.BodyView != nil {
			s.setBodyView(nil)
		}
		s.BodyViewLoading = false
		s.loadedBodyEntry = 0
		s.ShowDetailPanel = false
		s.resetLiveReconciliation()
		s.syncDetail()
		return
	}
	if previous != entry.ID {
		s.terminalRefresh.Activate(entry.ID)
		s.bodyToken++
		if s.BodyView != nil {
			s.setBodyView(nil)
		}
		s.BodyViewLoading = false
		s.loadedBodyEntry = 0
		s.resetLiveReconciliation()
	}
	s.syncDetail()
}

func (s *TrafficStore) ensureBodyLoaded(force bool, finished func()) {
	if finished == nil {
		finished = func() {}
	}
	entry := s.Selected
	if entry == nil || !s.ShowDetailPanel || !s.surfaceActive || !logic.CapabilitiesOf(entry).CanLoadBody {
		finished()
		return
	}
	if !force && s.BodyView != nil && s.loadedBodyEntry == entry.ID {
		finished()
		return
	}
	s.bodyToken++
	token := s.bodyToken
	s.BodyViewLoading = true
	s.bodySequence++
	key := fmt.Sprintf("traffic:body:%d", s.bodySequence)
	entryID := entry.ID
	runtime.Run(s.rt.Jobs, key, func(context.Context) (*proxyservice.TrafficBodyView, error) {
		view, err := s.backend.GetTrafficBodyView(entryID)
		if err == nil && view == nil {
			err = fmt.Errorf("traffic body view %d was not returned", entryID)
		}
		return view, err
	}, func(view *proxyservice.TrafficBodyView, err error) {
		defer finished()
		s.finishBodyLoad(entryID, token, force, view, err)
	})
}

func (s *TrafficStore) finishBodyLoad(entryID, token uint64, force bool, view *proxyservice.TrafficBodyView, err error) {
	current := token == s.bodyToken
	selected := s.selectedID() == entryID
	switch {
	case err != nil:
		if current && selected {
			if force && s.BodyView != nil && s.loadedBodyEntry == entryID {
				s.initLiveCursors(entryID, s.BodyView)
			} else {
				if s.BodyView != nil {
					s.setBodyView(nil)
				}
				s.loadedBodyEntry = 0
			}
			logger.G().Errorf("Failed to get traffic entry with body: %v", err)
		}
	case current && s.ShowDetailPanel && s.surfaceActive && selected:
		s.setBodyView(view)
		s.loadedBodyEntry = entryID
		s.initLiveCursors(entryID, view)
	}
	if !current {
		return
	}
	s.BodyViewLoading = false
	if len(s.pendingLive) > 0 {
		s.scheduleLiveFlush(0)
	}
	if s.terminalRefresh.CompleteLoad(entryID) && selected && s.ShowDetailPanel && s.surfaceActive {
		s.ensureBodyLoaded(true, nil)
	}
}

func (s *TrafficStore) refreshBodyForTerminalMessages(previous, next *logic.TrafficEntry) {
	sides := logic.NewTerminalMessageSides(previous, next)
	if len(sides) == 0 || s.selectedID() != next.ID || !s.ShowDetailPanel || !s.surfaceActive || !logic.CapabilitiesOf(next).CanLoadBody {
		return
	}
	s.terminalRefresh.Activate(next.ID)
	if s.terminalRefresh.Request(next.ID, sides, s.BodyViewLoading) {
		s.ensureBodyLoaded(true, nil)
	}
}

// GetBodyView loads the bodies of any entry, for actions such as copying.
// done receives nil for raw TCP entries and on failure.
func (s *TrafficStore) GetBodyView(entryID uint64, done func(*proxyservice.TrafficBodyView)) {
	if index, ok := s.index[entryID]; ok && logic.IsRawTCP(s.Entries[index]) {
		done(nil)
		return
	}
	s.bodySequence++
	key := fmt.Sprintf("traffic:body:%d", s.bodySequence)
	runtime.Run(s.rt.Jobs, key, func(context.Context) (*proxyservice.TrafficBodyView, error) {
		return s.backend.GetTrafficBodyView(entryID)
	}, func(view *proxyservice.TrafficBodyView, err error) {
		if err != nil {
			logger.G().Errorf("Failed to get traffic body view: %v", err)
			done(nil)
			return
		}
		done(view)
	})
}

func (s *TrafficStore) resetLiveReconciliation() {
	if s.stopLive != nil {
		s.stopLive()
		s.stopLive = nil
	}
	s.pendingLive = nil
	s.liveCursorEntry = 0
	s.liveSSEOffset = 0
	s.liveSSEDecoder = &utf8StreamDecoder{}
	s.liveSSEBody = nil
	s.liveSSEReady = false
	s.liveWSCount = 0
	s.liveRecovering = false
}

func (s *TrafficStore) initLiveCursors(entryID uint64, view *proxyservice.TrafficBodyView) {
	s.liveCursorEntry = entryID
	s.liveSSEDecoder = &utf8StreamDecoder{}
	// Seeded from the snapshot by the first SSE chunk, so bodies that never
	// stream are not copied.
	s.liveSSEBody = nil
	s.liveSSEReady = !view.ResponseBodyBinary
	s.liveSSEOffset = 0
	if s.liveSSEReady {
		s.liveSSEOffset = int64(len(view.ResponseBody))
	}
	s.liveWSCount = len(view.WebSocketMessages)
}

// prepareBinarySSESnapshot switches a snapshot first loaded as binary to
// text once SSE data arrives, decoding its bytes as the stream's start.
func (s *TrafficStore) prepareBinarySSESnapshot(view *proxyservice.TrafficBodyView) *proxyservice.TrafficBodyView {
	if s.liveSSEReady || !view.ResponseBodyBinary {
		return view
	}
	s.liveSSEDecoder = &utf8StreamDecoder{}
	decoded := s.liveSSEDecoder.decode([]byte(view.ResponseBody))
	s.liveSSEOffset = int64(len(view.ResponseBody))
	s.liveSSEReady = true
	s.liveSSEBody = &strings.Builder{}
	s.liveSSEBody.WriteString(decoded)
	copied := *view
	copied.ResponseBody = s.liveSSEBody.String()
	copied.ResponseBodyBinary = false
	return &copied
}

// liveSSEBodyFor returns the builder holding current, seeding a new one when
// the published body is not the builder's. Comparing with the builder's own
// string is constant time while the two share their bytes.
func (s *TrafficStore) liveSSEBodyFor(current string) *strings.Builder {
	if s.liveSSEBody != nil && s.liveSSEBody.String() == current {
		return s.liveSSEBody
	}
	s.liveSSEBody = &strings.Builder{}
	s.liveSSEBody.WriteString(current)
	return s.liveSSEBody
}

func (s *TrafficStore) scheduleLiveFlush(delay time.Duration) {
	if s.stopLive != nil {
		return
	}
	s.stopLive = s.clock.After(delay, func() {
		s.stopLive = nil
		s.flushLive()
	})
}

func (s *TrafficStore) recoverBodyView() {
	if s.liveRecovering {
		return
	}
	s.liveRecovering = true
	s.ensureBodyLoaded(true, func() { s.liveRecovering = false })
}

func (s *TrafficStore) flushLive() {
	entryID := s.selectedID()
	if entryID == 0 || !s.ShowDetailPanel || !s.surfaceActive || len(s.pendingLive) == 0 {
		return
	}
	view := s.BodyView
	if view == nil || s.loadedBodyEntry != entryID || s.BodyViewLoading {
		return
	}
	if s.liveCursorEntry != entryID {
		s.initLiveCursors(entryID, view)
	}
	batch := s.pendingLive
	s.pendingLive = nil

	var sseUpdates, wsUpdates []proxyservice.TrafficLiveUpdate
	truncated := false
	for _, update := range batch {
		switch {
		case update.Kind == proxyservice.TrafficLiveUpdateSSEChunk && update.Offset != nil:
			sseUpdates = append(sseUpdates, update)
		case update.Kind == proxyservice.TrafficLiveUpdateWebSocketMessage && update.MessageIndex != nil && update.Message != nil:
			wsUpdates = append(wsUpdates, update)
		case update.Kind == proxyservice.TrafficLiveUpdateWebSocketTruncated:
			truncated = true
		}
	}
	slices.SortStableFunc(sseUpdates, func(a, b proxyservice.TrafficLiveUpdate) int { return cmp.Compare(*a.Offset, *b.Offset) })
	slices.SortStableFunc(wsUpdates, func(a, b proxyservice.TrafficLiveUpdate) int { return cmp.Compare(*a.MessageIndex, *b.MessageIndex) })

	next, changed, err := s.applyLive(view, sseUpdates, wsUpdates, truncated)
	if err != nil {
		logger.G().Warnf("Live traffic updates lost ordering; reloading body snapshot: %v", err)
		s.recoverBodyView()
		return
	}
	if changed && s.selectedID() == entryID && s.ShowDetailPanel {
		s.setBodyView(next)
	}
}

func (s *TrafficStore) applyLive(view *proxyservice.TrafficBodyView, sse, ws []proxyservice.TrafficLiveUpdate, truncated bool) (*proxyservice.TrafficBodyView, bool, error) {
	next := *view
	changed := false
	if len(sse) > 0 {
		wasBinary := next.ResponseBodyBinary
		next = *s.prepareBinarySSESnapshot(&next)
		if wasBinary && !next.ResponseBodyBinary {
			changed = true
		}
		body := s.liveSSEBodyFor(next.ResponseBody)
		for _, update := range sse {
			chunk := update.Chunk
			start := *update.Offset
			end := start + int64(len(chunk))
			if end <= s.liveSSEOffset {
				continue
			}
			if start > s.liveSSEOffset {
				return nil, false, fmt.Errorf("SSE live update gap: expected %d, received %d", s.liveSSEOffset, start)
			}
			body.WriteString(s.liveSSEDecoder.decode(chunk[max(0, s.liveSSEOffset-start):]))
			s.liveSSEOffset = end
			changed = true
		}
		next.ResponseBody = body.String()
		next.ResponseBodyBinary = false
	}
	if len(ws) > 0 {
		messages := next.WebSocketMessages
		for _, update := range ws {
			index := *update.MessageIndex
			if index < s.liveWSCount {
				continue
			}
			if index > s.liveWSCount {
				return nil, false, fmt.Errorf("WebSocket live update gap: expected %d, received %d", s.liveWSCount, index)
			}
			messages = append(messages, update.Message)
			s.liveWSCount++
			changed = true
		}
		next.WebSocketMessages = messages
	}
	if truncated {
		next.WsMsgsTruncated = true
		changed = true
	}
	return &next, changed, nil
}

func (s *TrafficStore) handleLiveUpdate(data any) {
	update, ok := data.(proxyservice.TrafficLiveUpdate)
	if !ok {
		pointer, isPointer := data.(*proxyservice.TrafficLiveUpdate)
		if !isPointer || pointer == nil {
			return
		}
		update = *pointer
	}
	entryID := s.selectedID()
	if entryID == 0 || !s.ShowDetailPanel || !s.surfaceActive || update.TrafficID != entryID {
		return
	}
	s.pendingLive = append(s.pendingLive, update)
	s.scheduleLiveFlush(liveUpdateBatchInterval)
}

// ResetState empties the store, as when a new capture session starts.
func (s *TrafficStore) ResetState() {
	s.rt.Jobs.Cancel(jobTrafficStatistics)
	s.recoveryEpoch++
	s.resyncRequested = false
	s.invalidateInitialSnapshot()
	s.bodyToken++
	s.terminalRefresh.Cancel()
	s.resetEntryBatch()
	s.Entries = nil
	s.entriesChanged()
	clear(s.index)
	s.evictedThrough = 0
	s.Selected = nil
	s.SelectedCount = 0
	if s.BodyView != nil {
		s.setBodyView(nil)
	}
	s.BodyViewLoading = false
	s.loadedBodyEntry = 0
	s.Statistics = proxyservice.TrafficStatistics{}
	clear(s.Highlights)
	s.ShowDetailPanel = false
	s.PendingFocusID = 0
	s.pausedEntries.Clear()
	s.resetLiveReconciliation()
	s.syncDetail()
	if s.OnReset != nil {
		s.OnReset()
	}
}

// ClearAll asks the backend to clear the capture.
func (s *TrafficStore) ClearAll(done func(error)) {
	s.recoveryEpoch++
	s.invalidateInitialSnapshot()
	runtime.Run(s.rt.Jobs, jobTrafficClear, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.ClearTraffic()
	}, func(_ struct{}, err error) {
		if err == nil {
			s.updateStatistics()
		}
		if done != nil {
			done(err)
		}
	})
}

func (s *TrafficStore) updateStatistics() {
	runtime.Run(s.rt.Jobs, jobTrafficStatistics, func(context.Context) (proxyservice.TrafficStatistics, error) {
		return s.backend.GetStatistics(), nil
	}, func(statistics proxyservice.TrafficStatistics, err error) {
		if err == nil {
			s.Statistics = statistics
		}
	})
}

func (s *TrafficStore) scheduleStatistics() {
	if s.stopStats != nil {
		s.stopStats()
	}
	s.stopStats = s.clock.After(statisticsDebounceInterval, func() {
		s.stopStats = nil
		s.updateStatistics()
	})
}

func (s *TrafficStore) rebuildIndex() {
	clear(s.index)
	for position, entry := range s.Entries {
		s.index[entry.ID] = position
	}
}

func (s *TrafficStore) reconcileSelected() {
	id := s.selectedID()
	if id == 0 {
		return
	}
	if position, ok := s.index[id]; ok {
		s.Select(s.Entries[position])
		return
	}
	s.Select(nil)
}

type normalizedSnapshot struct {
	entries []*logic.TrafficEntry
	evicted uint64
}

func normalizeSnapshot(snapshot []*logic.TrafficEntry) normalizedSnapshot {
	valid := make([]*logic.TrafficEntry, 0, len(snapshot))
	for _, entry := range snapshot {
		if entry != nil {
			valid = append(valid, entry)
		}
	}
	slices.SortFunc(valid, func(a, b *logic.TrafficEntry) int { return cmp.Compare(a.ID, b.ID) })
	if len(valid) <= TrafficEntryCap {
		return normalizedSnapshot{entries: valid}
	}
	overflow := len(valid) - TrafficEntryCap
	return normalizedSnapshot{entries: valid[overflow:], evicted: valid[overflow-1].ID}
}

func (s *TrafficStore) invalidateInitialSnapshot() {
	s.snapshotEpoch++
	if s.initial != nil {
		s.initial.clear()
	}
}

func processProgress(process *proxyservice.ProcessInfo) int {
	switch {
	case process == nil:
		return -1
	case process.Status == proxyservice.ProcessStatusPending:
		return 0
	case process.Status == proxyservice.ProcessStatusResolved && process.IconKey != "":
		return 2
	default:
		return 1
	}
}

func mergeInitialMessage(snapshot, buffered *proxyservice.HTTPMessage) *proxyservice.HTTPMessage {
	if snapshot == nil {
		return buffered
	}
	if buffered == nil {
		return snapshot
	}
	merged := *snapshot
	if merged.Proto == "" {
		merged.Proto = buffered.Proto
	}
	if snapshot.HeaderFields == nil {
		merged.HeaderFields = buffered.HeaderFields
		merged.HeadersTruncated = buffered.HeadersTruncated
		merged.HeaderOrderUnavailable = buffered.HeaderOrderUnavailable
	}
	if len(snapshot.TrailerFields) == 0 && len(buffered.TrailerFields) > 0 {
		merged.TrailerFields = buffered.TrailerFields
		merged.TrailersTruncated = buffered.TrailersTruncated
		merged.TrailerOrderUnavailable = buffered.TrailerOrderUnavailable
	}
	return &merged
}

func mergeInitialEntry(snapshot, buffered *logic.TrafficEntry) *logic.TrafficEntry {
	if snapshot.Revision > 0 || buffered.Revision > 0 {
		if buffered.Revision > snapshot.Revision {
			return buffered
		}
		return snapshot
	}
	// Snapshot reads and event delivery are not atomic, so keep fields that
	// only move forward.
	merged := *snapshot
	metadata := snapshot.Metadata
	if metadata == nil {
		metadata = buffered.Metadata
	}
	var snapshotProcess, bufferedProcess *proxyservice.ProcessInfo
	if snapshot.Metadata != nil {
		snapshotProcess = snapshot.Metadata.Process
	}
	if buffered.Metadata != nil {
		bufferedProcess = buffered.Metadata.Process
	}
	if metadata != nil && processProgress(bufferedProcess) > processProgress(snapshotProcess) {
		copied := *metadata
		copied.Process = bufferedProcess
		metadata = &copied
	}
	merged.Metadata = metadata
	if merged.StatusCode == 0 {
		merged.StatusCode = buffered.StatusCode
	}
	if merged.Status == "" {
		merged.Status = buffered.Status
	}
	if merged.RawTCP == nil {
		merged.RawTCP = buffered.RawTCP
	}
	merged.Request = mergeInitialMessage(snapshot.Request, buffered.Request)
	merged.Response = mergeInitialMessage(snapshot.Response, buffered.Response)
	if merged.Error == nil {
		merged.Error = buffered.Error
	}
	return &merged
}

func (s *TrafficStore) mergeRecoveredSnapshot(snapshot normalizedSnapshot) {
	currentByID := make(map[uint64]*logic.TrafficEntry, len(s.Entries))
	for _, entry := range s.Entries {
		currentByID[entry.ID] = entry
	}
	recovered := make([]*logic.TrafficEntry, 0, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		if current, ok := currentByID[entry.ID]; ok {
			delete(currentByID, entry.ID)
			recovered = append(recovered, mergeInitialEntry(entry, current))
			continue
		}
		recovered = append(recovered, entry)
	}
	if s.paused && len(s.Entries) > 0 {
		// Recovery must not replace the window being read. Refresh the kept
		// entries and merge missed IDs before the queued live entries.
		queued := make(map[uint64]*logic.TrafficEntry, s.pausedEntries.Len()+len(recovered))
		s.pausedEntries.Range(func(id uint64, entry *logic.TrafficEntry) { queued[id] = entry })
		for _, entry := range recovered {
			if existing, ok := queued[entry.ID]; ok {
				queued[entry.ID] = mergeInitialEntry(entry, existing)
				continue
			}
			queued[entry.ID] = entry
		}
		s.pausedEntries.Clear()
		merged := make([]*logic.TrafficEntry, 0, len(queued))
		for _, entry := range queued {
			merged = append(merged, entry)
		}
		slices.SortFunc(merged, func(a, b *logic.TrafficEntry) int { return cmp.Compare(a.ID, b.ID) })
		s.applyBatch(merged)
		s.reconcileSelected()
		return
	}
	var newest uint64
	for _, entry := range recovered {
		newest = max(newest, entry.ID)
	}
	for _, current := range s.Entries {
		if _, kept := currentByID[current.ID]; !kept {
			continue
		}
		if newest != 0 && current.ID <= newest {
			continue
		}
		recovered = append(recovered, current)
	}
	var evicted uint64
	if over := len(recovered) - TrafficEntryCap; over > 0 {
		evicted = recovered[over-1].ID
		recovered = recovered[over:]
	}
	s.Entries = recovered
	s.advanceEviction(evicted, snapshot.evicted)
	s.entriesChanged()
	s.rebuildIndex()
	s.reconcileSelected()
	s.scheduleStatistics()
}

func (s *TrafficStore) requestRecovery() {
	s.resyncRequested = true
	if s.resyncInFlight || s.initial != nil || !s.surfaceActive {
		return
	}
	s.recoverSnapshot()
}

func (s *TrafficStore) recoveryWanted() bool {
	return s.resyncRequested && s.initial == nil && s.surfaceActive
}

func (s *TrafficStore) recoverSnapshot() {
	if s.resyncInFlight || s.initial != nil || !s.surfaceActive {
		return
	}
	s.resyncInFlight = true
	s.recoverNext()
}

func (s *TrafficStore) finishRecovery() {
	s.resyncInFlight = false
	if s.recoveryWanted() {
		s.recoverSnapshot()
	}
}

func (s *TrafficStore) recoverNext() {
	if !s.recoveryWanted() {
		s.finishRecovery()
		return
	}
	s.resyncRequested = false
	generation, epoch := s.generation, s.recoveryEpoch
	runtime.Run(s.rt.Jobs, jobTrafficRecover, func(context.Context) ([]*logic.TrafficEntry, error) {
		return s.backend.GetTraffic(), nil
	}, func(entries []*logic.TrafficEntry, err error) {
		if err != nil {
			logger.G().Errorf("Failed to recover dropped traffic events: %v", err)
			s.finishRecovery()
			return
		}
		if generation != s.generation || epoch != s.recoveryEpoch {
			s.finishRecovery()
			return
		}
		if !s.surfaceActive {
			s.resyncRequested = true
			s.finishRecovery()
			return
		}
		s.mergeRecoveredSnapshot(normalizeSnapshot(entries))
		s.recoverNext()
	})
}

func (s *TrafficStore) recoverDroppedLiveUpdates() {
	if !s.surfaceActive {
		return
	}
	s.resetLiveReconciliation()
	s.recoverBodyView()
}

// SetSurfaceActive records whether the live capture is on screen. While it is
// not, events are skipped and a snapshot is merged on return.
func (s *TrafficStore) SetSurfaceActive(active bool) {
	if s.surfaceActive == active {
		return
	}
	s.surfaceActive = active
	if !active {
		s.resyncRequested = true
		s.terminalRefresh.Cancel()
		s.bodyToken++
		s.BodyViewLoading = false
		s.resetLiveReconciliation()
		s.syncDetail()
		return
	}
	s.requestRecovery()
	s.syncDetail()
}

func setBoundedEntry(target *orderedMap[*logic.TrafficEntry], entry *logic.TrafficEntry) *logic.TrafficEntry {
	var evicted *logic.TrafficEntry
	if !target.Has(entry.ID) && target.Len() >= TrafficEntryCap {
		if _, oldest, ok := target.Oldest(); ok {
			evicted = oldest
			target.DeleteOldest()
			target.compact()
		}
	}
	target.Set(entry.ID, entry)
	return evicted
}

func addBoundedDeleted(target *orderedMap[struct{}], id uint64) {
	if !target.Has(id) && target.Len() >= TrafficEntryCap {
		target.DeleteOldest()
		target.compact()
	}
	target.Set(id, struct{}{})
}

func bufferInitialPatch(buffer *initialTrafficBuffer, patch *logic.TrafficPatch) {
	if buffer.deletedIDs.Has(patch.TrafficID) {
		return
	}
	patches, ok := buffer.patches.Get(patch.TrafficID)
	if !ok && buffer.patches.Len() >= TrafficEntryCap {
		buffer.patches.DeleteOldest()
		buffer.patches.compact()
	}
	patches = append(patches, patch)
	if over := len(patches) - trafficPatchBufferPerEntry; over > 0 {
		patches = patches[over:]
	}
	buffer.patches.Set(patch.TrafficID, patches)
}

// advanceEviction moves the eviction watermark past evicted IDs and forgets
// the highlights of entries that left the window. Call it after Entries no
// longer holds the evicted entries.
func (s *TrafficStore) advanceEviction(evicted ...uint64) {
	s.evictedThrough = logic.AdvanceEvictionWatermark(s.evictedThrough, evicted...)
	for id := range s.Highlights {
		// A held window can still show entries at or below the watermark, and
		// the watermark can pass an entry before the window lets it go.
		if logic.IsTrafficEntryEvicted(id, s.evictedThrough) && !s.holdsEntry(id) {
			delete(s.Highlights, id)
		}
	}
}

func (s *TrafficStore) holdsEntry(id uint64) bool {
	_, found := slices.BinarySearchFunc(s.Entries, id, func(entry *logic.TrafficEntry, id uint64) int {
		return cmp.Compare(entry.ID, id)
	})
	return found
}

func (s *TrafficStore) outsideWindow(entry *logic.TrafficEntry) bool {
	return logic.IsTrafficEntryEvicted(entry.ID, s.evictedThrough)
}

func (s *TrafficStore) resetEntryBatch() {
	if s.stopBatch != nil {
		s.stopBatch()
		s.stopBatch = nil
	}
	s.pending.Clear()
}

func (s *TrafficStore) applyBatch(batch []*logic.TrafficEntry) {
	if len(batch) == 0 {
		return
	}
	changed, needsSort := false, false
	for _, entry := range batch {
		if position, ok := s.index[entry.ID]; ok {
			current := s.Entries[position]
			if current.Revision > 0 && entry.Revision < current.Revision {
				continue
			}
			s.Entries[position] = entry
			changed = true
			if s.selectedID() == entry.ID {
				s.Selected = entry
			}
			continue
		}
		if s.outsideWindow(entry) {
			continue
		}
		if s.paused && len(s.Entries) >= TrafficEntryCap {
			s.queuePaused(entry)
			continue
		}
		if count := len(s.Entries); count > 0 && entry.ID < s.Entries[count-1].ID {
			needsSort = true
		}
		s.index[entry.ID] = len(s.Entries)
		s.Entries = append(s.Entries, entry)
		changed = true
	}
	// Snapshot recovery can fill gaps before entries already received; ordered
	// live appends and patches do not need sorting.
	if needsSort {
		s.Entries = slices.Clone(s.Entries)
		slices.SortFunc(s.Entries, func(a, b *logic.TrafficEntry) int { return cmp.Compare(a.ID, b.ID) })
	}
	reindex := needsSort
	if over := len(s.Entries) - TrafficEntryCap; over > 0 {
		evicted := s.Entries[over-1].ID
		s.Entries = slices.Clone(s.Entries[over:])
		s.advanceEviction(evicted)
		reindex = true
		s.EvictionVersion++
	}
	if reindex {
		s.rebuildIndex()
	}
	if changed {
		s.entriesChanged()
	}
	s.scheduleStatistics()
}

// FlushPending applies the entries waiting for the next batch.
func (s *TrafficStore) FlushPending() {
	if s.stopBatch != nil {
		s.stopBatch()
		s.stopBatch = nil
	}
	if s.pending.Len() == 0 {
		return
	}
	batch := s.pending.Values()
	s.pending.Clear()
	s.applyBatch(batch)
}

// AddOrUpdate queues an entry for the next batch.
func (s *TrafficStore) AddOrUpdate(entry *logic.TrafficEntry) {
	if pending, ok := s.pending.Get(entry.ID); ok && pending.Revision > 0 && entry.Revision <= pending.Revision {
		return
	}
	var current *logic.TrafficEntry
	if position, ok := s.index[entry.ID]; ok {
		current = s.Entries[position]
	} else if paused, ok := s.pausedEntries.Get(entry.ID); ok {
		current = paused
	}
	if current != nil && current.Revision > 0 && entry.Revision <= current.Revision {
		return
	}
	if evicted := setBoundedEntry(s.pending, entry); evicted != nil {
		if _, retained := s.index[evicted.ID]; !retained {
			s.advanceEviction(evicted.ID)
		}
	}
	if s.stopBatch != nil {
		return
	}
	s.stopBatch = s.clock.After(trafficEntryBatchInterval, func() {
		s.stopBatch = nil
		s.FlushPending()
	})
}

func (s *TrafficStore) updatePatch(patch *logic.TrafficPatch) {
	if pending, ok := s.pending.Get(patch.TrafficID); ok {
		updated := logic.ApplyTrafficPatch(pending, patch)
		if updated != pending {
			s.pending.Set(patch.TrafficID, updated)
			s.refreshBodyForTerminalMessages(pending, updated)
		}
		return
	}
	if paused, ok := s.pausedEntries.Get(patch.TrafficID); ok {
		if updated := logic.ApplyTrafficPatch(paused, patch); updated != paused {
			s.pausedEntries.Set(patch.TrafficID, updated)
		}
		return
	}
	position, ok := s.index[patch.TrafficID]
	if !ok {
		return
	}
	entry := s.Entries[position]
	if updated := logic.ApplyTrafficPatch(entry, patch); updated != entry {
		s.refreshBodyForTerminalMessages(entry, updated)
		s.AddOrUpdate(updated)
	}
}

func (s *TrafficStore) queuePaused(entry *logic.TrafficEntry) {
	if s.outsideWindow(entry) {
		return
	}
	if current, ok := s.pausedEntries.Get(entry.ID); ok && current.Revision > entry.Revision {
		return
	}
	if evicted := setBoundedEntry(s.pausedEntries, entry); evicted != nil {
		s.advanceEviction(evicted.ID)
	}
}

// PauseLiveEviction holds the current window while it is being read.
func (s *TrafficStore) PauseLiveEviction() { s.paused = true }

// ResumeLiveEviction releases the window and applies the queued requests.
func (s *TrafficStore) ResumeLiveEviction() {
	s.paused = false
	queued := s.pausedEntries.Values()
	s.pausedEntries.Clear()
	s.applyBatch(queued)
	s.FlushPending()
}

// Delete removes entries here and in the backend.
func (s *TrafficStore) Delete(ids []uint64) {
	idSet := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	if len(idSet) == 0 {
		return
	}
	recoverAfter := s.resyncRequested || s.resyncInFlight
	s.recoveryEpoch++
	backendIDs := make([]int64, 0, len(idSet))
	for id := range idSet {
		backendIDs = append(backendIDs, int64(id))
		if s.initial != nil {
			s.initial.entries.Delete(id)
			s.initial.patches.Delete(id)
			addBoundedDeleted(s.initial.deletedIDs, id)
		}
		s.pending.Delete(id)
		s.pausedEntries.Delete(id)
		delete(s.Highlights, id)
	}
	s.Entries = slices.DeleteFunc(slices.Clone(s.Entries), func(entry *logic.TrafficEntry) bool { return idSet[entry.ID] })
	s.entriesChanged()
	s.rebuildIndex()

	if s.Selected != nil && idSet[s.Selected.ID] {
		s.terminalRefresh.Cancel()
		s.bodyToken++
		s.Selected = nil
		if s.BodyView != nil {
			s.setBodyView(nil)
		}
		s.BodyViewLoading = false
		s.loadedBodyEntry = 0
		s.ShowDetailPanel = false
		s.resetLiveReconciliation()
		s.syncDetail()
	}
	s.deleteSequence++
	runtime.Run(s.rt.Jobs, fmt.Sprintf("traffic:delete:%d", s.deleteSequence), func(context.Context) (struct{}, error) {
		s.backend.DeleteTraffic(backendIDs)
		return struct{}{}, nil
	}, func(struct{}, error) {
		if recoverAfter {
			s.requestRecovery()
		}
		s.scheduleStatistics()
	})
}

// SetHighlight colors a row; an empty color removes the highlight.
func (s *TrafficStore) SetHighlight(id uint64, color string) {
	if color != "" {
		s.Highlights[id] = color
		return
	}
	delete(s.Highlights, id)
}

// FocusEntry asks the table to reveal an entry.
func (s *TrafficStore) FocusEntry(id uint64) { s.PendingFocusID = id }

// ClearPendingFocus forgets a focus request.
func (s *TrafficStore) ClearPendingFocus() { s.PendingFocusID = 0 }

func asEntry(data any) *logic.TrafficEntry {
	entry, _ := data.(*logic.TrafficEntry)
	return entry
}

func asPatch(data any) *logic.TrafficPatch {
	switch patch := data.(type) {
	case logic.TrafficPatch:
		return &patch
	case *logic.TrafficPatch:
		return patch
	default:
		return nil
	}
}

func (s *TrafficStore) releaseListeners() {
	for _, off := range s.offs {
		off()
	}
	s.offs = nil
}

// Initialize subscribes to live events, then loads a backend snapshot and
// merges what arrived meanwhile. done, if set, hears a failure to load.
func (s *TrafficStore) Initialize(done func(error)) {
	s.generation++
	generation := s.generation
	s.resetEntryBatch()
	s.releaseListeners()
	s.liveReady = false

	buffer := &initialTrafficBuffer{
		active: true, entries: newOrderedMap[*logic.TrafficEntry](),
		patches: newOrderedMap[[]*logic.TrafficPatch](), deletedIDs: newOrderedMap[struct{}](),
	}
	s.initial = buffer

	s.offs = []func(){
		s.router.Subscribe(eventTrafficReset, func(any) {
			if generation == s.generation {
				s.ResetState()
			}
		}, nil),
		s.router.Subscribe(eventTrafficEntry, func(data any) {
			if generation != s.generation {
				return
			}
			if !s.surfaceActive {
				s.resyncRequested = true
				return
			}
			entry := asEntry(data)
			if entry == nil {
				return
			}
			if !buffer.active {
				s.AddOrUpdate(entry)
				return
			}
			if buffer.deletedIDs.Has(entry.ID) {
				return
			}
			if evicted := setBoundedEntry(buffer.entries, entry); evicted != nil {
				buffer.patches.Delete(evicted.ID)
				buffer.evicted = logic.AdvanceEvictionWatermark(buffer.evicted, evicted.ID)
			}
		}, func(uint64) { s.requestRecoveryFor(generation) }),
		s.router.Subscribe(eventTrafficPatch, func(data any) {
			if generation != s.generation {
				return
			}
			if !s.surfaceActive {
				s.resyncRequested = true
				return
			}
			patch := asPatch(data)
			if patch == nil {
				return
			}
			if buffer.active {
				bufferInitialPatch(buffer, patch)
				return
			}
			s.updatePatch(patch)
		}, func(uint64) { s.requestRecoveryFor(generation) }),
		s.router.Subscribe(eventTrafficLiveUpdate, func(data any) {
			if generation == s.generation {
				s.handleLiveUpdate(data)
			}
		}, func(uint64) {
			if generation == s.generation {
				s.recoverDroppedLiveUpdates()
			}
		}),
	}
	s.loadInitialSnapshot(generation, buffer, done)
}

func (s *TrafficStore) requestRecoveryFor(generation uint64) {
	if generation == s.generation {
		s.requestRecovery()
	}
}

var errTrafficSnapshotStale = errors.New("traffic snapshot superseded")

func (s *TrafficStore) loadInitialSnapshot(generation uint64, buffer *initialTrafficBuffer, done func(error)) {
	epoch := s.snapshotEpoch
	runtime.Run(s.rt.Jobs, jobTrafficInitial, func(context.Context) ([]*logic.TrafficEntry, error) {
		return s.backend.GetTraffic(), nil
	}, func(entries []*logic.TrafficEntry, err error) {
		switch {
		case generation != s.generation:
			err = errTrafficSnapshotStale
		case err != nil:
			buffer.active = false
			s.releaseListeners()
			if s.initial == buffer {
				s.initial = nil
			}
			s.resetEntryBatch()
		case epoch != s.snapshotEpoch:
			s.loadInitialSnapshot(generation, buffer, done)
			return
		default:
			s.finishInitialSnapshot(generation, buffer, normalizeSnapshot(entries))
		}
		if done != nil && !errors.Is(err, errTrafficSnapshotStale) {
			done(err)
		}
	})
}

func (s *TrafficStore) finishInitialSnapshot(generation uint64, buffer *initialTrafficBuffer, snapshot normalizedSnapshot) {
	s.Entries = slices.DeleteFunc(snapshot.entries, func(entry *logic.TrafficEntry) bool { return buffer.deletedIDs.Has(entry.ID) })
	s.advanceEviction(snapshot.evicted, buffer.evicted)
	s.entriesChanged()
	s.rebuildIndex()

	var buffered []*logic.TrafficEntry
	buffer.entries.Range(func(id uint64, entry *logic.TrafficEntry) {
		if buffer.deletedIDs.Has(id) {
			return
		}
		position, inSnapshot := s.index[id]
		if !inSnapshot && s.outsideWindow(entry) {
			return
		}
		if inSnapshot {
			entry = mergeInitialEntry(s.Entries[position], entry)
		}
		buffered = append(buffered, entry)
	})
	buffer.active = false
	if s.initial == buffer {
		s.initial = nil
	}
	s.applyBatch(buffered)
	buffer.patches.Range(func(id uint64, patches []*logic.TrafficPatch) {
		if buffer.deletedIDs.Has(id) {
			return
		}
		for _, patch := range patches {
			s.updatePatch(patch)
		}
	})
	s.FlushPending()
	s.reconcileSelected()
	s.updateStatistics()

	if generation != s.generation {
		return
	}
	s.liveReady = true
	if s.Selected != nil && s.ShowDetailPanel && logic.CapabilitiesOf(s.Selected).CanSubscribeLiveDetail {
		s.setDesiredLive(s.Selected.ID)
	} else {
		s.setDesiredLive(0)
	}
	if s.resyncRequested {
		s.recoverSnapshot()
	}
}

// Cleanup stops listening and abandons work in progress.
func (s *TrafficStore) Cleanup() {
	s.generation++
	s.rt.Jobs.Cancel(jobTrafficStatistics)
	s.bodyToken++
	s.terminalRefresh.Cancel()
	s.BodyViewLoading = false
	s.initial = nil
	s.resyncRequested = false
	s.resetEntryBatch()
	if s.stopStats != nil {
		s.stopStats()
		s.stopStats = nil
	}
	s.paused = false
	s.pausedEntries.Clear()
	s.setDesiredLive(0)
	s.releaseListeners()
	s.resetLiveReconciliation()
}
