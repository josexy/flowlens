package proxyservice

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
	"github.com/josexy/flowlens/internal/pkg/fs"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func testCapturedBodyFromBytes(t *testing.T, flowID uint64, kind string, data []byte) *capturedBody {
	t.Helper()

	body := newCapturedBody(flowID, kind, bodycache.MaxBodyCacheThresholdBytes, nil)
	body.Write(data)
	body.Close()
	return body
}

func TestGetTrafficBodyViewFallsBackToDiskCache(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   1,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
		Response: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"application/octet-stream"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})

	if err := cache.Write(entry.ID, bodycache.KindRequest, []byte("request from disk")); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindResponse, []byte{0x00, 0x01, 0x02}); err != nil {
		t.Fatalf("cache.Write response: %v", err)
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if view.RequestBody != "request from disk" {
		t.Fatalf("RequestBody = %q, want request from disk", view.RequestBody)
	}
	if !view.ResponseBodyBinary {
		t.Fatal("ResponseBodyBinary = false, want true")
	}
	if view.ResponseBody != "\x00\x01\x02" {
		t.Fatalf("ResponseBody = %q, want raw bytes 00 01 02", view.ResponseBody)
	}
}

func TestGetTrafficBodyViewReadsActiveCacheBackedResponse(t *testing.T) {
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   2,
		Type: "http",
		Response: &HTTPMessage{HeaderFields: testHeaderFields(map[string][]string{
			"Content-Type": {"text/event-stream"},
		})},
	}
	responseBody := newCapturedBody(entry.ID, bodycache.KindResponse, 4, cache)
	t.Cleanup(responseBody.Close)
	responseBody.Write([]byte("data: first\n\n"))
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{
		liveState:    1,
		responseBody: responseBody,
	})

	firstView, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView first: %v", err)
	}
	if firstView.ResponseBody != "data: first\n\n" {
		t.Fatalf("first response body = %q", firstView.ResponseBody)
	}

	responseBody.Write([]byte("data: second\n\n"))
	secondView, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView second: %v", err)
	}
	if secondView.ResponseBody != "data: first\n\ndata: second\n\n" {
		t.Fatalf("second response body = %q", secondView.ResponseBody)
	}
}

func TestGetTrafficBodyViewOfLargeInProgressBodyCopiesItOnce(t *testing.T) {
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   3,
		Type: "http",
		Response: &HTTPMessage{HeaderFields: testHeaderFields(map[string][]string{
			"Content-Type": {"application/octet-stream"},
		})},
	}
	responseBody := newCapturedBody(entry.ID, bodycache.KindResponse, bodycache.MaxBodyCacheThresholdBytes, cache)
	t.Cleanup(responseBody.Close)
	svc.storeTrafficEntry(entry)
	bodies := &TrafficBodies{liveState: 1, responseBody: responseBody}
	svc.trafficBodies.Store(entry.ID, bodies)

	const bodySize = 64 << 20
	chunk := make([]byte, 1<<20)
	received := make([]byte, 0, bodySize)
	for i := 0; len(received) < bodySize; i++ {
		for j := range chunk {
			chunk[j] = byte(i + j)
		}
		bodies.lockRespBody.Lock()
		responseBody.Write(chunk)
		bodies.lockRespBody.Unlock()
		received = append(received, chunk...)
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	view, err := svc.GetTrafficBodyView(entry.ID)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 2*bodySize {
		t.Fatalf("body view of a %d-byte in-progress body allocated %d bytes, want < %d", bodySize, allocated, 2*bodySize)
	}
	if view.ResponseBody != string(received) {
		t.Fatalf("response body has %d bytes, want the %d bytes received so far", len(view.ResponseBody), len(received))
	}

	// The stream keeps receiving after the view was opened.
	bodies.lockRespBody.Lock()
	responseBody.Write([]byte("tail"))
	bodies.lockRespBody.Unlock()
	view, err = svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView after more data: %v", err)
	}
	if len(view.ResponseBody) != bodySize+len("tail") || !strings.HasSuffix(view.ResponseBody, "tail") {
		t.Fatalf("response body after more data has %d bytes", len(view.ResponseBody))
	}
}

func TestStreamBodyReaderSpoolsLargeRequestBodyToCache(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 8,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	payload := bytes.Repeat([]byte("x"), 32)
	entry := &TrafficEntry{
		ID:     101,
		Type:   "http",
		Method: http.MethodPost,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)

	reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), entry.ID, "", true)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("stream reader returned different payload")
	}
	if !cache.Has(entry.ID, bodycache.KindRequest) {
		t.Fatal("large request body should be cached on disk")
	}

	value, ok := svc.trafficBodies.Load(entry.ID)
	if !ok {
		t.Fatal("traffic bodies missing")
	}
	bodies := value.(*TrafficBodies)
	bodies.lockReqBody.RLock()
	if bodies.requestBody == nil {
		t.Fatal("request body capture should remain available after spooling")
	}
	if bodies.requestBody.Memory() != nil {
		t.Fatalf("request memory buffer should be released after spooling, len=%d", bodies.requestBody.Memory().Len())
	}
	if bodies.requestBody.writer != nil {
		t.Fatal("request body cache writer should be closed after EOF")
	}
	bodies.lockReqBody.RUnlock()

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if view.RequestBody != string(payload) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, string(payload))
	}
}

func TestStreamBodyReaderKeepsSmallRequestBodyInMemory(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 1024,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	payload := []byte("small request")
	entry := &TrafficEntry{
		ID:     102,
		Type:   "http",
		Method: http.MethodPost,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)

	reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), entry.ID, "", true)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("stream reader returned different payload")
	}
	if cache.Has(entry.ID, bodycache.KindRequest) {
		t.Fatal("small request body should not be cached on disk")
	}

	value, ok := svc.trafficBodies.Load(entry.ID)
	if !ok {
		t.Fatal("traffic bodies missing")
	}
	bodies := value.(*TrafficBodies)
	bodies.lockReqBody.RLock()
	if bodies.requestBody == nil || bodies.requestBody.Memory() == nil {
		t.Fatal("small request body should remain in memory")
	}
	if got := bodies.requestBody.Memory().String(); got != string(payload) {
		t.Fatalf("in-memory request body = %q, want %q", got, string(payload))
	}
	bodies.lockReqBody.RUnlock()

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if view.RequestBody != string(payload) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, string(payload))
	}
}

func TestStreamBodyReaderFinalizesSpoolOnEarlyClose(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 8,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	payload := bytes.Repeat([]byte("a"), 32)
	entry := &TrafficEntry{
		ID:     103,
		Type:   "http",
		Method: http.MethodPost,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)

	reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), entry.ID, "", true)
	buf := make([]byte, 16)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(buf) {
		t.Fatalf("Read n = %d, want %d", n, len(buf))
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !cache.Has(entry.ID, bodycache.KindRequest) {
		t.Fatal("early-closed spooled request body should be finalized in cache")
	}
	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if view.RequestBody != string(payload[:n]) {
		t.Fatalf("RequestBody = %q, want partial %q", view.RequestBody, string(payload[:n]))
	}
	if _, err := os.Stat(filepath.Join(cache.SessionDir(), "103_req.body.tmp")); !os.IsNotExist(err) {
		t.Fatalf("tmp cache file should be removed after early close, got %v", err)
	}
}

type closeTrackingReadCloser struct {
	*bytes.Reader
	closed atomic.Bool
}

func (r *closeTrackingReadCloser) Close() error {
	r.closed.Store(true)
	return nil
}

func TestStreamBodyReaderClosesEncodedSourceBody(t *testing.T) {
	svc := newTestProxyService(t, nil)
	payload := []byte(`{"decoded":true}`)
	compressed, err := compressWithGzip(payload)
	if err != nil {
		t.Fatalf("compressWithGzip: %v", err)
	}

	source := &closeTrackingReadCloser{Reader: bytes.NewReader(compressed)}
	entry := &TrafficEntry{
		ID:     104,
		Type:   "http",
		Method: http.MethodPost,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type":     {"application/json"},
				"Content-Encoding": {"gzip"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)

	reader := svc.newStreamBodyReader(source, entry.ID, "gzip", true)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, compressed) {
		t.Fatal("encoded stream reader should forward the original compressed payload")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !source.closed.Load() {
		t.Fatal("encoded stream reader should close the original source body")
	}
}

func TestStreamBodyReaderDamagedCompressedPayloadDoesNotDeadlockForwarding(t *testing.T) {
	svc := newTestProxyService(t, nil)
	// This is a valid gzip header followed by an invalid DEFLATE block type.
	// Keep a large tail so the capture decoder fails while the forwarding
	// TeeReader still has several writes left to make through the pipe.
	compressed := append(
		[]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x06},
		bytes.Repeat([]byte("unconsumed-encoded-payload"), 16*1024)...,
	)
	entry := &TrafficEntry{ID: 10_401, Type: "http", Method: http.MethodGet}
	svc.storeTrafficEntry(entry)
	reader := svc.newStreamBodyReader(
		io.NopCloser(bytes.NewReader(compressed)),
		entry.ID,
		"gzip",
		false,
	)

	type readResult struct {
		data []byte
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(reader)
		result <- readResult{data: data, err: err}
	}()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("ReadAll: %v", got.err)
		}
		if !bytes.Equal(got.data, compressed) {
			t.Fatal("forwarded bytes changed after compressed capture decode failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("damaged compressed stream forwarding deadlocked")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStreamBodyReaderPreservesZstdPayloadAfterChecksumError(t *testing.T) {
	for _, size := range []int{1408, 320 << 10} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			svc := newTestProxyService(t, nil)
			const pattern = "decoded-zstd-body-"
			payload := bytes.Repeat([]byte(pattern), size/len(pattern)+1)[:size]
			encoded, err := compressWithZstd(payload)
			if err != nil {
				t.Fatalf("compressWithZstd: %v", err)
			}
			encoded[len(encoded)-1] ^= 0xff // content checksum
			// Forwarding must also drain bytes after the damaged frame, while
			// capture retains only the payload decoded before the error.
			encoded = append(encoded, bytes.Repeat([]byte{0xa5}, 64<<10)...)
			entry := &TrafficEntry{ID: 10_403, Type: "http", Method: http.MethodGet}
			svc.storeTrafficEntry(entry)
			reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(encoded)), entry.ID, "zstd", false)
			defer reader.Close()
			type readResult struct {
				data []byte
				err  error
			}
			result := make(chan readResult, 1)
			go func() {
				data, readErr := io.ReadAll(reader)
				result <- readResult{data: data, err: readErr}
			}()
			select {
			case got := <-result:
				if got.err != nil {
					t.Fatalf("ReadAll: %v", got.err)
				}
				if !bytes.Equal(got.data, encoded) {
					t.Fatal("forwarded bytes changed after zstd checksum error")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("forwarding stalled after zstd checksum error")
			}
			if err := reader.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			view, err := svc.GetTrafficBodyView(entry.ID)
			if err != nil {
				t.Fatalf("GetTrafficBodyView: %v", err)
			}
			if view.ResponseBody != string(payload) {
				t.Fatalf("captured %d bytes before checksum error, want %d", len(view.ResponseBody), len(payload))
			}
		})
	}
}

func TestStreamBodyReaderDrainsBytesAfterDeflateStream(t *testing.T) {
	svc := newTestProxyService(t, nil)
	payload := []byte(`{"decoded":"deflate"}`)
	compressed, err := compressWithFlate(payload)
	if err != nil {
		t.Fatalf("compressWithFlate: %v", err)
	}
	// The decoder stops at the final DEFLATE block. Everything after it must
	// still be forwarded without the TeeReader blocking on the pipe.
	encoded := append(append([]byte{}, compressed...), bytes.Repeat([]byte{0xa5}, 64*1024)...)
	entry := &TrafficEntry{ID: 10_402, Type: "http", Method: http.MethodGet}
	svc.storeTrafficEntry(entry)
	captured := make(chan struct{})
	reader := svc.newStreamBodyReader(
		io.NopCloser(bytes.NewReader(encoded)),
		entry.ID,
		"deflate",
		false,
		func() { close(captured) },
	)

	result := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		result <- data
	}()
	select {
	case got := <-result:
		if !bytes.Equal(got, encoded) {
			t.Fatalf("forwarded %d bytes, want %d unchanged bytes", len(got), len(encoded))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("forwarding stalled on bytes after the deflate stream")
	}
	select {
	case <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("captured body did not complete")
	}

	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	stream := reader.(*StreamBodyReader)
	select {
	case <-stream.decodedDone:
	default:
		t.Fatal("decoding goroutine still running after Close")
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	if view.ResponseBody != string(payload) {
		t.Fatalf("captured body = %q, want %q", view.ResponseBody, payload)
	}
}

func TestStreamBodyReaderUsesLargeReadBuffer(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	payload := bytes.Repeat([]byte("r"), 32*1024)
	entry := &TrafficEntry{ID: 105, Type: "http", Method: http.MethodGet}
	svc.storeTrafficEntry(entry)

	reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), entry.ID, "", true)
	buf := make([]byte, len(payload))
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Read n = %d, want %d", n, len(payload))
	}
	if !bytes.Equal(buf[:n], payload) {
		t.Fatal("stream reader returned different payload")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestGetTrafficBodyViewBase64EncodesBinaryRequestBody(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   2,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"application/octet-stream"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})

	requestBody := []byte{0x00, 0x01, 0x02}
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestBody); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}

	if !view.RequestBodyBinary {
		t.Fatal("RequestBodyBinary = false, want true")
	}
	if view.RequestBody != string(requestBody) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, requestBody)
	}
}

func TestResendRequestReadsCachedRequestBody(t *testing.T) {
	t.Parallel()

	payload := []byte("cached request body")
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		received <- bodyBytes
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:     201,
		Type:   "http",
		Method: http.MethodPost,
		URL:    server.URL,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})
	if err := cache.Write(entry.ID, bodycache.KindRequest, payload); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}

	result, err := svc.ResendRequest(context.Background(), entry.ID, ResendConfig{Count: 1})
	if err != nil {
		t.Fatalf("ResendRequest: %v", err)
	}
	if result.Success != 1 || result.Failed != 0 {
		t.Fatalf("ResendRequest result = %+v, want 1 success", result)
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, payload) {
			t.Fatalf("resent request body = %q, want %q", string(got), string(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resent request body")
	}
}

func TestResendRequestReencodesDecodedGzipRequestBody(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"cached":true}`)
	type receivedRequest struct {
		body            []byte
		contentEncoding string
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		received <- receivedRequest{
			body:            bodyBytes,
			contentEncoding: r.Header.Get("Content-Encoding"),
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:     202,
		Type:   "http",
		Method: http.MethodPost,
		URL:    server.URL,
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type":     {"application/json"},
				"Content-Encoding": {"gzip"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})
	if err := cache.Write(entry.ID, bodycache.KindRequest, payload); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}

	result, err := svc.ResendRequest(context.Background(), entry.ID, ResendConfig{Count: 1})
	if err != nil {
		t.Fatalf("ResendRequest: %v", err)
	}
	if result.Success != 1 || result.Failed != 0 {
		t.Fatalf("ResendRequest result = %+v, want 1 success", result)
	}
	select {
	case got := <-received:
		if got.contentEncoding != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", got.contentEncoding)
		}
		if bytes.Equal(got.body, payload) {
			t.Fatal("resent request body was not gzip encoded")
		}
		reader, err := getDecodedReader(bytes.NewReader(got.body), got.contentEncoding)
		if err != nil {
			t.Fatalf("getDecodedReader: %v", err)
		}
		decoded, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read decoded body: %v", err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close decoded reader: %v", err)
		}
		if !bytes.Equal(decoded, payload) {
			t.Fatalf("decoded resent request body = %q, want %q", string(decoded), string(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resent request body")
	}
}

func TestBodyViewCacheReadersShareClearCacheLease(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	cache, err := bodycache.New(filepath.Join(baseDir, "cache", "session-body-view"))
	if err != nil {
		t.Fatalf("bodycache.New: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   202,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
		Response: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})
	if err := cache.Write(entry.ID, bodycache.KindRequest, []byte("cached request")); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindResponse, []byte("cached response")); err != nil {
		t.Fatalf("cache.Write response: %v", err)
	}

	bodyView, err := svc.getTrafficBodyViewInner(entry.ID)
	if err != nil {
		t.Fatalf("getTrafficBodyViewInner: %v", err)
	}
	if bodyView.RequestBodyReader == nil || bodyView.ResponseBodyReader == nil {
		t.Fatal("expected request and response cache readers")
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.ClearCacheFiles()
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("ClearCacheFiles completed before body readers closed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	bodyView.closeReqBodyReaderSafely()
	select {
	case err := <-done:
		t.Fatalf("ClearCacheFiles completed before response reader closed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	bodyView.closeRspBodyReaderSafely()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ClearCacheFiles: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ClearCacheFiles after closing body readers")
	}
}

func TestGetTrafficBodyViewBase64EncodesGzipRequestBody(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   3,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type":     {"application/json"},
				"Content-Encoding": {"gzip"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})

	requestBody, err := compressWithGzip([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("compressWithGzip: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestBody); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}

	if !view.RequestBodyBinary {
		t.Fatal("RequestBodyBinary = false, want true")
	}
	if view.RequestBody != string(requestBody) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, requestBody)
	}
}

func TestGetTrafficBodyViewShowsDecodedGzipJSONBodiesAsText(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   31,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type":     {"application/json"},
				"Content-Encoding": {"gzip"},
			}),
		},
		Response: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type":     {"application/json"},
				"Content-Encoding": {"gzip"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})

	requestBody := []byte(`{"request":true}`)
	responseBody := []byte(`{"response":true}`)
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestBody); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindResponse, responseBody); err != nil {
		t.Fatalf("cache.Write response: %v", err)
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}

	if view.RequestBodyBinary {
		t.Fatal("RequestBodyBinary = true, want false")
	}
	if view.RequestBody != string(requestBody) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, string(requestBody))
	}
	if view.ResponseBodyBinary {
		t.Fatal("ResponseBodyBinary = true, want false")
	}
	if view.ResponseBody != string(responseBody) {
		t.Fatalf("ResponseBody = %q, want %q", view.ResponseBody, string(responseBody))
	}
}

func TestGetTrafficBodyViewMarksInvalidUTF8RequestBodyBinary(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{
		ID:   4,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: testHeaderFields(map[string][]string{
				"Content-Type": {"text/plain; charset=utf-8"},
			}),
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{})

	requestBody := []byte{0x1f, 0x8b, 0x08}
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestBody); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}

	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}

	if !view.RequestBodyBinary {
		t.Fatal("RequestBodyBinary = false, want true")
	}
	if view.RequestBody != string(requestBody) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, requestBody)
	}
}

func TestDeleteTrafficEntryRemovesCachedBodyFiles(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{ID: 7, Type: "http"}
	svc.storeTrafficEntry(entry)
	if err := cache.Write(entry.ID, bodycache.KindRequest, []byte("cached")); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	svc.deleteTrafficEntry(entry.ID)

	if cache.Has(entry.ID, bodycache.KindRequest) {
		t.Fatal("cache entry should be removed after deleteTrafficEntry")
	}
	if _, err := os.Stat(filepath.Join(cache.SessionDir(), "7_req.body")); !os.IsNotExist(err) {
		t.Fatalf("request cache file should be removed, got %v", err)
	}
}

func TestDeleteBetweenCaptureCheckAndAttachLeavesNoCaptureState(t *testing.T) {
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 8,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// deleteDuringAttach blocks the capture side right after its tombstone
	// check and deletes the entry from another goroutine before letting the
	// capture attach its state.
	deleteDuringAttach := func(t *testing.T, entry *TrafficEntry, attach func()) {
		t.Helper()
		passedCheck := make(chan struct{})
		continueAttach := make(chan struct{})
		var once sync.Once
		svc.captureAttachPrecheckHook = func() {
			once.Do(func() {
				close(passedCheck)
				<-continueAttach
			})
		}
		t.Cleanup(func() { svc.captureAttachPrecheckHook = nil })
		attached := make(chan struct{})
		go func() {
			defer close(attached)
			attach()
		}()
		<-passedCheck
		deleted := make(chan struct{})
		go func() {
			svc.deleteTrafficEntry(entry.ID)
			close(deleted)
		}()
		select {
		case <-deleted:
		case <-time.After(2 * time.Second):
			t.Fatal("delete blocked behind the capture attach")
		}
		close(continueAttach)
		<-attached
	}
	assertNoCaptureState := func(t *testing.T, id uint64) {
		t.Helper()
		if _, ok := svc.trafficBodies.Load(id); ok {
			t.Fatal("trafficBodies still holds the deleted entry")
		}
		if _, ok := svc.trafficWsMsgs.Load(id); ok {
			t.Fatal("trafficWsMsgs still holds the deleted entry")
		}
		files, err := os.ReadDir(cache.SessionDir())
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		prefix := strconv.FormatUint(id, 10) + "_"
		for _, file := range files {
			if strings.HasPrefix(file.Name(), prefix) {
				t.Fatalf("body cache still holds %s", file.Name())
			}
		}
	}

	t.Run("body", func(t *testing.T) {
		entry := svc.newTrafficEntry(TrafficEntry{Type: "http", Method: http.MethodPost})
		svc.storeTrafficEntry(entry)
		payload := bytes.Repeat([]byte("orphan-body"), 64)
		var reader io.ReadCloser
		deleteDuringAttach(t, entry, func() {
			reader = svc.newCaptureStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), entry, "", true)
		})
		got, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("forwarded body changed after the entry was deleted")
		}
		assertNoCaptureState(t, entry.ID)
	})

	t.Run("websocket", func(t *testing.T) {
		entry := svc.newTrafficEntry(TrafficEntry{Type: "ws", Method: http.MethodGet})
		svc.storeTrafficEntry(entry)
		var wsMsgs *TrafficWsMsgs
		deleteDuringAttach(t, entry, func() {
			wsMsgs = svc.newCaptureWebSocketMessages(entry)
		})
		if wsMsgs != nil {
			t.Fatal("deleted entry received WebSocket message storage")
		}
		assertNoCaptureState(t, entry.ID)
	})
}

type testHistoryCleaner struct {
	calls int
	err   error
}

func (c *testHistoryCleaner) ClearHistories() error {
	c.calls++
	return c.err
}

func TestClearCacheAndHistoryClearsCurrentHistoryAndCacheAndPreservesUnknownHistory(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	historyCleaner := &testHistoryCleaner{}
	svc.SetHistoryCleaner(historyCleaner)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}

	cache, err := bodycache.New(filepath.Join(baseDir, "cache", "session-a"))
	if err != nil {
		t.Fatalf("bodycache.New: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{ID: 9, Type: "http"}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{
		requestBody: testCapturedBodyFromBytes(t, entry.ID, bodycache.KindRequest, []byte("in-memory")),
	})
	if err := cache.Write(entry.ID, bodycache.KindRequest, []byte("cached")); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll historyDir: %v", err)
	}
	unknownHistoryFile := filepath.Join(historyDir, "unsupported.hbin")
	if err := os.WriteFile(unknownHistoryFile, []byte("unsupported"), 0o644); err != nil {
		t.Fatalf("WriteFile history: %v", err)
	}
	currentHistoryPaths := []string{
		filepath.Join(historyDir, fs.GetHBinFileName(svc.currentHistoryMetadata.Key)),
		filepath.Join(historyDir, fs.GetHIdxFileName(svc.currentHistoryMetadata.Key)),
	}
	for _, path := range currentHistoryPaths {
		if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
			t.Fatalf("WriteFile current history: %v", err)
		}
	}

	requestDraftCacheDir, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath: %v", err)
	}
	requestDraftCacheFile := filepath.Join(requestDraftCacheDir, "request.bin")
	if err := os.WriteFile(requestDraftCacheFile, []byte("request-draft-cache"), 0o644); err != nil {
		t.Fatalf("WriteFile request draft cache: %v", err)
	}

	if _, err := svc.ClearCacheAndHistory(); err != nil {
		t.Fatalf("ClearCacheAndHistory: %v", err)
	}

	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
	for _, path := range currentHistoryPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("current history file should be removed, got %v", err)
		}
	}
	if _, err := os.Stat(unknownHistoryFile); err != nil {
		t.Fatalf("unsupported history file should be preserved, got %v", err)
	}
	if historyCleaner.calls != 1 {
		t.Fatalf("history cleaner calls = %d, want 1", historyCleaner.calls)
	}
	if _, err := os.Stat(requestDraftCacheFile); !os.IsNotExist(err) {
		t.Fatalf("request draft cache file should be removed, got %v", err)
	}
	if svc.bodyCache != nil {
		t.Fatal("body cache should remain nil when proxy is not running")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "cache", "session-a", "9_req.body")); !os.IsNotExist(err) {
		t.Fatalf("old cache file should be removed, got %v", err)
	}
}

func TestGetLocalDataSizeIncludesRequestDraftCache(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	bodyCacheDir := filepath.Join(baseDir, "cache", "session-a")
	if err := os.MkdirAll(bodyCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll body cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bodyCacheDir, "1_req.body"), []byte("1234"), 0o644); err != nil {
		t.Fatalf("WriteFile body cache: %v", err)
	}

	requestDraftCacheDir, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath: %v", err)
	}
	if err := os.WriteFile(filepath.Join(requestDraftCacheDir, "request.bin"), []byte("123456"), 0o644); err != nil {
		t.Fatalf("WriteFile request draft cache: %v", err)
	}

	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll history: %v", err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, "sample.hbin"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("WriteFile history: %v", err)
	}

	size, err := svc.GetLocalDataSize()
	if err != nil {
		t.Fatalf("GetLocalDataSize: %v", err)
	}
	if size.CacheBytes != 10 {
		t.Fatalf("CacheBytes = %d, want 10", size.CacheBytes)
	}
	if size.HistoryBytes != 5 {
		t.Fatalf("HistoryBytes = %d, want 5", size.HistoryBytes)
	}
}

func TestClearCacheFilesRemovesCachesButKeepsHistory(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}

	cache, err := bodycache.New(filepath.Join(baseDir, "cache", "session-a"))
	if err != nil {
		t.Fatalf("bodycache.New: %v", err)
	}
	svc.bodyCache = cache

	entry := &TrafficEntry{ID: 11, Type: "http"}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{
		requestBody: testCapturedBodyFromBytes(t, entry.ID, bodycache.KindRequest, []byte("in-memory")),
	})
	if err := cache.Write(entry.ID, bodycache.KindRequest, []byte("cached")); err != nil {
		t.Fatalf("cache.Write: %v", err)
	}

	requestDraftCacheDir, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath: %v", err)
	}
	requestDraftCacheFile := filepath.Join(requestDraftCacheDir, "request.bin")
	if err := os.WriteFile(requestDraftCacheFile, []byte("request-draft-cache"), 0o644); err != nil {
		t.Fatalf("WriteFile request draft cache: %v", err)
	}

	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll historyDir: %v", err)
	}
	historyFile := filepath.Join(historyDir, "sample.hbin")
	if err := os.WriteFile(historyFile, []byte("history"), 0o644); err != nil {
		t.Fatalf("WriteFile history: %v", err)
	}

	if _, err := svc.ClearCacheFiles(); err != nil {
		t.Fatalf("ClearCacheFiles: %v", err)
	}

	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
	if _, err := os.Stat(requestDraftCacheFile); !os.IsNotExist(err) {
		t.Fatalf("request draft cache file should be removed, got %v", err)
	}
	if _, err := os.Stat(historyFile); err != nil {
		t.Fatalf("history file should remain, got %v", err)
	}
	if svc.bodyCache != nil {
		t.Fatal("body cache should remain nil when proxy is not running")
	}
}

func TestClearCacheFilesStartsNewHistoryWithoutOverwritingSavedCapture(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	svc.currentHistoryMetadata.Alias = "saved capture"
	oldEntry := svc.newTrafficEntry(TrafficEntry{
		Type:      "http",
		Method:    "GET",
		URL:       "https://old.example/",
		Host:      "old.example",
		Path:      "/",
		StartedAt: time.Now(),
	})
	if !svc.storeTrafficEntry(oldEntry) {
		t.Fatal("store old traffic entry")
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flush old capture: %v", err)
	}

	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("get history path: %v", err)
	}
	oldKey := svc.currentHistoryMetadata.Key
	oldBinPath := filepath.Join(historyDir, fs.GetHBinFileName(oldKey))
	oldIndexPath := filepath.Join(historyDir, fs.GetHIdxFileName(oldKey))
	oldBin, err := os.ReadFile(oldBinPath)
	if err != nil {
		t.Fatalf("read old hbin: %v", err)
	}
	oldIndex, err := os.ReadFile(oldIndexPath)
	if err != nil {
		t.Fatalf("read old hidx: %v", err)
	}

	if _, err := svc.ClearCacheFiles(); err != nil {
		t.Fatalf("ClearCacheFiles: %v", err)
	}
	newKey := svc.currentHistoryMetadata.Key
	if newKey == oldKey {
		t.Fatalf("history key was reused after cache clear: %s", oldKey)
	}
	if svc.currentHistoryMetadata.Alias != "" {
		t.Fatalf("new capture alias = %q, want empty", svc.currentHistoryMetadata.Alias)
	}

	freshEntry := svc.newTrafficEntry(TrafficEntry{
		Type:      "http",
		Method:    "GET",
		URL:       "https://new.example/",
		Host:      "new.example",
		Path:      "/",
		StartedAt: time.Now(),
	})
	if !svc.storeTrafficEntry(freshEntry) {
		t.Fatal("store fresh traffic entry")
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flush fresh capture: %v", err)
	}

	if got, readErr := os.ReadFile(oldBinPath); readErr != nil {
		t.Fatalf("read preserved hbin: %v", readErr)
	} else if !bytes.Equal(got, oldBin) {
		t.Fatal("cache clear allowed the next capture to overwrite the saved hbin")
	}
	if got, readErr := os.ReadFile(oldIndexPath); readErr != nil {
		t.Fatalf("read preserved hidx: %v", readErr)
	} else if !bytes.Equal(got, oldIndex) {
		t.Fatal("cache clear allowed the next capture to overwrite the saved hidx")
	}
	for _, path := range []string{
		filepath.Join(historyDir, fs.GetHBinFileName(newKey)),
		filepath.Join(historyDir, fs.GetHIdxFileName(newKey)),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("new capture history file missing: %v", statErr)
		}
	}
}

func TestClearCacheFilesAbortsOpenBodySpoolBeforeRemovingCacheDir(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	cache, err := bodycache.New(filepath.Join(baseDir, "cache", "session-open"))
	if err != nil {
		t.Fatalf("bodycache.New: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 8,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	entry := &TrafficEntry{ID: 12, Type: "http"}
	svc.storeTrafficEntry(entry)
	reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("z"), 64))), entry.ID, "", true)
	buf := make([]byte, 16)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}

	tmpPath := filepath.Join(cache.SessionDir(), "12_req.body.tmp")
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("tmp cache file should exist before ClearCacheFiles: %v", err)
	}

	if _, err := svc.ClearCacheFiles(); err != nil {
		t.Fatalf("ClearCacheFiles: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close reader: %v", err)
	}

	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("tmp cache file should be removed after ClearCacheFiles, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "cache")); !os.IsNotExist(err) {
		t.Fatalf("cache dir should be removed after ClearCacheFiles, got %v", err)
	}
	if svc.bodyCache != nil {
		t.Fatal("body cache should remain nil when proxy is not running")
	}
}

func TestClearCacheFilesSynchronizesWithNewBodyCaptures(t *testing.T) {
	setTestConfigDir(t)

	svc := newTestProxyService(t, nil)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	cache, err := bodycache.New(filepath.Join(baseDir, "cache", "session-race"))
	if err != nil {
		t.Fatalf("bodycache.New: %v", err)
	}
	svc.bodyCache = cache
	if err := svc.settingService.Update(&settingservice.Settings{
		ProxyConfig: &settingservice.ProxyConfig{},
		CacheConfig: &settingservice.CacheConfig{
			BodyCacheThresholdBytes: 8,
			MaxWsMessages:           1000,
		},
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	var nextID atomic.Uint64
	stop := make(chan struct{})
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			payload := bytes.Repeat([]byte("c"), 64)
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := nextID.Add(1)
				reader := svc.newStreamBodyReader(io.NopCloser(bytes.NewReader(payload)), id, "", true)
				if _, err := io.Copy(io.Discard, reader); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				if err := reader.Close(); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		})
	}

	for range 5 {
		if _, err := svc.ClearCacheFiles(); err != nil {
			t.Fatalf("ClearCacheFiles: %v", err)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatalf("capture goroutine failed: %v", err)
	default:
	}
}

func TestClearTrafficClearsBodyBuffersAndWebSocketMessages(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, nil)
	entry := &TrafficEntry{ID: 21, Type: "ws"}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{
		requestBody:  testCapturedBodyFromBytes(t, entry.ID, bodycache.KindRequest, []byte("request-body")),
		responseBody: testCapturedBodyFromBytes(t, entry.ID, bodycache.KindResponse, []byte("response-body")),
	})
	svc.trafficWsMsgs.Store(entry.ID, &TrafficWsMsgs{
		Messages: []*WebSocketMessage{
			{Direction: "receive", MsgType: "text", Data: "payload"},
		},
	})

	svc.ClearTraffic()

	if svc.trafficEntries.Len() != 0 {
		t.Fatalf("trafficEntries.Len = %d, want 0", svc.trafficEntries.Len())
	}
	if _, ok := svc.trafficBodies.Load(entry.ID); ok {
		t.Fatal("traffic bodies should be removed after ClearTraffic")
	}
	if value, ok := svc.trafficWsMsgs.Load(entry.ID); ok {
		wsMsgs := value.(*TrafficWsMsgs)
		if wsMsgs.Messages != nil {
			t.Fatal("websocket messages should be nil after ClearTraffic")
		}
	}
}
