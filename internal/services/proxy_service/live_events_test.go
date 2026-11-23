package proxyservice

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTrafficResetMarkerOrdersOldAndNewTrafficAcrossBatches(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan LiveBatch, 2)
	svc.liveBatcher = newLiveEventBatcher(
		liveEventBatcherOptions{
			FlushInterval:    time.Hour,
			MaxPendingEvents: 16,
			MaxPendingBytes:  4096,
		},
		func(batch LiveBatch) { batches <- batch },
	)
	t.Cleanup(svc.liveBatcher.Close)

	oldEntry := svc.newTrafficEntry(TrafficEntry{Type: "http", URL: "https://old.example/"})
	if !svc.storeTrafficEntry(oldEntry) {
		t.Fatal("store old traffic entry")
	}
	svc.emitTraffic(oldEntry)
	svc.liveBatcher.emitPending()

	svc.ClearTraffic()
	freshEntry := svc.newTrafficEntry(TrafficEntry{Type: "http", URL: "https://new.example/"})
	if !svc.storeTrafficEntry(freshEntry) {
		t.Fatal("store fresh traffic entry")
	}
	svc.emitTraffic(freshEntry)
	svc.liveBatcher.emitPending()

	first := <-batches
	second := <-batches
	if len(first.Events) != 1 || first.Events[0].(TrafficEntryEvent).Entry.ID != oldEntry.ID {
		t.Fatalf("first batch = %#v, want old traffic entry", first.Events)
	}
	if len(second.Events) != 2 || second.Events[1].(TrafficEntryEvent).Entry.ID != freshEntry.ID {
		t.Fatalf("second batch = %#v, want reset then fresh traffic entry", second.Events)
	}
	if reset, ok := second.Events[0].(TrafficResetEvent); !ok || reset.CaptureGeneration != 1 {
		t.Fatalf("reset marker = %#v, want capture generation 1", second.Events[0])
	}
}

func TestRestartAndCacheClearPublishTrafficResetMarker(t *testing.T) {
	tests := []struct {
		name  string
		reset func(*ProxyService) error
	}{
		{name: "restart capture", reset: func(service *ProxyService) error { return service.RestartCapture(false) }},
		{name: "clear cache", reset: func(service *ProxyService) error { _, err := service.ClearCacheFiles(); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestConfigDir(t)
			svc := newTestProxyService(t, nil)
			batches := make(chan LiveBatch, 1)
			svc.liveBatcher = newLiveEventBatcher(
				liveEventBatcherOptions{FlushInterval: time.Hour},
				func(batch LiveBatch) { batches <- batch },
			)
			t.Cleanup(svc.liveBatcher.Close)

			if err := tt.reset(svc); err != nil {
				t.Fatalf("reset: %v", err)
			}
			svc.liveBatcher.emitPending()
			batch := <-batches
			if len(batch.Events) != 1 || batch.Events[0].liveKind() != LiveTrafficReset {
				t.Fatalf("events = %#v, want one traffic reset", batch.Events)
			}
		})
	}
}

func TestHighFrequencyLiveEventsShareOneOrderedBatch(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan LiveBatch, 1)
	svc.liveBatcher = newLiveEventBatcher(
		liveEventBatcherOptions{
			FlushInterval:    time.Millisecond,
			MaxPendingEvents: 16,
			MaxPendingBytes:  4096,
		},
		func(batch LiveBatch) {
			batches <- batch
		},
	)
	t.Cleanup(svc.liveBatcher.Close)

	svc.emitHTTPRequestEvent(nil, HTTPRequestStreamEvent{SessionID: "http", EventType: "chunk"})
	svc.emitWebSocketSessionEvent(nil, WebSocketSessionEvent{SessionID: "ws", EventType: "message"})
	svc.SetLiveTrafficDetail(7)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})

	select {
	case batch := <-batches:
		if len(batch.Events) != 3 {
			t.Fatalf("events = %d, want 3", len(batch.Events))
		}
		wantKinds := []LiveEventKind{LiveHTTPStream, LiveWebSocket, LiveTrafficLiveUpdate}
		for index, want := range wantKinds {
			if got := batch.Events[index].liveKind(); got != want {
				t.Fatalf("event[%d] = %s, want %s", index, got, want)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event batch")
	}
}

func TestRequestEditorLiveEventsBypassSharedBatchLimits(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan LiveBatch, 1)
	svc.liveBatcher = newLiveEventBatcher(
		liveEventBatcherOptions{
			FlushInterval:    time.Hour,
			MaxPendingEvents: 1,
			MaxPendingBytes:  128,
		},
		func(batch LiveBatch) {
			batches <- batch
		},
	)
	t.Cleanup(svc.liveBatcher.Close)

	svc.emitHTTPRequestEvent(nil, HTTPRequestStreamEvent{
		SessionID: "http",
		EventType: "chunk",
		Chunk:     bytes.Repeat([]byte("h"), 256),
	})
	svc.emitWebSocketSessionEvent(nil, WebSocketSessionEvent{
		SessionID: "ws",
		EventType: "message",
		Error:     strings.Repeat("w", 256),
	})
	svc.SetLiveTrafficDetail(7)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})
	svc.liveBatcher.Close()

	select {
	case batch := <-batches:
		if len(batch.Events) != 2 {
			t.Fatalf("events = %d, want 2", len(batch.Events))
		}
		wantKinds := []LiveEventKind{LiveHTTPStream, LiveWebSocket}
		for index, want := range wantKinds {
			if got := batch.Events[index].liveKind(); got != want {
				t.Fatalf("event[%d] = %s, want %s", index, got, want)
			}
		}
		if batch.Dropped[LiveTrafficLiveUpdate] != 1 {
			t.Fatalf("dropped = %#v, want one bounded traffic live update", batch.Dropped)
		}
		if _, ok := batch.Dropped[LiveHTTPStream]; ok {
			t.Fatalf("HTTP Request event was unexpectedly dropped: %#v", batch.Dropped)
		}
		if _, ok := batch.Dropped[LiveWebSocket]; ok {
			t.Fatalf("WebSocket Session event was unexpectedly dropped: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event batch")
	}
}

func TestEmitTrafficSnapshotsAnUnstoredWorkingEntry(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan LiveBatch, 1)
	svc.liveBatcher = newLiveEventBatcher(
		liveEventBatcherOptions{FlushInterval: time.Hour},
		func(batch LiveBatch) { batches <- batch },
	)
	t.Cleanup(svc.liveBatcher.Close)

	entry := svc.newTrafficEntry(TrafficEntry{
		Type:    "http",
		URL:     "https://example.test/",
		Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: "A", Value: "1"}}},
	})
	svc.emitTraffic(entry)
	entry.URL = "https://changed.test/"
	entry.Request.HeaderFields[0].Value = "2"
	svc.liveBatcher.emitPending()

	batch := <-batches
	if len(batch.Events) != 1 {
		t.Fatalf("events = %#v", batch.Events)
	}
	published := batch.Events[0].(TrafficEntryEvent).Entry
	if published.URL != "https://example.test/" || published.Request.HeaderFields[0].Value != "1" {
		t.Fatalf("published entry changed after publishing: %#v", published)
	}
}
