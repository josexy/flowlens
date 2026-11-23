package proxyservice

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// entryEvent is a traffic entry event identified only by its ID.
func entryEvent(id uint64) TrafficEntryEvent { return TrafficEntryEvent{Entry: &TrafficEntry{ID: id}} }

func TestLiveEventBatcherBatchesInOrder(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  1024,
	}, func(batch LiveBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	entry := entryEvent(1)
	if err := batcher.Publish(entry); err != nil {
		t.Fatalf("publish entry: %v", err)
	}
	if err := batcher.Publish(TrafficPatchEvent{Patch: TrafficEntryPatch{TrafficID: 1, Revision: 2}}); err != nil {
		t.Fatalf("publish patch: %v", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 2 {
			t.Fatalf("events = %d, want 2", len(batch.Events))
		}
		published, ok := batch.Events[0].(TrafficEntryEvent)
		if !ok || published.Entry != entry.Entry {
			t.Fatalf("first event = %#v, want the published entry", batch.Events[0])
		}
		if patch, ok := batch.Events[1].(TrafficPatchEvent); !ok || patch.Patch.Revision != 2 {
			t.Fatalf("second event = %#v, want the patch", batch.Events[1])
		}
		if len(batch.Dropped) != 0 {
			t.Fatalf("unexpected dropped events: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event batch")
	}
}

func TestLiveEventBatcherBoundsPendingEventsAndReportsDrops(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 1,
		MaxPendingBytes:  1024,
	}, func(batch LiveBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	if err := batcher.Publish(entryEvent(1)); err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if err := batcher.Publish(entryEvent(2)); !errors.Is(err, errLiveEventDropped) {
		t.Fatalf("second publish error = %v, want errLiveEventDropped", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 1 {
			t.Fatalf("events = %d, want 1", len(batch.Events))
		}
		if batch.Dropped[LiveTrafficEntry] != 1 || len(batch.Dropped) != 1 {
			t.Fatalf("dropped = %#v, want one traffic entry", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded event batch")
	}
}

func TestLiveEventBatcherPublishesUnboundedEventsBeyondConfiguredLimits(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 1,
		MaxPendingBytes:  128,
	}, func(batch LiveBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	if err := batcher.Publish(entryEvent(1)); err != nil {
		t.Fatalf("publish bounded event: %v", err)
	}
	if err := batcher.PublishUnbounded(HTTPStreamEvent{Event: HTTPRequestStreamEvent{Chunk: bytes.Repeat([]byte("h"), 256)}}); err != nil {
		t.Fatalf("publish unbounded HTTP Request event: %v", err)
	}
	if err := batcher.PublishUnbounded(WebSocketEvent{Event: WebSocketSessionEvent{Error: strings.Repeat("w", 256)}}); err != nil {
		t.Fatalf("publish unbounded WebSocket Session event: %v", err)
	}
	batcher.Close()

	select {
	case batch := <-batches:
		if len(batch.Events) != 3 {
			t.Fatalf("events = %d, want 3", len(batch.Events))
		}
		wantKinds := []LiveEventKind{LiveTrafficEntry, LiveHTTPStream, LiveWebSocket}
		for index, want := range wantKinds {
			if got := batch.Events[index].liveKind(); got != want {
				t.Fatalf("event[%d] = %s, want %s", index, got, want)
			}
		}
		if len(batch.Dropped) != 0 {
			t.Fatalf("unexpected dropped events: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unbounded event batch")
	}
}

func TestLiveEventBatcherDropsASingleEventLargerThanTheByteLimit(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  1024,
	}, func(batch LiveBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	oversized := TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1, Kind: TrafficLiveUpdateSSEChunk, Chunk: bytes.Repeat([]byte("x"), 1024)}}
	if err := batcher.Publish(oversized); !errors.Is(err, errLiveEventDropped) {
		t.Fatalf("publish error = %v, want errLiveEventDropped", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 0 {
			t.Fatalf("events = %d, want the oversized event dropped rather than delivered", len(batch.Events))
		}
		if batch.Dropped[LiveTrafficLiveUpdate] != 1 {
			t.Fatalf("dropped = %#v, want one traffic live update", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for oversized drop notification")
	}
}

func TestLiveEventBatcherReachesTheByteLimitBeforeTheEventLimit(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	batcher := newLiveEventBatcher(defaultLiveEventBatcherOptions(), func(batch LiveBatch) { batches <- batch })
	t.Cleanup(batcher.Close)

	// 64 KiB chunks fill the 4 MiB byte limit long before 512 events.
	chunk := TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1, Kind: TrafficLiveUpdateSSEChunk, Chunk: bytes.Repeat([]byte("x"), 64<<10)}}
	accepted, dropped := 0, 0
	for range 100 {
		switch err := batcher.Publish(chunk); {
		case err == nil:
			accepted++
		case errors.Is(err, errLiveEventDropped):
			dropped++
		default:
			t.Fatalf("publish: %v", err)
		}
	}
	wantAccepted := liveEventMaxBytes / chunk.liveSize()
	if accepted != wantAccepted || accepted >= liveEventMaxPending || dropped != 100-wantAccepted {
		t.Fatalf("accepted %d and dropped %d, want %d accepted under the byte limit", accepted, dropped, wantAccepted)
	}
	// Small events of another kind are dropped too while the bytes are spent.
	if err := batcher.Publish(TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1, Chunk: bytes.Repeat([]byte("y"), 64<<10)}}); !errors.Is(err, errLiveEventDropped) {
		t.Fatalf("publish after the byte limit = %v", err)
	}
	if err := batcher.Publish(TrafficPatchEvent{Patch: TrafficEntryPatch{TrafficID: 1, Error: &TrafficError{Error: strings.Repeat("e", 64<<10)}}}); !errors.Is(err, errLiveEventDropped) {
		t.Fatalf("patch after the byte limit = %v", err)
	}
	batcher.Close()
	batch := <-batches
	if len(batch.Events) != wantAccepted || batch.Dropped[LiveTrafficLiveUpdate] != uint64(dropped+1) || batch.Dropped[LiveTrafficPatch] != 1 {
		t.Fatalf("events = %d, dropped = %#v", len(batch.Events), batch.Dropped)
	}
}

func TestLiveEventBatcherKeepsTheByteLimitAcrossTypedPayloads(t *testing.T) {
	batches := make(chan LiveBatch, 1)
	chunk := TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1, Kind: TrafficLiveUpdateSSEChunk, Chunk: bytes.Repeat([]byte("x"), 200)}}
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 64,
		MaxPendingBytes:  2*chunk.liveSize() + chunk.liveSize()/2,
	}, func(batch LiveBatch) { batches <- batch })
	t.Cleanup(batcher.Close)

	if err := batcher.Publish(chunk); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if err := batcher.Publish(chunk); err != nil {
		t.Fatalf("second chunk: %v", err)
	}
	if err := batcher.Publish(chunk); !errors.Is(err, errLiveEventDropped) {
		t.Fatalf("third chunk error = %v, want errLiveEventDropped", err)
	}
	batcher.Close()
	batch := <-batches
	if len(batch.Events) != 2 || batch.Dropped[LiveTrafficLiveUpdate] != 1 {
		t.Fatalf("events = %d, dropped = %#v", len(batch.Events), batch.Dropped)
	}
}

func TestLiveEventBatcherPublishAfterCloseDoesNotBlock(t *testing.T) {
	batcher := newLiveEventBatcher(liveEventBatcherOptions{FlushInterval: time.Hour}, nil)
	batcher.Close()
	done := make(chan error, 1)
	go func() { done <- batcher.Publish(entryEvent(1)) }()
	select {
	case err := <-done:
		if !errors.Is(err, errLiveBatcherClosed) {
			t.Fatalf("publish error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("publish blocked after close")
	}
}

func TestLiveSizeCountsPayloadBytes(t *testing.T) {
	small := TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1}}.liveSize()
	large := TrafficLiveUpdateEvent{Update: TrafficLiveUpdate{TrafficID: 1, Chunk: bytes.Repeat([]byte("x"), 4096)}}.liveSize()
	if large-small != 4096 {
		t.Fatalf("large-small = %d, want 4096", large-small)
	}
	stream := HTTPStreamEvent{Event: HTTPRequestStreamEvent{Chunk: bytes.Repeat([]byte("x"), 4096)}}.liveSize()
	if stream-(HTTPStreamEvent{}).liveSize() != 4096 {
		t.Fatalf("stream chunk size = %d", stream)
	}
	message := WebSocketEvent{Event: WebSocketSessionEvent{Message: &WebSocketMessage{Data: strings.Repeat("m", 1000)}}}.liveSize()
	if message < 1000 {
		t.Fatalf("WebSocket message size = %d, want at least its data", message)
	}
	entry := TrafficEntryEvent{Entry: &TrafficEntry{URL: strings.Repeat("u", 100), Host: strings.Repeat("h", 20), Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: strings.Repeat("n", 50), Value: strings.Repeat("v", 30)}}}}}
	if got, want := entry.liveSize(), liveEventOverhead+100+20+liveFieldOverhead+50+30; got != want {
		t.Fatalf("entry size = %d, want %d", got, want)
	}
	patch := TrafficPatchEvent{Patch: TrafficEntryPatch{ResponseHeaders: &TrafficResponseHeadersPatch{HeaderFields: []HTTPHeaderField{{Name: "a", Value: strings.Repeat("b", 99)}}}}}
	if got, want := patch.liveSize(), liveEventOverhead+liveFieldOverhead+100; got != want {
		t.Fatalf("patch size = %d, want %d", got, want)
	}
}

type claimingBatchRecorder struct {
	batches  chan LiveBatch
	releases chan func()
	claim    bool
}

func newClaimingBatchRecorder(claim bool) *claimingBatchRecorder {
	return &claimingBatchRecorder{
		batches:  make(chan LiveBatch, 16),
		releases: make(chan func(), 16),
		claim:    claim,
	}
}

func (r *claimingBatchRecorder) emit(batch LiveBatch) {
	if r.claim {
		r.releases <- batch.Claim()
	}
	r.batches <- batch
}

func (r *claimingBatchRecorder) next(t *testing.T, within time.Duration) LiveBatch {
	t.Helper()
	select {
	case batch := <-r.batches:
		return batch
	case <-time.After(within):
		t.Fatal("timed out waiting for event batch")
		return LiveBatch{}
	}
}

func (r *claimingBatchRecorder) none(t *testing.T, during time.Duration) {
	t.Helper()
	select {
	case batch := <-r.batches:
		t.Fatalf("unexpected batch while the previous one is claimed: %#v", batch)
	case <-time.After(during):
	}
}

func eventIDs(batch LiveBatch) []uint64 {
	ids := make([]uint64, 0, len(batch.Events))
	for _, event := range batch.Events {
		ids = append(ids, event.(TrafficEntryEvent).Entry.ID)
	}
	return ids
}

func TestLiveEventBatcherDoesNotWaitForUnclaimedBatches(t *testing.T) {
	recorder := newClaimingBatchRecorder(false)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval: time.Millisecond,
	}, recorder.emit)
	t.Cleanup(batcher.Close)

	for id := range uint64(3) {
		if err := batcher.Publish(entryEvent(id)); err != nil {
			t.Fatalf("publish %d: %v", id, err)
		}
		if got := eventIDs(recorder.next(t, time.Second)); len(got) != 1 || got[0] != id {
			t.Fatalf("batch %d = %v", id, got)
		}
	}
}

func TestLiveEventBatcherHoldsBatchesWhileClaimed(t *testing.T) {
	recorder := newClaimingBatchRecorder(true)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 4,
		MaxPendingBytes:  1 << 20,
	}, recorder.emit)
	t.Cleanup(batcher.Close)

	if err := batcher.Publish(entryEvent(0)); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	recorder.next(t, time.Second)
	release := <-recorder.releases

	// A burst beyond the pending limit while the first batch is unreleased.
	var dropped int
	for id := uint64(1); id <= 10; id++ {
		if err := batcher.Publish(entryEvent(id)); errors.Is(err, errLiveEventDropped) {
			dropped++
		} else if err != nil {
			t.Fatalf("publish %d: %v", id, err)
		}
	}
	if dropped != 6 {
		t.Fatalf("dropped = %d, want the 6 events beyond the limit of 4", dropped)
	}
	recorder.none(t, 50*time.Millisecond)

	release()
	release() // Releasing twice is harmless.
	batch := recorder.next(t, time.Second)
	if got := eventIDs(batch); len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("events after release = %v, want [1 2 3 4]", got)
	}
	if batch.Dropped[LiveTrafficEntry] != 6 {
		t.Fatalf("dropped counts = %#v, want 6 traffic entries", batch.Dropped)
	}
}

func TestLiveEventBatcherKeepsClaimsBoundedDuringLongStalls(t *testing.T) {
	for _, byteBound := range []bool{false, true} {
		t.Run(fmt.Sprintf("byteBound=%t", byteBound), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := newClaimingBatchRecorder(true)
				options := liveEventBatcherOptions{FlushInterval: time.Millisecond, MaxPendingEvents: 4, MaxPendingBytes: 1 << 20}
				wantPending := 4
				if byteBound {
					options.MaxPendingEvents = 100
					options.MaxPendingBytes = 2 * entryEvent(0).liveSize()
					wantPending = 2
				}
				batcher := newLiveEventBatcher(options, recorder.emit)
				defer batcher.Close()
				if err := batcher.Publish(entryEvent(0)); err != nil {
					t.Fatal(err)
				}
				recorder.next(t, time.Second)
				release := <-recorder.releases
				for round := range 3 {
					for i := range 10 {
						_ = batcher.Publish(entryEvent(uint64(round*10 + i + 1)))
					}
					// Each interval exceeds the former two-second escape hatch.
					recorder.none(t, 3*time.Second)
				}
				release()
				batch := recorder.next(t, time.Second)
				if len(batch.Events) != wantPending || batch.Dropped[LiveTrafficEntry] != uint64(30-wantPending) {
					t.Fatalf("pending=%d dropped=%v, want %d and %d", len(batch.Events), batch.Dropped, wantPending, 30-wantPending)
				}
				for i, id := range eventIDs(batch) {
					if id != uint64(i+1) {
						t.Fatalf("event %d = %d, want publication order", i, id)
					}
				}
			})
		})
	}
}

func TestLiveEventBatcherCloseReturnsWhileWaitingForRelease(t *testing.T) {
	recorder := newClaimingBatchRecorder(true)
	batcher := newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval: time.Millisecond,
	}, recorder.emit)

	if err := batcher.Publish(entryEvent(1)); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	recorder.next(t, time.Second)
	if err := batcher.Publish(entryEvent(2)); err != nil {
		t.Fatalf("publish second: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		batcher.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on an unreleased batch")
	}
	select {
	case batch := <-recorder.batches:
		t.Fatalf("shutdown queued another batch behind an abandoned claim: %+v", batch)
	default:
	}
	if batcher.hasPending() {
		t.Fatal("shutdown kept pending payloads alive")
	}
}
