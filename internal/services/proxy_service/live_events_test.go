package proxyservice

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
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

// sinkRecorder keeps the events one stream's sink received.
type sinkRecorder struct {
	mu     sync.Mutex
	events []HTTPRequestStreamEvent
	done   chan struct{}
}

func newSinkRecorder() *sinkRecorder { return &sinkRecorder{done: make(chan struct{})} }

func (r *sinkRecorder) sink(event HTTPRequestStreamEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	if event.EventType != HTTPStreamChunk {
		close(r.done)
	}
}

func (r *sinkRecorder) body() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	var body []byte
	for _, event := range r.events {
		body = append(body, event.Chunk...)
	}
	return body
}

func TestHTTPStreamSinkReceivesOnlyItsSessionsEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + r.URL.Query().Get("name") + "\n\n"))
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})

	recorders := map[string]*sinkRecorder{"one": newSinkRecorder(), "two": newSinkRecorder()}
	sessions := map[string]string{}
	for name, recorder := range recorders {
		response, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
			http.MethodGet, server.URL+"/?name="+name, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone}, recorder.sink)
		if err != nil || !response.Streaming {
			t.Fatalf("send %s: streaming=%t err=%v", name, response.Streaming, err)
		}
		sessions[name] = response.StreamSessionID
	}
	for name, recorder := range recorders {
		select {
		case <-recorder.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("stream %s did not end", name)
		}
		if got, want := string(recorder.body()), "data: "+name+"\n\n"; got != want {
			t.Fatalf("stream %s body = %q, want %q", name, got, want)
		}
		recorder.mu.Lock()
		for _, event := range recorder.events {
			if event.SessionID != sessions[name] {
				t.Errorf("stream %s received an event of session %s", name, event.SessionID)
			}
		}
		if last := recorder.events[len(recorder.events)-1]; last.EventType != HTTPStreamComplete {
			t.Errorf("stream %s ended with %s", name, last.EventType)
		}
		recorder.mu.Unlock()
	}
}

func TestStreamEndFollowsItsChunksUnderAFullPendingBound(t *testing.T) {
	svc := newTestProxyService(t, nil)
	batches := make(chan LiveBatch, 1)
	svc.liveBatcher = newLiveEventBatcher(liveEventBatcherOptions{
		FlushInterval: time.Hour, MaxPendingEvents: 2, MaxPendingBytes: 1 << 20,
	}, func(batch LiveBatch) { batches <- batch })
	t.Cleanup(svc.liveBatcher.Close)

	// Capture traffic fills the pending bound.
	for id := uint64(1); id <= 4; id++ {
		svc.publishLive(TrafficEntryEvent{Entry: &TrafficEntry{ID: id}})
	}
	var received []HTTPRequestStreamEvent
	sink := func(event HTTPRequestStreamEvent) { received = append(received, event) }
	session := &httpRequestStreamSession{id: "s", sink: sink, cancel: func() {}}
	svc.httpRequestStreams.Store(session.id, session)
	for offset := int64(0); offset < 3; offset++ {
		chunkOffset := offset
		svc.emitHTTPRequestEvent(session.sink, HTTPRequestStreamEvent{SessionID: "s", EventType: HTTPStreamChunk, Offset: &chunkOffset, Chunk: bytes.Repeat([]byte("x"), 1)})
		session.offset.Store(offset + 1)
	}
	svc.finalizeHTTPRequestStream("s", HTTPStreamComplete, nil)
	svc.liveBatcher.Close()

	batch := <-batches
	if batch.Dropped[LiveTrafficEntry] != 2 || batch.Dropped[LiveHTTPStream] != 0 {
		t.Fatalf("dropped = %v, want only the traffic beyond the bound", batch.Dropped)
	}
	for _, event := range batch.Events {
		if stream, ok := event.(HTTPStreamEvent); ok {
			stream.Deliver()
		}
	}
	if len(received) != 4 || received[3].EventType != HTTPStreamComplete || *received[3].Offset != 3 {
		t.Fatalf("received = %+v, want three chunks and then the end", received)
	}
	for index, event := range received[:3] {
		if event.EventType != HTTPStreamChunk || *event.Offset != int64(index) {
			t.Fatalf("event %d = %+v", index, event)
		}
	}
}

func TestWebSocketEventsWithoutABatcherReachTheSinkAtOnce(t *testing.T) {
	svc := newTestProxyService(t, nil)
	var received []WebSocketSessionEvent
	svc.emitWebSocketSessionEvent(func(event WebSocketSessionEvent) { received = append(received, event) },
		WebSocketSessionEvent{SessionID: "w", EventType: WebSocketEventConnected, Status: "connected"})
	svc.emitWebSocketSessionEvent(nil, WebSocketSessionEvent{SessionID: "w", EventType: WebSocketEventMessage})
	if len(received) != 1 || received[0].EventType != WebSocketEventConnected {
		t.Fatalf("received = %+v", received)
	}
}
