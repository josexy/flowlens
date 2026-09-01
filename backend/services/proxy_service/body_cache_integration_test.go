package proxyservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
	"github.com/josexy/flowlens/backend/pkg/fs"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
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
	if view.ResponseBodyEncoding != "base64" {
		t.Fatalf("ResponseBodyEncoding = %q, want base64", view.ResponseBodyEncoding)
	}
	if view.ResponseBody != "AAEC" {
		t.Fatalf("ResponseBody = %q, want AAEC", view.ResponseBody)
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

	wantEncoded := base64.StdEncoding.EncodeToString(requestBody)
	if view.RequestBodyEncoding != "base64" {
		t.Fatalf("RequestBodyEncoding = %q, want base64", view.RequestBodyEncoding)
	}
	if view.RequestBody != wantEncoded {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, wantEncoded)
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
		done <- svc.ClearCacheFiles()
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

	wantEncoded := base64.StdEncoding.EncodeToString(requestBody)
	if view.RequestBodyEncoding != "base64" {
		t.Fatalf("RequestBodyEncoding = %q, want base64", view.RequestBodyEncoding)
	}
	if view.RequestBody != wantEncoded {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, wantEncoded)
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

	if view.RequestBodyEncoding != "" {
		t.Fatalf("RequestBodyEncoding = %q, want empty", view.RequestBodyEncoding)
	}
	if view.RequestBody != string(requestBody) {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, string(requestBody))
	}
	if view.ResponseBodyEncoding != "" {
		t.Fatalf("ResponseBodyEncoding = %q, want empty", view.ResponseBodyEncoding)
	}
	if view.ResponseBody != string(responseBody) {
		t.Fatalf("ResponseBody = %q, want %q", view.ResponseBody, string(responseBody))
	}
}

func TestGetTrafficBodyViewBase64EncodesInvalidUTF8RequestBody(t *testing.T) {
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

	wantEncoded := base64.StdEncoding.EncodeToString(requestBody)
	if view.RequestBodyEncoding != "base64" {
		t.Fatalf("RequestBodyEncoding = %q, want base64", view.RequestBodyEncoding)
	}
	if view.RequestBody != wantEncoded {
		t.Fatalf("RequestBody = %q, want %q", view.RequestBody, wantEncoded)
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

	if err := svc.ClearCacheAndHistory(); err != nil {
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

	if err := svc.ClearCacheFiles(); err != nil {
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

	if err := svc.ClearCacheFiles(); err != nil {
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

	if err := svc.ClearCacheFiles(); err != nil {
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
		if err := svc.ClearCacheFiles(); err != nil {
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
