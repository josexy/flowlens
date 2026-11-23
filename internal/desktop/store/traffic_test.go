package store

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/serviceruntime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

type fakeTimer struct {
	at      time.Duration
	fn      func()
	stopped bool
}

type fakeClock struct {
	now    time.Duration
	timers []*fakeTimer
}

func (c *fakeClock) After(d time.Duration, fn func()) func() {
	timer := &fakeTimer{at: c.now + d, fn: fn}
	c.timers = append(c.timers, timer)
	return func() { timer.stopped = true }
}

func (c *fakeClock) tick(d time.Duration) {
	c.now += d
	for {
		index := slices.IndexFunc(c.timers, func(timer *fakeTimer) bool { return timer.at <= c.now })
		if index < 0 {
			return
		}
		timer := c.timers[index]
		c.timers = slices.Delete(c.timers, index, index+1)
		if !timer.stopped {
			timer.fn()
		}
	}
}

type fakeTrafficBackend struct {
	mu        sync.Mutex
	snapshot  []*logic.TrafficEntry
	gate      chan struct{}
	stats     proxyservice.TrafficStatistics
	statsGate chan struct{}
	bodies    map[uint64]*proxyservice.TrafficBodyView
	bodyErr   error
	bodyCalls int
	live      []uint64
	deleted   [][]uint64
	cleared   int
}

func (f *fakeTrafficBackend) GetTraffic() []*logic.TrafficEntry {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.snapshot)
}

func (f *fakeTrafficBackend) GetStatistics() proxyservice.TrafficStatistics {
	f.mu.Lock()
	gate := f.statsGate
	stats := f.stats
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return stats
}

func (f *fakeTrafficBackend) ClearTraffic() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return nil
}

func (f *fakeTrafficBackend) DeleteTraffic(ids []uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids)
}

func (f *fakeTrafficBackend) SetLiveTrafficDetail(id uint64) { f.live = append(f.live, id) }

func (f *fakeTrafficBackend) GetTrafficBodyView(id uint64) (*proxyservice.TrafficBodyView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodyCalls++
	if f.bodyErr != nil {
		return nil, f.bodyErr
	}
	view, ok := f.bodies[id]
	if !ok {
		return &proxyservice.TrafficBodyView{}, nil
	}
	copied := *view
	return &copied, nil
}

func (f *fakeTrafficBackend) setSnapshot(entries []*logic.TrafficEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = entries
}

type trafficHarness struct {
	store   *TrafficStore
	backend *fakeTrafficBackend
	clock   *fakeClock
	router  *EventRouter
	rt      Runtime
	window  *runtimetest.Window
}

func (h *trafficHarness) settle() {
	for range 8 {
		h.rt.Jobs.Wait()
		h.window.Drain()
	}
}

func (h *trafficHarness) tick() {
	h.clock.tick(100 * time.Millisecond)
}

func (h *trafficHarness) emit(events ...proxyservice.LiveEvent) {
	h.router.Dispatch(proxyservice.LiveBatch{Events: events})
}

func (h *trafficHarness) drop(kind proxyservice.LiveEventKind) {
	h.router.Dispatch(proxyservice.LiveBatch{Dropped: map[proxyservice.LiveEventKind]uint64{kind: 1}})
	h.settle()
}

func trafficEntry(id uint64) *logic.TrafficEntry {
	return &logic.TrafficEntry{ID: id, Revision: 1, Type: "http", Method: "GET", Host: "example.test"}
}

func withStatus(entry *logic.TrafficEntry, status int) *logic.TrafficEntry {
	entry.StatusCode = status
	return entry
}

func withRevision(entry *logic.TrafficEntry, revision uint64) *logic.TrafficEntry {
	entry.Revision = revision
	return entry
}

func entryRange(first, count int) []*logic.TrafficEntry {
	entries := make([]*logic.TrafficEntry, count)
	for i := range entries {
		entries[i] = trafficEntry(uint64(first + i))
	}
	return entries
}

func newTrafficHarnessIdle(t *testing.T, initial []*logic.TrafficEntry) *trafficHarness {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	backend := &fakeTrafficBackend{snapshot: initial, bodies: map[uint64]*proxyservice.TrafficBodyView{}}
	clock := &fakeClock{}
	router := NewEventRouter()
	return &trafficHarness{
		store:   NewTrafficStore(rt, backend, router, clock),
		backend: backend, clock: clock, router: router, rt: rt, window: window,
	}
}

func newTrafficHarness(t *testing.T, initial []*logic.TrafficEntry) *trafficHarness {
	t.Helper()
	h := newTrafficHarnessIdle(t, initial)
	h.store.Initialize(nil)
	h.settle()
	return h
}

func entryIDs(entries []*logic.TrafficEntry) []uint64 {
	ids := make([]uint64, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}

func idRange(first, count int) []uint64 {
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = uint64(first + i)
	}
	return ids
}

func requireIDs(t *testing.T, entries []*logic.TrafficEntry, want []uint64) {
	t.Helper()
	got := entryIDs(entries)
	if !slices.Equal(got, want) {
		if len(got) > 12 {
			t.Fatalf("got %d ids from %d to %d, want %d ids from %d to %d", len(got), got[0], got[len(got)-1], len(want), want[0], want[len(want)-1])
		}
		t.Fatalf("got %v, want %v", got, want)
	}
}

func createdPatch(id, revision uint64) *logic.TrafficPatch {
	return &logic.TrafficPatch{
		TrafficID: id, Revision: revision,
		ResponseHeaders: &proxyservice.TrafficResponseHeadersPatch{StatusCode: 201, Status: "Created", Proto: "HTTP/1.1", HeaderFields: []logic.HeaderField{}},
	}
}

func TestReadingRetainsWindowAndBoundsQueue(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 5000))
	h.store.PauseLiveEviction()
	for batch := range 10 {
		for i := 1; i <= 1000; i++ {
			h.store.AddOrUpdate(trafficEntry(uint64(5000 + batch*1000 + i)))
		}
		h.tick()
	}
	if len(h.store.Entries) != 5000 || h.store.Entries[0].ID != 1 || h.store.PendingLiveEntryCount() != 5000 || h.store.EvictionVersion != 0 {
		t.Fatalf("len %d first %d queued %d eviction %d", len(h.store.Entries), h.store.Entries[0].ID, h.store.PendingLiveEntryCount(), h.store.EvictionVersion)
	}
	h.store.ResumeLiveEviction()
	if h.store.PendingLiveEntryCount() != 0 || h.store.EvictionVersion != 1 {
		t.Fatal("queue should drain with one eviction")
	}
	requireIDs(t, h.store.Entries, idRange(10001, 5000))
}

func TestQueuedRequestsKeepPatchesAndRejectStaleReplacements(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 5000))
	h.store.PauseLiveEviction()
	h.store.AddOrUpdate(trafficEntry(5001))
	h.tick()
	h.emit(proxyservice.TrafficPatchEvent{Patch: *createdPatch(5001, 3)})
	h.store.AddOrUpdate(withRevision(trafficEntry(5001), 2))
	h.emit(proxyservice.TrafficPatchEvent{Patch: logic.TrafficPatch{TrafficID: 5001, Revision: 4, Error: &proxyservice.TrafficError{Error: "connection closed"}}})
	h.tick()
	h.store.AddOrUpdate(trafficEntry(5002))
	h.store.ResumeLiveEviction()
	entries := h.store.Entries
	requireIDs(t, entries[len(entries)-2:], []uint64{5001, 5002})
	if queued := entries[len(entries)-2]; queued.StatusCode != 201 || queued.Revision != 4 {
		t.Fatalf("status %d revision %d", queued.StatusCode, queued.Revision)
	}
	if h.store.PendingLiveEntryCount() != 0 {
		t.Fatal("queue not empty")
	}
}

func TestLiveRequestsStayInIDOrder(t *testing.T) {
	h := newTrafficHarness(t, []*logic.TrafficEntry{trafficEntry(108)})
	h.emit(proxyservice.TrafficEntryEvent{Entry: withStatus(trafficEntry(111), 200)})
	h.emit(proxyservice.TrafficEntryEvent{Entry: withStatus(trafficEntry(110), 500)})
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{108, 110, 111})

	h.store.Select(h.store.Entries[1])
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(109)})
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(112)})
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{108, 109, 110, 111, 112})
	if h.store.Selected.ID != 110 {
		t.Fatalf("selected %d", h.store.Selected.ID)
	}

	// Inserting an earlier ID must also refresh the positions used by patches.
	h.emit(proxyservice.TrafficPatchEvent{Patch: *createdPatch(110, 2)})
	h.emit(proxyservice.TrafficEntryEvent{Entry: withStatus(trafficEntry(110), 500)})
	h.tick()
	if h.store.Entries[2].StatusCode != 201 || h.store.Selected.StatusCode != 201 || h.store.Entries[3].StatusCode != 200 {
		t.Fatalf("status %d selected %d next %d", h.store.Entries[2].StatusCode, h.store.Selected.StatusCode, h.store.Entries[3].StatusCode)
	}
	requireIDs(t, h.store.Entries, []uint64{108, 109, 110, 111, 112})

	sorter := logic.NewTrafficTableSorter()
	requireIDs(t, sorter.Sort(h.store.Entries, logic.TrafficSort{Key: logic.ColumnID, Order: logic.SortDesc}), []uint64{112, 111, 110, 109, 108})
	requireIDs(t, sorter.Sort(h.store.Entries, logic.TrafficSort{Key: logic.ColumnStatusCode, Order: logic.SortAsc}), []uint64{108, 109, 112, 111, 110})
}

func TestSnapshotsUseIDOrder(t *testing.T) {
	h := newTrafficHarness(t, []*logic.TrafficEntry{trafficEntry(3), trafficEntry(1), trafficEntry(2)})
	requireIDs(t, h.store.Entries, []uint64{1, 2, 3})
	h.store.AddOrUpdate(trafficEntry(6))
	h.store.AddOrUpdate(trafficEntry(5))
	h.tick()
	h.backend.setSnapshot([]*logic.TrafficEntry{trafficEntry(3), trafficEntry(1), trafficEntry(4), trafficEntry(2)})
	h.drop(proxyservice.LiveTrafficEntry)
	requireIDs(t, h.store.Entries, []uint64{1, 2, 3, 4, 5, 6})
}

func TestEventsBufferedDuringLoadMergeInOrder(t *testing.T) {
	h := newTrafficHarnessIdle(t, []*logic.TrafficEntry{trafficEntry(111), trafficEntry(108), trafficEntry(110)})
	gate := make(chan struct{})
	h.backend.gate = gate
	h.store.Initialize(nil)
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(112)})
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(109)})
	close(gate)
	h.settle()
	requireIDs(t, h.store.Entries, []uint64{108, 109, 110, 111, 112})
}

func TestPatchesBufferedDuringLoadApplyToSnapshot(t *testing.T) {
	h := newTrafficHarnessIdle(t, []*logic.TrafficEntry{trafficEntry(1)})
	gate := make(chan struct{})
	h.backend.gate = gate
	h.store.Initialize(nil)
	h.emit(proxyservice.TrafficPatchEvent{Patch: *createdPatch(1, 2)})
	close(gate)
	h.settle()
	if h.store.Entries[0].StatusCode != 201 {
		t.Fatalf("status %d", h.store.Entries[0].StatusCode)
	}
}

func TestDeleteDuringLoadWins(t *testing.T) {
	h := newTrafficHarnessIdle(t, []*logic.TrafficEntry{trafficEntry(1), trafficEntry(2)})
	gate := make(chan struct{})
	h.backend.gate = gate
	h.store.Initialize(nil)
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)})
	h.store.Delete([]uint64{1})
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)})
	close(gate)
	h.settle()
	requireIDs(t, h.store.Entries, []uint64{2})
}

func TestOversizedSnapshotsTrimLowestIDs(t *testing.T) {
	snapshot := entryRange(1, 5001)
	slices.Reverse(snapshot)
	h := newTrafficHarness(t, snapshot)
	requireIDs(t, h.store.Entries, idRange(2, 5000))
	h.store.AddOrUpdate(trafficEntry(5002))
	h.tick()
	if h.store.Entries[len(h.store.Entries)-1].ID != 5002 || h.store.Entries[0].ID != 3 {
		t.Fatalf("first %d", h.store.Entries[0].ID)
	}
	h.emit(proxyservice.TrafficResetEvent{})
	h.store.AddOrUpdate(trafficEntry(2))
	h.store.AddOrUpdate(trafficEntry(1))
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{1, 2})
}

func TestLateRequestsCannotReturnAfterEviction(t *testing.T) {
	h := newTrafficHarness(t, entryRange(2, 5000))
	h.store.AddOrUpdate(trafficEntry(1))
	h.tick()
	requireIDs(t, h.store.Entries, idRange(2, 5000))
	h.store.AddOrUpdate(withRevision(trafficEntry(1), 2))
	h.store.AddOrUpdate(trafficEntry(5002))
	h.tick()
	requireIDs(t, h.store.Entries, idRange(3, 5000))
}

func TestOrderedPendingKeepsLatest5000(t *testing.T) {
	h := newTrafficHarness(t, nil)
	for id := 1; id <= 5001; id++ {
		h.store.AddOrUpdate(trafficEntry(uint64(id)))
	}
	h.tick()
	requireIDs(t, h.store.Entries, idRange(2, 5000))
}

func TestRecoveryMergesMissingIDsBeforeQueuedEntries(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 5000))
	h.store.PauseLiveEviction()
	for id := 5002; id <= 10000; id++ {
		h.store.AddOrUpdate(trafficEntry(uint64(id)))
		if id%100 == 0 {
			h.tick()
		}
	}
	h.tick()
	h.store.AddOrUpdate(withStatus(withRevision(trafficEntry(5002), 3), 201))
	h.tick()
	h.backend.setSnapshot(entryRange(5001, 5000))
	h.drop(proxyservice.LiveTrafficEntry)
	if h.store.Entries[0].ID != 1 || h.store.PendingLiveEntryCount() != 5000 {
		t.Fatalf("first %d queued %d", h.store.Entries[0].ID, h.store.PendingLiveEntryCount())
	}
	h.store.AddOrUpdate(trafficEntry(10001))
	h.tick()
	h.store.ResumeLiveEviction()
	requireIDs(t, h.store.Entries, idRange(5002, 5000))
	if h.store.Entries[0].StatusCode != 201 {
		t.Fatalf("status %d", h.store.Entries[0].StatusCode)
	}
}

func TestRecoveryAndResetRespectHeldWindow(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 5000))
	h.store.PauseLiveEviction()
	h.backend.setSnapshot(entryRange(5001, 5000))
	h.drop(proxyservice.LiveTrafficEntry)
	if h.store.Entries[0].ID != 1 || h.store.PendingLiveEntryCount() != 5000 {
		t.Fatalf("first %d queued %d", h.store.Entries[0].ID, h.store.PendingLiveEntryCount())
	}
	h.store.ResetState()
	if len(h.store.Entries) != 0 || h.store.PendingLiveEntryCount() != 0 {
		t.Fatal("reset should empty")
	}
}

func TestDropOfPatchesConvergesOnSnapshot(t *testing.T) {
	h := newTrafficHarness(t, []*logic.TrafficEntry{trafficEntry(1)})
	h.backend.setSnapshot([]*logic.TrafficEntry{withStatus(withRevision(trafficEntry(1), 5), 204), trafficEntry(2)})
	h.drop(proxyservice.LiveTrafficPatch)
	requireIDs(t, h.store.Entries, []uint64{1, 2})
	if h.store.Entries[0].StatusCode != 204 {
		t.Fatalf("status %d", h.store.Entries[0].StatusCode)
	}
}

func TestInactiveSurfaceSkipsEventsAndRecoversOnReturn(t *testing.T) {
	h := newTrafficHarness(t, []*logic.TrafficEntry{trafficEntry(1)})
	h.store.SetSurfaceActive(false)
	h.emit(proxyservice.TrafficEntryEvent{Entry: trafficEntry(2)})
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{1})
	h.backend.setSnapshot([]*logic.TrafficEntry{trafficEntry(1), trafficEntry(2)})
	h.store.SetSurfaceActive(true)
	h.settle()
	requireIDs(t, h.store.Entries, []uint64{1, 2})
}

func TestResetInvalidatesInFlightStatistics(t *testing.T) {
	h := newTrafficHarness(t, nil)
	gate := make(chan struct{})
	h.backend.mu.Lock()
	h.backend.statsGate, h.backend.stats = gate, proxyservice.TrafficStatistics{Total: 42}
	h.backend.mu.Unlock()
	h.store.updateStatistics()
	h.store.ResetState()
	close(gate)
	h.settle()
	if h.store.Statistics.Total != 0 {
		t.Fatalf("total %d", h.store.Statistics.Total)
	}
}

func TestDeleteClearsSelectionAndCallsBackend(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 3))
	h.store.Select(h.store.Entries[1])
	h.store.SetShowDetailPanel(true)
	h.settle()
	h.store.SetHighlight(2, "#fff")
	h.store.Delete([]uint64{2})
	h.settle()
	requireIDs(t, h.store.Entries, []uint64{1, 3})
	if h.store.Selected != nil || h.store.ShowDetailPanel || h.store.BodyView != nil || len(h.store.Highlights) != 0 {
		t.Fatal("selection should clear")
	}
	if len(h.backend.deleted) != 1 || h.backend.deleted[0][0] != 2 {
		t.Fatalf("deleted %v", h.backend.deleted)
	}
	h.store.AddOrUpdate(trafficEntry(4))
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{1, 3, 4})
}

func TestRouterKeepsOrderAndReportsDrops(t *testing.T) {
	router := NewEventRouter()
	var received []uint64
	var dropped []uint64
	offEntry := router.OnTrafficEntry(func(entry *logic.TrafficEntry) { received = append(received, entry.ID) })
	offDrops := router.OnDropped(proxyservice.LiveTrafficEntry, func(count uint64) { dropped = append(dropped, count) })
	router.Dispatch(proxyservice.LiveBatch{
		Events: []proxyservice.LiveEvent{
			proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)},
			proxyservice.TrafficPatchEvent{Patch: logic.TrafficPatch{TrafficID: 9}},
			proxyservice.TrafficEntryEvent{Entry: trafficEntry(2)},
		},
		Dropped: map[proxyservice.LiveEventKind]uint64{proxyservice.LiveTrafficEntry: 3, proxyservice.LiveTrafficPatch: 5},
	})
	if !slices.Equal(received, []uint64{1, 2}) || !slices.Equal(dropped, []uint64{3}) {
		t.Fatalf("%v %v", received, dropped)
	}
	offEntry()
	offEntry()
	offDrops()
	router.Dispatch(proxyservice.LiveBatch{Events: []proxyservice.LiveEvent{proxyservice.TrafficEntryEvent{Entry: trafficEntry(4)}}})
	if len(received) != 2 || router.ListenerCount() != 0 {
		t.Fatal("still subscribed")
	}
}

func TestRouterDispatchesEveryKindInBatchOrder(t *testing.T) {
	router := NewEventRouter()
	var got []string
	router.OnTrafficEntry(func(entry *logic.TrafficEntry) { got = append(got, fmt.Sprint("entry ", entry.ID)) })
	router.OnTrafficPatch(func(patch *logic.TrafficPatch) { got = append(got, fmt.Sprint("patch ", patch.TrafficID)) })
	router.OnTrafficLiveUpdate(func(update proxyservice.TrafficLiveUpdate) { got = append(got, fmt.Sprint("live ", update.TrafficID)) })
	router.OnTrafficReset(func(generation uint64) { got = append(got, fmt.Sprint("reset ", generation)) })
	httpSink := func(event proxyservice.HTTPRequestStreamEvent) { got = append(got, "http "+event.SessionID) }
	wsSink := func(event proxyservice.WebSocketSessionEvent) { got = append(got, "ws "+event.SessionID) }
	for _, kind := range []proxyservice.LiveEventKind{proxyservice.LiveTrafficPatch, proxyservice.LiveTrafficEntry} {
		router.OnDropped(kind, func(count uint64) { got = append(got, fmt.Sprint("dropped ", kind, " ", count)) })
	}
	router.Dispatch(proxyservice.LiveBatch{
		Events: []proxyservice.LiveEvent{
			proxyservice.TrafficResetEvent{CaptureGeneration: 3},
			proxyservice.WebSocketEvent{Event: proxyservice.WebSocketSessionEvent{SessionID: "w"}, Sink: wsSink},
			proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)},
			proxyservice.HTTPStreamEvent{Event: proxyservice.HTTPRequestStreamEvent{SessionID: "h"}, Sink: httpSink},
			proxyservice.HTTPStreamEvent{Event: proxyservice.HTTPRequestStreamEvent{SessionID: "ignored"}},
			proxyservice.TrafficPatchEvent{Patch: logic.TrafficPatch{TrafficID: 1}},
			proxyservice.TrafficLiveUpdateEvent{Update: proxyservice.TrafficLiveUpdate{TrafficID: 1}},
		},
		Dropped: map[proxyservice.LiveEventKind]uint64{proxyservice.LiveTrafficPatch: 2, proxyservice.LiveTrafficEntry: 1},
	})
	want := []string{"reset 3", "ws w", "entry 1", "http h", "patch 1", "live 1", "dropped traffic entry 1", "dropped traffic patch 2"}
	if !slices.Equal(got, want) {
		t.Fatalf("delivery = %q, want %q", got, want)
	}
}

func TestResetMarkerClearsBeforeLaterEvents(t *testing.T) {
	h := newTrafficHarness(t, []*logic.TrafficEntry{trafficEntry(1)})
	h.emit(proxyservice.TrafficResetEvent{}, proxyservice.TrafficEntryEvent{Entry: withStatus(trafficEntry(1), 204)})
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{1})
	if h.store.Entries[0].StatusCode != 204 {
		t.Fatal("old entry survived the reset")
	}
}

func sseChunk(id uint64, offset int64, text string) proxyservice.TrafficLiveUpdate {
	return proxyservice.TrafficLiveUpdate{TrafficID: id, Kind: proxyservice.TrafficLiveUpdateSSEChunk, Offset: &offset, Chunk: []byte(text)}
}

func wsMessage(id uint64, index int) proxyservice.TrafficLiveUpdate {
	return proxyservice.TrafficLiveUpdate{TrafficID: id, Kind: proxyservice.TrafficLiveUpdateWebSocketMessage, MessageIndex: &index, Message: &proxyservice.WebSocketMessage{}}
}

func openDetail(t *testing.T, view *proxyservice.TrafficBodyView) *trafficHarness {
	t.Helper()
	h := newTrafficHarness(t, entryRange(1, 1))
	h.backend.bodies[1] = view
	h.store.Select(h.store.Entries[0])
	h.store.SetShowDetailPanel(true)
	h.settle()
	if h.store.BodyView == nil {
		t.Fatal("body not loaded")
	}
	return h
}

func TestLiveSSEReconcilesByByteOffset(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "hello"})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 8, " wo")})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 3, "lo, ")})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 0, "he")})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 7, "x")})
	h.tick()
	if h.store.BodyView.ResponseBody != "hello, x wo" {
		t.Fatalf("body %q", h.store.BodyView.ResponseBody)
	}
}

func TestLiveSSEJoinsSplitCharacters(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{})
	text := "汉"
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 0, text[:1])})
	h.tick()
	if h.store.BodyView.ResponseBody != "" {
		t.Fatalf("body %q", h.store.BodyView.ResponseBody)
	}
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 1, text[1:])})
	h.tick()
	if h.store.BodyView.ResponseBody != text {
		t.Fatalf("body %q", h.store.BodyView.ResponseBody)
	}
}

func TestLiveSSESwitchesBinarySnapshotToText(t *testing.T) {
	// The snapshot ends inside "é", so it was marked binary as invalid UTF-8.
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "abc\xc3", ResponseBodyBinary: true})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 4, "\xa9def")})
	h.tick()
	if h.store.BodyView.ResponseBody != "abcédef" || h.store.BodyView.ResponseBodyBinary {
		t.Fatalf("%+v", h.store.BodyView)
	}
}

func TestLiveGapAfterBinarySnapshotReloadsSnapshot(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "\x00\x01", ResponseBodyBinary: true})
	before := h.backend.bodyCalls
	h.backend.bodies[1] = &proxyservice.TrafficBodyView{ResponseBody: "data: x\n\n"}
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 5, "late")})
	h.tick()
	h.settle()
	if h.backend.bodyCalls != before+1 || h.store.BodyView.ResponseBody != "data: x\n\n" || h.store.BodyView.ResponseBodyBinary {
		t.Fatalf("calls %d view %+v", h.backend.bodyCalls, h.store.BodyView)
	}
}

func TestLiveGapReloadsSnapshot(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "abc"})
	before := h.backend.bodyCalls
	h.backend.bodies[1] = &proxyservice.TrafficBodyView{ResponseBody: "abcdefghij"}
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(1, 9, "j")})
	h.tick()
	h.settle()
	if h.backend.bodyCalls != before+1 || h.store.BodyView.ResponseBody != "abcdefghij" {
		t.Fatalf("calls %d body %q", h.backend.bodyCalls, h.store.BodyView.ResponseBody)
	}
}

func TestLiveWebSocketMessagesAndGap(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{WebSocketMessages: []*proxyservice.WebSocketMessage{{}}})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: wsMessage(1, 2)})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: wsMessage(1, 1)})
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: wsMessage(1, 0)})
	h.tick()
	if len(h.store.BodyView.WebSocketMessages) != 3 {
		t.Fatalf("messages %d", len(h.store.BodyView.WebSocketMessages))
	}
	before := h.backend.bodyCalls
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: wsMessage(1, 9)})
	h.tick()
	h.settle()
	if h.backend.bodyCalls != before+1 {
		t.Fatal("gap should reload")
	}
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: proxyservice.TrafficLiveUpdate{TrafficID: 1, Kind: proxyservice.TrafficLiveUpdateWebSocketTruncated}})
	h.tick()
	if !h.store.BodyView.WsMsgsTruncated {
		t.Fatal("truncated")
	}
}

func TestDroppedLiveUpdatesReload(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "a"})
	before := h.backend.bodyCalls
	h.drop(proxyservice.LiveTrafficLiveUpdate)
	if h.backend.bodyCalls != before+1 {
		t.Fatal("drop should reload")
	}
}

func TestLiveUpdatesForOtherEntriesAreIgnored(t *testing.T) {
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: "a"})
	version := h.store.BodyViewVersion
	h.emit(proxyservice.TrafficLiveUpdateEvent{Update: sseChunk(2, 0, "zz")})
	h.tick()
	if h.store.BodyViewVersion != version {
		t.Fatal("changed")
	}
}

func TestLiveSubscriptionFollowsThePanel(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 1))
	h.store.Select(h.store.Entries[0])
	if len(h.backend.live) != 0 {
		t.Fatalf("live %v", h.backend.live)
	}
	h.store.SetShowDetailPanel(true)
	h.settle()
	if !slices.Equal(h.backend.live, []uint64{1}) {
		t.Fatalf("live %v", h.backend.live)
	}
	h.store.SetShowDetailPanel(false)
	if !slices.Equal(h.backend.live, []uint64{1, 0}) || h.store.BodyView != nil {
		t.Fatalf("live %v", h.backend.live)
	}
}

func TestRawTCPDoesNotSubscribeOrLoad(t *testing.T) {
	entry := trafficEntry(1)
	entry.Type = "tcp"
	h := newTrafficHarness(t, []*logic.TrafficEntry{entry})
	h.store.Select(h.store.Entries[0])
	h.store.SetShowDetailPanel(true)
	h.settle()
	if len(h.backend.live) != 0 || h.backend.bodyCalls != 0 {
		t.Fatalf("live %v calls %d", h.backend.live, h.backend.bodyCalls)
	}
	done := false
	h.store.GetBodyView(1, func(view *proxyservice.TrafficBodyView) { done = view == nil })
	if !done {
		t.Fatal("raw TCP has no bodies")
	}
}

func TestTerminalPatchRefreshesBody(t *testing.T) {
	entry := trafficEntry(1)
	entry.Response = &proxyservice.HTTPMessage{Metrics: &proxyservice.HTTPMessageMetrics{State: proxyservice.HTTPMessageStatePending}}
	h := newTrafficHarness(t, []*logic.TrafficEntry{entry})
	h.store.Select(h.store.Entries[0])
	h.store.SetShowDetailPanel(true)
	h.settle()
	before := h.backend.bodyCalls
	h.emit(proxyservice.TrafficPatchEvent{Patch: logic.TrafficPatch{TrafficID: 1, Revision: 2, Metrics: &proxyservice.TrafficMetricsPatch{Response: &proxyservice.HTTPMessageMetrics{State: proxyservice.HTTPMessageStateCompleted}}}})
	h.settle()
	if h.backend.bodyCalls != before+1 {
		t.Fatalf("calls %d", h.backend.bodyCalls)
	}
}

func TestFailedBodyLoadClearsView(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 1))
	h.backend.bodyErr = errors.New("gone")
	h.store.Select(h.store.Entries[0])
	h.store.SetShowDetailPanel(true)
	h.settle()
	if h.store.BodyView != nil || h.store.BodyViewLoading {
		t.Fatal("failed load")
	}
}

func TestDecoderStripsBOMAndReplacesInvalidBytes(t *testing.T) {
	decoder := &utf8StreamDecoder{}
	if got := decoder.decode([]byte("\xef\xbb")); got != "" {
		t.Fatalf("%q", got)
	}
	if got := decoder.decode([]byte("\xbfhi\xff!")); got != "hi�!" {
		t.Fatalf("%q", got)
	}
}

func TestStatisticsAreDebounced(t *testing.T) {
	h := newTrafficHarness(t, nil)
	h.backend.mu.Lock()
	h.backend.stats = proxyservice.TrafficStatistics{Total: 7}
	h.backend.mu.Unlock()
	h.store.AddOrUpdate(trafficEntry(1))
	h.tick()
	h.settle()
	if h.store.Statistics.Total != 0 {
		t.Fatal("too early")
	}
	h.clock.tick(300 * time.Millisecond)
	h.settle()
	if h.store.Statistics.Total != 7 {
		t.Fatalf("total %d", h.store.Statistics.Total)
	}
}

func TestConnectDeliversBatchesOnTheMainThread(t *testing.T) {
	h := newTrafficHarness(t, nil)
	var live serviceruntime.Topic[proxyservice.LiveBatch]
	off := h.router.Connect(&live, h.rt.Dispatcher.Post)
	defer off()
	live.Publish(proxyservice.LiveBatch{Events: []proxyservice.LiveEvent{proxyservice.TrafficEntryEvent{Entry: trafficEntry(1)}}})
	h.settle()
	h.tick()
	requireIDs(t, h.store.Entries, []uint64{1})
}

func TestResetAndDropInOneBatchEndOnTheSnapshot(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 3))
	h.backend.setSnapshot(entryRange(10, 2))
	h.router.Dispatch(proxyservice.LiveBatch{
		Events:  []proxyservice.LiveEvent{proxyservice.TrafficResetEvent{}},
		Dropped: map[proxyservice.LiveEventKind]uint64{proxyservice.LiveTrafficEntry: 4},
	})
	h.settle()
	requireIDs(t, h.store.Entries, idRange(10, 2))
}

func TestLiveSSEAppendsChunksWithoutCopyingTheBody(t *testing.T) {
	const (
		snapshotSize = 1 << 20
		chunkSize    = 1 << 10
		chunks       = 1000
	)
	snapshot := strings.Repeat("s", snapshotSize)
	h := openDetail(t, &proxyservice.TrafficBodyView{ResponseBody: snapshot})
	updates := make([]proxyservice.TrafficLiveUpdate, chunks)
	var want strings.Builder
	want.WriteString(snapshot)
	for i := range updates {
		text := strings.Repeat(string(rune('a'+i%26)), chunkSize)
		updates[i] = sseChunk(1, int64(snapshotSize+i*chunkSize), text)
		want.WriteString(text)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, update := range updates {
		h.emit(proxyservice.TrafficLiveUpdateEvent{Update: update})
		h.tick()
	}
	runtime.ReadMemStats(&after)

	if h.store.BodyView.ResponseBody != want.String() {
		t.Fatalf("body has %d bytes, want the %d-byte concatenation", len(h.store.BodyView.ResponseBody), want.Len())
	}
	// Copying the body on every chunk would allocate over 1 GiB here. Apart
	// from seeding and growing the builder (a few times the final body), each
	// chunk must cost a bounded multiple of its own size.
	allocated := after.TotalAlloc - before.TotalAlloc
	seed := uint64(4 * (snapshotSize + chunks*chunkSize))
	if perChunk := (max(allocated, seed) - seed) / chunks; perChunk > 16*chunkSize {
		t.Fatalf("allocated %d bytes for %d chunks (%d per chunk beyond the seed)", allocated, chunks, perChunk)
	}
}

func TestEvictionForgetsHighlights(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, 1))
	h.store.SetHighlight(1, "red")
	for id := uint64(2); id <= TrafficEntryCap+2; id++ {
		h.store.AddOrUpdate(trafficEntry(id))
	}
	h.store.SetHighlight(TrafficEntryCap+2, "blue")
	h.tick()

	if h.store.holdsEntry(1) {
		t.Fatal("entry 1 should have left the window")
	}
	if _, ok := h.store.Highlights[1]; ok {
		t.Fatal("highlight of an evicted entry was kept")
	}
	if h.store.Highlights[TrafficEntryCap+2] != "blue" {
		t.Fatalf("highlight of a shown entry was removed: %v", h.store.Highlights)
	}
}

func TestHeldWindowKeepsHighlightsOfShownEntries(t *testing.T) {
	h := newTrafficHarness(t, entryRange(1, TrafficEntryCap))
	h.store.SetHighlight(1, "red")
	h.store.PauseLiveEviction()
	for id := uint64(TrafficEntryCap + 1); id <= 3*TrafficEntryCap; id++ {
		h.store.AddOrUpdate(trafficEntry(id))
		if id%1000 == 0 {
			h.tick()
		}
	}
	h.tick()
	if h.store.Highlights[1] != "red" {
		t.Fatal("highlight of an entry shown in the held window was removed")
	}
	h.store.ResumeLiveEviction()
	if _, ok := h.store.Highlights[1]; ok {
		t.Fatal("highlight was kept after the held window released its entry")
	}
}
