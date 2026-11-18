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

func TestFrontendEventBatcherBatchesInOrder(t *testing.T) {
	batches := make(chan FrontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  1024,
	}, func(batch FrontendEventBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	if err := batcher.Publish("traffic:entry", map[string]any{"id": 1}); err != nil {
		t.Fatalf("publish entry: %v", err)
	}
	if err := batcher.Publish("traffic:patch", map[string]any{"trafficId": 1, "revision": 2}); err != nil {
		t.Fatalf("publish patch: %v", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 2 {
			t.Fatalf("events = %d, want 2", len(batch.Events))
		}
		if batch.Events[0].Name != "traffic:entry" || batch.Events[1].Name != "traffic:patch" {
			t.Fatalf("event order = %#v", batch.Events)
		}
		entry, ok := batch.Events[0].Data.(map[string]any)
		if !ok || entry["id"] != 1 {
			t.Fatalf("entry = %#v, want the published value", batch.Events[0].Data)
		}
		if len(batch.Dropped) != 0 {
			t.Fatalf("unexpected dropped events: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event batch")
	}
}

func TestFrontendEventBatcherBoundsPendingEventsAndReportsDrops(t *testing.T) {
	batches := make(chan FrontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 1,
		MaxPendingBytes:  1024,
	}, func(batch FrontendEventBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	if err := batcher.Publish("traffic:entry", map[string]any{"id": 1}); err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if err := batcher.Publish("traffic:entry", map[string]any{"id": 2}); !errors.Is(err, errFrontendEventDropped) {
		t.Fatalf("second publish error = %v, want errFrontendEventDropped", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 1 {
			t.Fatalf("events = %d, want 1", len(batch.Events))
		}
		if batch.Dropped["traffic:entry"] != 1 {
			t.Fatalf("dropped = %#v, want one traffic:entry", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bounded event batch")
	}
}

func TestFrontendEventBatcherPublishesUnboundedEventsBeyondConfiguredLimits(t *testing.T) {
	batches := make(chan FrontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 1,
		MaxPendingBytes:  128,
	}, func(batch FrontendEventBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	if err := batcher.Publish("traffic:entry", map[string]any{"id": 1}); err != nil {
		t.Fatalf("publish bounded event: %v", err)
	}
	if err := batcher.PublishUnbounded("http-request:event", map[string]any{
		"body": strings.Repeat("h", 256),
	}); err != nil {
		t.Fatalf("publish unbounded HTTP Request event: %v", err)
	}
	if err := batcher.PublishUnbounded("websocket-session:event", map[string]any{
		"body": strings.Repeat("w", 256),
	}); err != nil {
		t.Fatalf("publish unbounded WebSocket Session event: %v", err)
	}
	batcher.Close()

	select {
	case batch := <-batches:
		if len(batch.Events) != 3 {
			t.Fatalf("events = %d, want 3", len(batch.Events))
		}
		wantNames := []string{"traffic:entry", "http-request:event", "websocket-session:event"}
		for index, wantName := range wantNames {
			if batch.Events[index].Name != wantName {
				t.Fatalf("event[%d].Name = %q, want %q", index, batch.Events[index].Name, wantName)
			}
		}
		if len(batch.Dropped) != 0 {
			t.Fatalf("unexpected dropped events: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unbounded event batch")
	}
}

func TestFrontendEventBatcherRejectsSingleOversizedPayload(t *testing.T) {
	batches := make(chan FrontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  32,
	}, func(batch FrontendEventBatch) {
		batches <- batch
	})
	t.Cleanup(batcher.Close)

	err := batcher.Publish("traffic:live-update", map[string]any{"chunkBase64": "a payload larger than the configured byte budget"})
	if !errors.Is(err, errFrontendEventDropped) {
		t.Fatalf("publish error = %v, want errFrontendEventDropped", err)
	}

	select {
	case batch := <-batches:
		if len(batch.Events) != 0 {
			t.Fatalf("events = %d, want 0", len(batch.Events))
		}
		if batch.Dropped["traffic:live-update"] != 1 {
			t.Fatalf("dropped = %#v, want one traffic:live-update", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for oversized drop notification")
	}
}

func TestFrontendEventBatcherKeepsTheByteLimitAcrossTypedPayloads(t *testing.T) {
	batches := make(chan FrontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 64,
		MaxPendingBytes:  700,
	}, func(batch FrontendEventBatch) { batches <- batch })
	t.Cleanup(batcher.Close)

	chunk := TrafficLiveUpdate{TrafficID: 1, Kind: TrafficLiveUpdateSSEChunk, Chunk: bytes.Repeat([]byte("x"), 200)}
	if err := batcher.Publish("traffic:live-update", chunk); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if err := batcher.Publish("traffic:live-update", chunk); err != nil {
		t.Fatalf("second chunk: %v", err)
	}
	if err := batcher.Publish("traffic:live-update", chunk); !errors.Is(err, errFrontendEventDropped) {
		t.Fatalf("third chunk error = %v, want errFrontendEventDropped", err)
	}
	batcher.Close()
	batch := <-batches
	if len(batch.Events) != 2 || batch.Dropped["traffic:live-update"] != 1 {
		t.Fatalf("events = %d, dropped = %#v", len(batch.Events), batch.Dropped)
	}
}

func TestFrontendEventBatcherPublishAfterCloseDoesNotBlock(t *testing.T) {
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{FlushInterval: time.Hour}, nil)
	batcher.Close()
	done := make(chan error, 1)
	go func() { done <- batcher.Publish("traffic:entry", 1) }()
	select {
	case err := <-done:
		if !errors.Is(err, errFrontendEventBatcherClosed) {
			t.Fatalf("publish error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("publish blocked after close")
	}
}

func TestEstimateFrontendEventSizeCountsPayloadText(t *testing.T) {
	small := estimateFrontendEventSize(TrafficLiveUpdate{TrafficID: 1})
	large := estimateFrontendEventSize(TrafficLiveUpdate{TrafficID: 1, Chunk: bytes.Repeat([]byte("x"), 4096)})
	if large-small != 4096 {
		t.Fatalf("large-small = %d, want 4096", large-small)
	}
	entry := &TrafficEntry{URL: strings.Repeat("u", 100), Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: strings.Repeat("n", 50)}}}}
	if got := estimateFrontendEventSize(entry); got < 150 {
		t.Fatalf("entry size = %d, want at least the text it holds", got)
	}
}

type claimingBatchRecorder struct {
	batches  chan FrontendEventBatch
	releases chan func()
	claim    bool
}

func newClaimingBatchRecorder(claim bool) *claimingBatchRecorder {
	return &claimingBatchRecorder{
		batches:  make(chan FrontendEventBatch, 16),
		releases: make(chan func(), 16),
		claim:    claim,
	}
}

func (r *claimingBatchRecorder) emit(batch FrontendEventBatch) {
	if r.claim {
		r.releases <- batch.Claim()
	}
	r.batches <- batch
}

func (r *claimingBatchRecorder) next(t *testing.T, within time.Duration) FrontendEventBatch {
	t.Helper()
	select {
	case batch := <-r.batches:
		return batch
	case <-time.After(within):
		t.Fatal("timed out waiting for event batch")
		return FrontendEventBatch{}
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

func eventIDs(batch FrontendEventBatch) []int {
	ids := make([]int, 0, len(batch.Events))
	for _, event := range batch.Events {
		ids = append(ids, event.Data.(int))
	}
	return ids
}

func TestFrontendEventBatcherDoesNotWaitForUnclaimedBatches(t *testing.T) {
	recorder := newClaimingBatchRecorder(false)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval: time.Millisecond,
	}, recorder.emit)
	t.Cleanup(batcher.Close)

	for id := range 3 {
		if err := batcher.Publish("traffic:entry", id); err != nil {
			t.Fatalf("publish %d: %v", id, err)
		}
		if got := eventIDs(recorder.next(t, time.Second)); len(got) != 1 || got[0] != id {
			t.Fatalf("batch %d = %v", id, got)
		}
	}
}

func TestFrontendEventBatcherHoldsBatchesWhileClaimed(t *testing.T) {
	recorder := newClaimingBatchRecorder(true)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 4,
		MaxPendingBytes:  1 << 20,
	}, recorder.emit)
	t.Cleanup(batcher.Close)

	if err := batcher.Publish("traffic:entry", 0); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	recorder.next(t, time.Second)
	release := <-recorder.releases

	// A burst beyond the pending limit while the first batch is unreleased.
	var dropped int
	for id := 1; id <= 10; id++ {
		if err := batcher.Publish("traffic:entry", id); errors.Is(err, errFrontendEventDropped) {
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
	if batch.Dropped["traffic:entry"] != 6 {
		t.Fatalf("dropped counts = %#v, want 6 traffic:entry", batch.Dropped)
	}
}

func TestFrontendEventBatcherKeepsClaimsBoundedDuringLongStalls(t *testing.T) {
	for _, byteBound := range []bool{false, true} {
		t.Run(fmt.Sprintf("byteBound=%t", byteBound), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := newClaimingBatchRecorder(true)
				options := frontendEventBatcherOptions{FlushInterval: time.Millisecond, MaxPendingEvents: 4, MaxPendingBytes: 1 << 20}
				wantPending := 4
				if byteBound {
					options.MaxPendingEvents = 100
					options.MaxPendingBytes = 2 * (len("traffic:entry") + frontendEventOverhead + estimateFrontendEventSize(1))
					wantPending = 2
				}
				batcher := newFrontendEventBatcher(options, recorder.emit)
				defer batcher.Close()
				if err := batcher.Publish("traffic:entry", 0); err != nil {
					t.Fatal(err)
				}
				recorder.next(t, time.Second)
				release := <-recorder.releases
				for round := range 3 {
					for i := range 10 {
						_ = batcher.Publish("traffic:entry", round*10+i+1)
					}
					// Each interval exceeds the former two-second escape hatch.
					recorder.none(t, 3*time.Second)
				}
				release()
				batch := recorder.next(t, time.Second)
				if len(batch.Events) != wantPending || batch.Dropped["traffic:entry"] != uint64(30-wantPending) {
					t.Fatalf("pending=%d dropped=%v, want %d and %d", len(batch.Events), batch.Dropped, wantPending, 30-wantPending)
				}
				for i, id := range eventIDs(batch) {
					if id != i+1 {
						t.Fatalf("event %d = %d, want publication order", i, id)
					}
				}
			})
		})
	}
}

func TestFrontendEventBatcherCloseReturnsWhileWaitingForRelease(t *testing.T) {
	recorder := newClaimingBatchRecorder(true)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval: time.Millisecond,
	}, recorder.emit)

	if err := batcher.Publish("traffic:entry", 1); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	recorder.next(t, time.Second)
	if err := batcher.Publish("traffic:entry", 2); err != nil {
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
