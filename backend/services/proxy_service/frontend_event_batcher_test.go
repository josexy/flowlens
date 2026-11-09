package proxyservice

import (
	"errors"
	"strings"
	"testing"
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

	chunk := TrafficLiveUpdate{TrafficID: 1, Kind: TrafficLiveUpdateSSEChunk, ChunkBase64: strings.Repeat("x", 200)}
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
	large := estimateFrontendEventSize(TrafficLiveUpdate{TrafficID: 1, ChunkBase64: strings.Repeat("x", 4096)})
	if large-small != 4096 {
		t.Fatalf("large-small = %d, want 4096", large-small)
	}
	entry := &TrafficEntry{URL: strings.Repeat("u", 100), Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: strings.Repeat("n", 50)}}}}
	if got := estimateFrontendEventSize(entry); got < 150 {
		t.Fatalf("entry size = %d, want at least the text it holds", got)
	}
}
