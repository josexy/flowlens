package proxyservice

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

func TestIsServerSentEventsContentType(t *testing.T) {
	t.Parallel()

	for _, contentType := range []string{
		"text/event-stream",
		"text/event-stream; charset=utf-8",
		"TEXT/EVENT-STREAM; CHARSET=UTF-8",
	} {
		if !isServerSentEventsContentType(contentType) {
			t.Fatalf("expected %q to be recognized as SSE", contentType)
		}
	}
	for _, contentType := range []string{"", "text/plain", "application/json", "invalid;="} {
		if isServerSentEventsContentType(contentType) {
			t.Fatalf("did not expect %q to be recognized as SSE", contentType)
		}
	}
}

func TestShouldStreamSSEUpdatesRequiresSupportedEncoding(t *testing.T) {
	t.Parallel()

	if !shouldStreamSSEUpdates("text/event-stream", "gzip") {
		t.Fatal("gzip SSE should stream live updates")
	}
	if shouldStreamSSEUpdates("text/event-stream", "compress") {
		t.Fatal("unsupported content encoding should retain existing binary behavior")
	}
	if shouldStreamSSEUpdates("text/plain", "") {
		t.Fatal("non-SSE response should not stream live updates")
	}
}

func TestTrafficLiveUpdateRequiresMatchingSubscription(t *testing.T) {
	svc := newTestProxyService(t, nil)
	var updates []TrafficLiveUpdate
	svc.emitTrafficLiveUpdateHook = func(update TrafficLiveUpdate) {
		updates = append(updates, update)
	}

	svc.SetLiveTrafficDetail(7)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 8, Kind: TrafficLiveUpdateSSEChunk})
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})
	svc.SetLiveTrafficDetail(0)
	svc.emitTrafficLiveUpdate(TrafficLiveUpdate{TrafficID: 7, Kind: TrafficLiveUpdateSSEChunk})

	if len(updates) != 1 || updates[0].TrafficID != 7 {
		t.Fatalf("updates = %#v, want one update for traffic 7", updates)
	}
}

func TestLiveTrafficSubscriptionClearsWithTrafficRemoval(t *testing.T) {
	svc := newTestProxyService(t, nil)
	svc.storeTrafficEntry(&TrafficEntry{ID: 30, Type: "http"})
	svc.SetLiveTrafficDetail(30)
	svc.deleteTrafficEntry(30)
	if activeID := svc.liveTrafficDetailID.Load(); activeID != 0 {
		t.Fatalf("active detail ID after delete = %d, want 0", activeID)
	}

	svc.SetLiveTrafficDetail(31)
	svc.ClearTraffic()
	if activeID := svc.liveTrafficDetailID.Load(); activeID != 0 {
		t.Fatalf("active detail ID after clear = %d, want 0", activeID)
	}
}

func TestObservedResponseStreamEmitsIdentityChunksWithContinuousOffsets(t *testing.T) {
	payload := bytes.Repeat([]byte("data: identity\n\n"), 4096)
	svc := newTestProxyService(t, nil)

	var decoded []byte
	var nextOffset int64
	reader := svc.newObservedStreamBodyReader(
		io.NopCloser(bytes.NewReader(payload)),
		11,
		"",
		false,
		func(offset int64, data []byte) {
			if offset != nextOffset {
				t.Fatalf("offset = %d, want %d", offset, nextOffset)
			}
			decoded = append(decoded, data...)
			nextOffset += int64(len(data))
		},
	)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("drain identity reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close identity reader: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded identity updates length = %d, want %d", len(decoded), len(payload))
	}
}

func TestObservedResponseStreamEmitsDecodedChunksWithContinuousOffsets(t *testing.T) {
	payload := []byte("data: first\n\ndata: 中文\n\n")
	compressed, err := compressWithGzip(payload)
	if err != nil {
		t.Fatalf("compressWithGzip: %v", err)
	}

	svc := newTestProxyService(t, nil)
	svc.SetLiveTrafficDetail(12)
	var mu sync.Mutex
	var updates []TrafficLiveUpdate
	svc.emitTrafficLiveUpdateHook = func(update TrafficLiveUpdate) {
		mu.Lock()
		updates = append(updates, update)
		mu.Unlock()
	}
	done := make(chan struct{})
	reader := svc.newObservedStreamBodyReader(
		io.NopCloser(bytes.NewReader(compressed)),
		12,
		"gzip",
		false,
		svc.capturedSSEChunkPublisher(&TrafficEntry{ID: 12, captureGeneration: svc.captureGeneration}),
		func() { close(done) },
	)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("drain encoded reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close encoded reader: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decoded stream capture")
	}

	mu.Lock()
	defer mu.Unlock()
	var decoded []byte
	var nextOffset int64
	for _, update := range updates {
		if update.Kind != TrafficLiveUpdateSSEChunk {
			t.Fatalf("unexpected update kind %q", update.Kind)
		}
		if update.Offset == nil || *update.Offset != nextOffset {
			t.Fatalf("offset = %v, want %d", update.Offset, nextOffset)
		}
		decoded = append(decoded, update.Chunk...)
		nextOffset += int64(len(update.Chunk))
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded updates = %q, want %q", decoded, payload)
	}
}

func TestCapturedSSEChunkPublisherOwnsChunk(t *testing.T) {
	svc := newTestProxyService(t, nil)
	svc.SetLiveTrafficDetail(14)
	var updates []TrafficLiveUpdate
	svc.emitTrafficLiveUpdateHook = func(update TrafficLiveUpdate) {
		updates = append(updates, update)
	}
	publish := svc.capturedSSEChunkPublisher(&TrafficEntry{ID: 14, captureGeneration: svc.captureGeneration})

	buffer := []byte("data: one\n\n")
	publish(0, buffer)
	copy(buffer, "XXXXXXXXXXX")

	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}
	if string(updates[0].Chunk) != "data: one\n\n" {
		t.Fatalf("delivered chunk = %q after the source buffer changed", updates[0].Chunk)
	}
}

// reusedBufferReader returns each chunk through the caller's buffer, so a
// publisher that keeps p[:n] would see the next chunk overwrite it.
type reusedBufferReader struct {
	chunks []string
}

func (r *reusedBufferReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

func TestHTTPRequestStreamReadLoopOwnsChunks(t *testing.T) {
	svc := newTestProxyService(t, nil)
	var events []HTTPRequestStreamEvent
	svc.emitHTTPRequestEventHook = func(event HTTPRequestStreamEvent) {
		events = append(events, event)
	}
	session := &httpRequestStreamSession{
		id:     "owned-chunks",
		cancel: func() {},
		body:   io.NopCloser(&reusedBufferReader{chunks: []string{"AAAA", "BBBB", "CCCC"}}),
	}

	svc.runHTTPRequestStreamReadLoop(session)

	var chunks []string
	for _, event := range events {
		if event.EventType == "chunk" {
			chunks = append(chunks, string(event.Chunk))
		}
	}
	if len(chunks) != 3 || chunks[0] != "AAAA" || chunks[1] != "BBBB" || chunks[2] != "CCCC" {
		t.Fatalf("delivered chunks = %q, want [AAAA BBBB CCCC]", chunks)
	}
}

func TestCapturedWebSocketUpdatesAreIndexedAndTruncatedOnce(t *testing.T) {
	svc := newTestProxyService(t, nil)
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{MaxWsMessages: 2},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	svc.SetLiveTrafficDetail(20)
	var updates []TrafficLiveUpdate
	svc.emitTrafficLiveUpdateHook = func(update TrafficLiveUpdate) {
		updates = append(updates, update)
	}
	wsMsgs := &TrafficWsMsgs{liveState: 1}

	svc.appendCapturedWebSocketMessage(20, wsMsgs, &WebSocketMessage{Direction: "send", MsgType: "text", Data: "one"})
	svc.appendCapturedWebSocketMessage(20, wsMsgs, &WebSocketMessage{Direction: "receive", MsgType: "text", Data: "two"})
	if svc.shouldStoreWebSocketMessage(20, wsMsgs) {
		t.Fatal("message beyond limit should not be stored")
	}
	if svc.shouldStoreWebSocketMessage(20, wsMsgs) {
		t.Fatal("later message beyond limit should not be stored")
	}

	if len(updates) != 3 {
		t.Fatalf("updates length = %d, want 3", len(updates))
	}
	if updates[0].Kind != TrafficLiveUpdateWebSocketMessage || updates[0].MessageIndex == nil || *updates[0].MessageIndex != 0 {
		t.Fatalf("first update = %#v", updates[0])
	}
	if updates[1].Kind != TrafficLiveUpdateWebSocketMessage || updates[1].MessageIndex == nil || *updates[1].MessageIndex != 1 {
		t.Fatalf("second update = %#v", updates[1])
	}
	if updates[2].Kind != TrafficLiveUpdateWebSocketTruncated {
		t.Fatalf("third update = %#v", updates[2])
	}
}
