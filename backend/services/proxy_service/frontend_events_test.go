package proxyservice

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTrafficResetMarkerOrdersOldAndNewTrafficAcrossBatches(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan frontendEventBatch, 2)
	svc.frontendEventBatcher = newFrontendEventBatcher(
		frontendEventBatcherOptions{
			FlushInterval:    time.Hour,
			MaxPendingEvents: 16,
			MaxPendingBytes:  4096,
		},
		func(batch frontendEventBatch) { batches <- batch },
	)
	t.Cleanup(svc.frontendEventBatcher.Close)

	oldEntry := svc.newTrafficEntry(TrafficEntry{Type: "http", URL: "https://old.example/"})
	if !svc.storeTrafficEntry(oldEntry) {
		t.Fatal("store old traffic entry")
	}
	svc.emitTraffic(oldEntry)
	svc.frontendEventBatcher.emitPending()

	svc.ClearTraffic()
	freshEntry := svc.newTrafficEntry(TrafficEntry{Type: "http", URL: "https://new.example/"})
	if !svc.storeTrafficEntry(freshEntry) {
		t.Fatal("store fresh traffic entry")
	}
	svc.emitTraffic(freshEntry)
	svc.frontendEventBatcher.emitPending()

	first := <-batches
	second := <-batches
	if len(first.Events) != 1 || first.Events[0].Name != trafficEventName {
		t.Fatalf("first batch = %#v, want old traffic entry", first.Events)
	}
	if len(second.Events) != 2 || second.Events[0].Name != trafficResetEventName || second.Events[1].Name != trafficEventName {
		t.Fatalf("second batch = %#v, want reset then fresh traffic entry", second.Events)
	}
	var reset struct {
		CaptureGeneration uint64 `json:"captureGeneration"`
	}
	if err := json.Unmarshal(second.Events[0].Data, &reset); err != nil {
		t.Fatalf("decode reset marker: %v", err)
	}
	if reset.CaptureGeneration != 1 {
		t.Fatalf("capture generation = %d, want 1", reset.CaptureGeneration)
	}
}

func TestRestartAndCacheClearPublishTrafficResetMarker(t *testing.T) {
	tests := []struct {
		name  string
		reset func(*ProxyService) error
	}{
		{name: "restart capture", reset: func(service *ProxyService) error { return service.RestartCapture(false) }},
		{name: "clear cache", reset: (*ProxyService).ClearCacheFiles},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestConfigDir(t)
			svc := newTestProxyService(t, nil)
			batches := make(chan frontendEventBatch, 1)
			svc.frontendEventBatcher = newFrontendEventBatcher(
				frontendEventBatcherOptions{FlushInterval: time.Hour},
				func(batch frontendEventBatch) { batches <- batch },
			)
			t.Cleanup(svc.frontendEventBatcher.Close)

			if err := tt.reset(svc); err != nil {
				t.Fatalf("reset: %v", err)
			}
			svc.frontendEventBatcher.emitPending()
			batch := <-batches
			if len(batch.Events) != 1 || batch.Events[0].Name != trafficResetEventName {
				t.Fatalf("events = %#v, want one traffic reset", batch.Events)
			}
		})
	}
}

func TestHighFrequencyFrontendEventsShareOneOrderedBatch(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan frontendEventBatch, 1)
	svc.frontendEventBatcher = newFrontendEventBatcher(
		frontendEventBatcherOptions{
			FlushInterval:    time.Millisecond,
			MaxPendingEvents: 16,
			MaxPendingBytes:  4096,
		},
		func(batch frontendEventBatch) {
			batches <- batch
		},
	)
	t.Cleanup(svc.frontendEventBatcher.Close)

	svc.emitHTTPRequestEvent(HTTPRequestStreamEvent{SessionID: "http", EventType: "chunk"})
	svc.emitWebSocketSessionEvent(WebSocketSessionEvent{SessionID: "ws", EventType: "message"})
	svc.SetLiveTrafficDetail(7)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})

	select {
	case batch := <-batches:
		if len(batch.Events) != 3 {
			t.Fatalf("events = %d, want 3", len(batch.Events))
		}
		wantNames := []string{httpRequestEventName, webSocketSessionEventName, trafficLiveUpdateEventName}
		for index, wantName := range wantNames {
			if batch.Events[index].Name != wantName {
				t.Fatalf("event[%d].Name = %q, want %q", index, batch.Events[index].Name, wantName)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frontend event batch")
	}
}

func TestRequestEditorFrontendEventsBypassSharedBatchLimits(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan frontendEventBatch, 1)
	svc.frontendEventBatcher = newFrontendEventBatcher(
		frontendEventBatcherOptions{
			FlushInterval:    time.Hour,
			MaxPendingEvents: 1,
			MaxPendingBytes:  128,
		},
		func(batch frontendEventBatch) {
			batches <- batch
		},
	)
	t.Cleanup(svc.frontendEventBatcher.Close)

	svc.emitHTTPRequestEvent(HTTPRequestStreamEvent{
		SessionID:   "http",
		EventType:   "chunk",
		ChunkBase64: strings.Repeat("h", 256),
	})
	svc.emitWebSocketSessionEvent(WebSocketSessionEvent{
		SessionID: "ws",
		EventType: "message",
		Error:     strings.Repeat("w", 256),
	})
	svc.SetLiveTrafficDetail(7)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})
	svc.frontendEventBatcher.Close()

	select {
	case batch := <-batches:
		if len(batch.Events) != 2 {
			t.Fatalf("events = %d, want 2", len(batch.Events))
		}
		wantNames := []string{httpRequestEventName, webSocketSessionEventName}
		for index, wantName := range wantNames {
			if batch.Events[index].Name != wantName {
				t.Fatalf("event[%d].Name = %q, want %q", index, batch.Events[index].Name, wantName)
			}
		}
		if batch.Dropped[trafficLiveUpdateEventName] != 1 {
			t.Fatalf("dropped = %#v, want one bounded traffic live update", batch.Dropped)
		}
		if _, ok := batch.Dropped[httpRequestEventName]; ok {
			t.Fatalf("HTTP Request event was unexpectedly dropped: %#v", batch.Dropped)
		}
		if _, ok := batch.Dropped[webSocketSessionEventName]; ok {
			t.Fatalf("WebSocket Session event was unexpectedly dropped: %#v", batch.Dropped)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frontend event batch")
	}
}
