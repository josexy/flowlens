package proxyservice

import (
	"bytes"
	"context"

	"sync"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

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
