package proxyservice

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingJSONMarshaler struct {
	calls *atomic.Int32
}

func (m countingJSONMarshaler) MarshalJSON() ([]byte, error) {
	m.calls.Add(1)
	return []byte(`{"value":true}`), nil
}

func TestFrontendEventBatcherSerializesAndBatchesInOrder(t *testing.T) {
	batches := make(chan frontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  1024,
	}, func(batch frontendEventBatch) {
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
		var entry struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(batch.Events[0].Data, &entry); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		if entry.ID != 1 {
			t.Fatalf("entry ID = %d, want 1", entry.ID)
		}
		if len(batch.Dropped) != 0 {
			t.Fatalf("unexpected dropped events: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event batch")
	}
}

func TestFrontendEventBatcherBoundsPendingEventsAndReportsDrops(t *testing.T) {
	batches := make(chan frontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 1,
		MaxPendingBytes:  1024,
	}, func(batch frontendEventBatch) {
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
	batches := make(chan frontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 1,
		MaxPendingBytes:  128,
	}, func(batch frontendEventBatch) {
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
	batches := make(chan frontendEventBatch, 1)
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Millisecond,
		MaxPendingEvents: 8,
		MaxPendingBytes:  32,
	}, func(batch frontendEventBatch) {
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

func TestFrontendEventBatcherDoesNotMarshalWhenPendingEventLimitIsAlreadyFull(t *testing.T) {
	batcher := newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval:    time.Hour,
		MaxPendingEvents: 1,
		MaxPendingBytes:  1024,
	}, nil)
	t.Cleanup(batcher.Close)

	if err := batcher.Publish("traffic:entry", map[string]any{"id": 1}); err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	var calls atomic.Int32
	err := batcher.Publish("traffic:entry", countingJSONMarshaler{calls: &calls})
	if !errors.Is(err, errFrontendEventDropped) {
		t.Fatalf("second publish error = %v, want errFrontendEventDropped", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("MarshalJSON calls = %d, want 0 for an already-full queue", got)
	}
}
