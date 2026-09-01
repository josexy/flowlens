package proxyservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/orderedmap"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func testHeaderFields(headers map[string][]string) []HTTPHeaderField {
	fields := make([]HTTPHeaderField, 0, len(headers))
	for name, values := range headers {
		for _, value := range values {
			fields = append(fields, HTTPHeaderField{Name: name, Value: value})
		}
	}
	return fields
}

func TestSendHTTPRequestTextBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		body                SendRequestBody
		wantContentType     string
		wantRequestBody     string
		responseContentType string
		responseBody        string
	}{
		{
			name: "json",
			body: SendRequestBody{
				BodyType: SendRequestBodyTypeJSON,
				Text:     `{"hello":"world"}`,
			},
			wantContentType:     "application/json",
			wantRequestBody:     `{"hello":"world"}`,
			responseContentType: "application/json",
			responseBody:        `{"ok":true}`,
		},
		{
			name: "text",
			body: SendRequestBody{
				BodyType: SendRequestBodyTypeText,
				Text:     "plain-text-body",
			},
			wantContentType:     "text/plain; charset=utf-8",
			wantRequestBody:     "plain-text-body",
			responseContentType: "text/plain; charset=utf-8",
			responseBody:        "text-response",
		},
		{
			name: "xml",
			body: SendRequestBody{
				BodyType: SendRequestBodyTypeXML,
				Text:     "<node>value</node>",
			},
			wantContentType:     "application/xml",
			wantRequestBody:     "<node>value</node>",
			responseContentType: "application/xml",
			responseBody:        "<ok/>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if got := r.Header.Get("Content-Type"); got != tt.wantContentType {
					t.Fatalf("content type = %q, want %q", got, tt.wantContentType)
				}
				if got := string(bodyBytes); got != tt.wantRequestBody {
					t.Fatalf("request body = %q, want %q", got, tt.wantRequestBody)
				}

				w.Header().Set("Content-Type", tt.responseContentType)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			svc := newTestProxyService(t, &settingservice.ProxyConfig{})
			resp, err := svc.SendHTTPRequest(
				context.Background(),
				SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodPost,
				server.URL,
				nil,
				tt.body,
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest returned error: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			if resp.Body != tt.responseBody {
				t.Fatalf("response body = %q, want %q", resp.Body, tt.responseBody)
			}
			if resp.BodyEncoding != "" {
				t.Fatalf("response body encoding = %q, want empty", resp.BodyEncoding)
			}
		})
	}
}

func TestSendHTTPRequestNoneBodyOmitsContentType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Fatalf("content type = %q, want empty", got)
		}
		if got := len(bodyBytes); got != 0 {
			t.Fatalf("request body length = %d, want 0", got)
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "ok" {
		t.Fatalf("response body = %q, want ok", resp.Body)
	}
}

func TestSendHTTPRequestSSEReturnsAfterResponseHeaders(t *testing.T) {
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: first\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	eventCh := make(chan HTTPRequestStreamEvent, 4)
	svc.emitHTTPRequestEventHook = func(event HTTPRequestStreamEvent) {
		eventCh <- event
	}

	resultCh := make(chan struct {
		response SendRequestResponse
		err      error
	}, 1)
	callCtx, cancelCall := context.WithCancel(context.Background())
	go func() {
		response, err := svc.SendHTTPRequest(
			callCtx,
			SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
			http.MethodGet,
			server.URL,
			nil,
			SendRequestBody{BodyType: SendRequestBodyTypeNone},
		)
		resultCh <- struct {
			response SendRequestResponse
			err      error
		}{response: response, err: err}
	}()

	var result struct {
		response SendRequestResponse
		err      error
	}
	select {
	case result = <-resultCh:
	case <-time.After(500 * time.Millisecond):
		svc.baseCancel()
		<-resultCh
		t.Fatal("SendHTTPRequest did not return after SSE response headers were flushed")
	}
	if result.err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", result.err)
	}
	if !result.response.Streaming || result.response.StreamSessionID == "" {
		t.Fatalf("streaming response = %#v", result.response)
	}
	if result.response.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want 200", result.response.StatusCode)
	}
	cancelCall()
	select {
	case <-handlerDone:
		t.Fatal("SSE stream stopped when the completed binding call context was cancelled")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case event := <-eventCh:
		if event.EventType != "chunk" || event.SessionID != result.response.StreamSessionID {
			t.Fatalf("stream event = %#v", event)
		}
		chunk, err := base64.StdEncoding.DecodeString(event.ChunkBase64)
		if err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		if string(chunk) != "data: first\n\n" {
			t.Fatalf("chunk = %q", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE chunk event")
	}

	if err := svc.DisconnectHTTPRequestStream(result.response.StreamSessionID); err != nil {
		t.Fatalf("DisconnectHTTPRequestStream: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not stop after disconnect")
	}
}

func TestSendHTTPRequestCancellationBeforeResponseHeaders(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	t.Cleanup(svc.baseCancel)
	callCtx, cancelCall := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.SendHTTPRequest(
			callCtx,
			SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
			http.MethodGet,
			server.URL,
			nil,
			SendRequestBody{BodyType: SendRequestBodyTypeNone},
		)
		resultCh <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		cancelCall()
		t.Fatal("timed out waiting for HTTP request to start")
	}
	cancelCall()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SendHTTPRequest error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendHTTPRequest did not stop after its binding context was cancelled")
	}
}

func TestSendHTTPRequestCancellationWhileReadingResponseBody(t *testing.T) {
	responseStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(responseStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	t.Cleanup(svc.baseCancel)
	callCtx, cancelCall := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.SendHTTPRequest(
			callCtx,
			SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
			http.MethodGet,
			server.URL,
			nil,
			SendRequestBody{BodyType: SendRequestBodyTypeNone},
		)
		resultCh <- err
	}()

	select {
	case <-responseStarted:
	case <-time.After(2 * time.Second):
		cancelCall()
		t.Fatal("timed out waiting for HTTP response body")
	}
	cancelCall()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SendHTTPRequest error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendHTTPRequest did not stop while reading the response body")
	}
}

func TestSendHTTPRequestSSECompletionIncludesTrailers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Trailer", "X-Stream-Result")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: done\n\n"))
		w.Header().Set("X-Stream-Result", "complete")
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	eventCh := make(chan HTTPRequestStreamEvent, 4)
	svc.emitHTTPRequestEventHook = func(event HTTPRequestStreamEvent) {
		eventCh <- event
	}
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if !response.Streaming || response.StreamSessionID == "" {
		t.Fatalf("streaming response = %#v", response)
	}

	var completeEvent HTTPRequestStreamEvent
	deadline := time.After(2 * time.Second)
	for completeEvent.EventType == "" {
		select {
		case event := <-eventCh:
			if event.EventType == "complete" {
				completeEvent = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for SSE completion event")
		}
	}
	wantTrailers := []HTTPHeaderField{{Name: "X-Stream-Result", Value: "complete"}}
	if !reflect.DeepEqual(completeEvent.TrailerFields, wantTrailers) {
		t.Fatalf("completion trailer fields = %#v, want %#v", completeEvent.TrailerFields, wantTrailers)
	}
	if completeEvent.TrailersTruncated || completeEvent.TrailerOrderUnavailable {
		t.Fatalf("completion trailer state = truncated:%v unavailable:%v", completeEvent.TrailersTruncated, completeEvent.TrailerOrderUnavailable)
	}
}

func TestSendHTTPRequestResponseTrailers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Trailer", "X-Trace, X-Checksum")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		w.Header().Set("X-Trace", "trace-1")
		w.Header().Set("X-Checksum", "sum-1")
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}

	if resp.Body != "ok" {
		t.Fatalf("body = %q, want ok", resp.Body)
	}
	if got := firstHeaderFieldValue(resp.TrailerFields, "X-Trace"); got != "trace-1" {
		t.Fatalf("X-Trace trailer = %q", got)
	}
	if got := firstHeaderFieldValue(resp.TrailerFields, "X-Checksum"); got != "sum-1" {
		t.Fatalf("X-Checksum trailer = %q", got)
	}
	if len(resp.TrailerFields) != 2 {
		t.Fatalf("trailer fields = %#v", resp.TrailerFields)
	}
	if resp.TrailersTruncated || resp.TrailerOrderUnavailable {
		t.Fatalf("trailer state = truncated:%v unavailable:%v", resp.TrailersTruncated, resp.TrailerOrderUnavailable)
	}
}

func TestCompleteResponseTrailerFieldsFallbackPreservesEmptyValues(t *testing.T) {
	t.Parallel()

	fields, truncated, unavailable := completeResponseTrailerFields(&http.Response{Trailer: http.Header{
		"Trailer":           {"X-Trace"},
		"Content-Length":    {"2"},
		"Transfer-Encoding": {"chunked"},
		"X-Empty":           {""},
		"X-Trace":           {"trace-1", "trace-2"},
	}}, nil)

	want := []HTTPHeaderField{
		{Name: "X-Empty", Value: ""},
		{Name: "X-Trace", Value: "trace-1"},
		{Name: "X-Trace", Value: "trace-2"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("trailer fields = %#v, want %#v", fields, want)
	}
	if truncated || !unavailable {
		t.Fatalf("fallback trailer state = truncated:%v unavailable:%v", truncated, unavailable)
	}
}

func TestResponseStreamBodyReaderRunsExtraOnDoneAfterDrain(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	done := make(chan struct{})
	reader := svc.newStreamBodyReader(
		io.NopCloser(strings.NewReader("payload")),
		999,
		"",
		false,
		func() {
			close(done)
		},
	)

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("body = %q, want payload", body)
	}
	select {
	case <-done:
	default:
		t.Fatal("extra onDone callback was not called after response body drain")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestFillResponseTrailersUpdatesStoredEntry(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	entry := &TrafficEntry{
		ID:       1001,
		Type:     "http",
		Response: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{}},
	}
	svc.storeTrafficEntry(entry)
	var emittedPatch TrafficEntryPatch
	svc.emitTrafficPatchHook = func(patch TrafficEntryPatch) { emittedPatch = patch }

	updated := svc.fillResponseTrailers(&http.Response{
		Trailer: http.Header{
			"X-Trace":        {"trace-1"},
			"Content-Length": {"2"},
		},
	}, entry)
	if !updated {
		t.Fatal("fillResponseTrailers returned false")
	}

	stored, ok := svc.trafficEntries.Get(entry.ID)
	if !ok {
		t.Fatal("stored entry not found")
	}
	if !reflect.DeepEqual(stored.Response.TrailerFields, []HTTPHeaderField{
		{Name: "X-Trace", Value: "trace-1"},
	}) {
		t.Fatalf("trailer fields = %#v", stored.Response.TrailerFields)
	}
	if !stored.Response.TrailerOrderUnavailable {
		t.Fatal("fallback trailer order should be marked unavailable")
	}
	if !svc.historyDirty.Load() {
		t.Fatal("history should be marked dirty after trailer update")
	}
	if emittedPatch.TrafficID != entry.ID || emittedPatch.Revision != stored.Revision ||
		emittedPatch.ResponseTrailers == nil || emittedPatch.ResponseHeaders != nil {
		t.Fatalf("emitted trailer patch = %+v", emittedPatch)
	}
}

func TestSendHTTPRequestReencodesGzipRequestBody(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"request":true}`)
	type receivedRequest struct {
		body            []byte
		contentEncoding string
		contentType     string
		contentLength   int64
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
			contentType:     r.Header.Get("Content-Type"),
			contentLength:   r.ContentLength,
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		server.URL,
		testHeaderFields(map[string][]string{
			"Content-Encoding": {"gzip"},
		}),
		SendRequestBody{
			BodyType: SendRequestBodyTypeJSON,
			Text:     string(payload),
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "ok" {
		t.Fatalf("response body = %q, want ok", resp.Body)
	}

	select {
	case got := <-received:
		if got.contentEncoding != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", got.contentEncoding)
		}
		if got.contentType != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got.contentType)
		}
		if got.contentLength != -1 {
			t.Fatalf("ContentLength = %d, want -1 for encoded streaming body", got.contentLength)
		}
		if bytes.Equal(got.body, payload) {
			t.Fatal("request body was not gzip encoded")
		}
		decoded := decodeTestBodyWithContentEncoding(t, got.body, got.contentEncoding)
		if !bytes.Equal(decoded, payload) {
			t.Fatalf("decoded request body = %q, want %q", string(decoded), string(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
}

func TestSendHTTPRequestURLEncodedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content type = %q, want application/x-www-form-urlencoded", got)
		}
		wantBody := "first=one&first=two&space=a+b&symbols=%25%26%3D&empty="
		if got := string(bodyBytes); got != wantBody {
			t.Fatalf("request body = %q, want %q", got, wantBody)
		}
		if got := r.ContentLength; got != int64(len(wantBody)) {
			t.Fatalf("content length = %d, want %d", got, len(wantBody))
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		server.URL,
		testHeaderFields(map[string][]string{
			"Content-Type": {"text/plain"},
		}),
		SendRequestBody{
			BodyType: SendRequestBodyTypeURLEncoded,
			URLEncoded: []*SendRequestURLEncodedItem{
				{Enabled: true, Name: "first", Value: "one"},
				{Enabled: false, Name: "disabled", Value: "skip"},
				{Enabled: true, Name: "", Value: "skip-empty-name"},
				{Enabled: true, Name: "first", Value: "two"},
				{Enabled: true, Name: "space", Value: "a b"},
				{Enabled: true, Name: "symbols", Value: "%&="},
				{Enabled: true, Name: "empty", Value: ""},
			},
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "ok" {
		t.Fatalf("response body = %q, want ok", resp.Body)
	}
}

func TestSendHTTPRequestUsesFallbackUAHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != UserAgentHeader {
			t.Fatalf("user-agent = %q, want request-editor-ua", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet,
		server.URL,
		testHeaderFields(map[string][]string{}),
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
}

func TestSendHTTPRequestPrefersExplicitUserAgentOverUAFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "explicit-ua" {
			t.Fatalf("user-agent = %q, want explicit-ua", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet,
		server.URL,
		testHeaderFields(map[string][]string{
			"User-Agent": {"explicit-ua"},
		}),
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
}

func TestSendHTTPRequestFileBodyStreamsFile(t *testing.T) {
	t.Parallel()

	fileContent := bytes.Repeat([]byte("0123456789abcdef"), 8192)
	filePath := writeTempFile(t, "upload-*.bin", fileContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("content type = %q, want application/octet-stream", got)
		}
		if got := r.ContentLength; got != int64(len(fileContent)) {
			t.Fatalf("content length = %d, want %d", got, len(fileContent))
		}
		if !bytes.Equal(bodyBytes, fileContent) {
			t.Fatalf("uploaded file content mismatch")
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		server.URL,
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeFile,
			File: &SendRequestFile{
				Path: filePath,
				Name: filepath.Base(filePath),
				Size: int64(len(fileContent)),
			},
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}

	wantEncoded := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})
	if resp.Body != wantEncoded {
		t.Fatalf("response body = %q, want %q", resp.Body, wantEncoded)
	}
	if resp.BodyEncoding != "base64" {
		t.Fatalf("response body encoding = %q, want base64", resp.BodyEncoding)
	}
}

func TestSendHTTPRequestReencodesGzipFileBody(t *testing.T) {
	t.Parallel()

	fileContent := bytes.Repeat([]byte("0123456789abcdef"), 8192)
	filePath := writeTempFile(t, "encoded-upload-*.bin", fileContent)

	type receivedRequest struct {
		body            []byte
		contentEncoding string
		contentType     string
		contentLength   int64
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
			contentType:     r.Header.Get("Content-Type"),
			contentLength:   r.ContentLength,
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		server.URL,
		testHeaderFields(map[string][]string{
			"Content-Encoding": {"gzip"},
		}),
		SendRequestBody{
			BodyType: SendRequestBodyTypeFile,
			File: &SendRequestFile{
				Path: filePath,
				Name: filepath.Base(filePath),
				Size: int64(len(fileContent)),
			},
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "ok" {
		t.Fatalf("response body = %q, want ok", resp.Body)
	}

	select {
	case got := <-received:
		if got.contentEncoding != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", got.contentEncoding)
		}
		if got.contentType != "application/octet-stream" {
			t.Fatalf("Content-Type = %q, want application/octet-stream", got.contentType)
		}
		if got.contentLength != -1 {
			t.Fatalf("ContentLength = %d, want -1 for encoded streaming body", got.contentLength)
		}
		if bytes.Equal(got.body, fileContent) {
			t.Fatal("request body was not gzip encoded")
		}
		decoded := decodeTestBodyWithContentEncoding(t, got.body, got.contentEncoding)
		if !bytes.Equal(decoded, fileContent) {
			t.Fatal("decoded file body mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request body")
	}
}

func TestSendHTTPRequestFormDataStreamsMultipart(t *testing.T) {
	t.Parallel()

	fileOneContent := []byte("file-one-content")
	fileTwoContent := []byte("file-two-content")
	fileOnePath := writeTempFile(t, "form-one-*.txt", fileOneContent)
	fileTwoPath := writeTempFile(t, "form-two-*.txt", fileTwoContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		if r.ContentLength != -1 {
			t.Fatalf("content length = %d, want -1 for streamed multipart", r.ContentLength)
		}

		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm failed: %v", err)
		}

		if got := r.FormValue("title"); got != "streamed" {
			t.Fatalf("form value title = %q, want streamed", got)
		}
		if got := r.FormValue("notes"); got != "kept" {
			t.Fatalf("form value notes = %q, want kept", got)
		}
		if got := r.FormValue("disabled"); got != "" {
			t.Fatalf("disabled field should be skipped, got %q", got)
		}
		if got := r.FormValue(""); got != "" {
			t.Fatalf("empty-name field should be skipped, got %q", got)
		}

		assertMultipartFile(t, r, "fileA", "alpha.txt", fileOneContent)
		assertMultipartFile(t, r, "fileB", filepath.Base(fileTwoPath), fileTwoContent)

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		server.URL,
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeFormData,
			FormData: []*SendRequestFormDataItem{
				{Enabled: true, Name: "title", ItemType: "text", Value: "streamed"},
				{Enabled: false, Name: "disabled", ItemType: "text", Value: "skip-me"},
				{Enabled: true, Name: "", ItemType: "text", Value: "skip-empty-name"},
				{
					Enabled:  true,
					Name:     "fileA",
					ItemType: "file",
					File: &SendRequestFile{
						Path: fileOnePath,
						Name: "alpha.txt",
						Size: int64(len(fileOneContent)),
					},
				},
				{
					Enabled:  true,
					Name:     "fileB",
					ItemType: "file",
					File: &SendRequestFile{
						Path: fileTwoPath,
						Name: "",
						Size: int64(len(fileTwoContent)),
					},
				},
				{Enabled: true, Name: "notes", ItemType: "text", Value: "kept"},
			},
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "ok" {
		t.Fatalf("response body = %q, want ok", resp.Body)
	}
}

func TestSendHTTPRequestFormDataInvalidFileFails(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost,
		"http://example.com",
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeFormData,
			FormData: []*SendRequestFormDataItem{
				{
					Enabled:  true,
					Name:     "fileA",
					ItemType: "file",
					File: &SendRequestFile{
						Path: filepath.Join(t.TempDir(), "missing.txt"),
						Name: "missing.txt",
					},
				},
			},
		},
	)
	if err == nil {
		t.Fatal("expected error for invalid form-data file path")
	}
	if !strings.Contains(err.Error(), "invalid form-data file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendHTTPRequestTimeoutMs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode: SendRequestProxyModeNone,
			TimeoutMs: 50,
		},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeText,
		},
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") && !strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func TestSendHTTPRequestCustomProxyPathStillWorks(t *testing.T) {
	t.Parallel()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxied"))
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != strings.TrimPrefix(targetServer.URL, "http://") {
			t.Fatalf("proxy received host %q, want %q", r.URL.Host, strings.TrimPrefix(targetServer.URL, "http://"))
		}

		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetServer.URL, r.Body)
		if err != nil {
			t.Fatalf("build upstream request: %v", err)
		}
		outReq.Header = r.Header.Clone()

		resp, err := http.DefaultTransport.RoundTrip(outReq)
		if err != nil {
			t.Fatalf("proxy round trip: %v", err)
		}
		defer resp.Body.Close()

		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:   SendRequestProxyModeCustom,
			CustomProxy: proxyServer.URL,
		},
		http.MethodPost,
		targetServer.URL,
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeText,
			Text:     "through-proxy",
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "proxied" {
		t.Fatalf("response body = %q, want proxied", resp.Body)
	}
}

func TestSendHTTPRequestSystemProxyPathUsesEnvironmentProxy(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("system-proxied"))
	}))
	defer targetServer.Close()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != strings.TrimPrefix(targetServer.URL, "http://") {
			t.Fatalf("proxy received host %q, want %q", r.URL.Host, strings.TrimPrefix(targetServer.URL, "http://"))
		}

		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetServer.URL, r.Body)
		if err != nil {
			t.Fatalf("build upstream request: %v", err)
		}
		outReq.Header = r.Header.Clone()

		resp, err := http.DefaultTransport.RoundTrip(outReq)
		if err != nil {
			t.Fatalf("proxy round trip: %v", err)
		}
		defer resp.Body.Close()

		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	t.Setenv("HTTP_PROXY", proxyServer.URL)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("REQUEST_METHOD", "")

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	resp, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode: SendRequestProxyModeSystem,
		},
		http.MethodGet,
		targetServer.URL,
		nil,
		SendRequestBody{
			BodyType: SendRequestBodyTypeNone,
		},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest returned error: %v", err)
	}
	if resp.Body != "system-proxied" {
		t.Fatalf("response body = %q, want system-proxied", resp.Body)
	}
}

func newTestProxyService(t *testing.T, proxyCfg *settingservice.ProxyConfig) *ProxyService {
	t.Helper()

	settingsSvc := &settingservice.SettingService{}
	if proxyCfg == nil {
		proxyCfg = &settingservice.ProxyConfig{}
	}
	err := settingsSvc.Update(&settingservice.Settings{
		ProxyConfig: proxyCfg,
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ProxyService{
		baseCtx:        ctx,
		baseCancel:     cancel,
		settingService: settingsSvc,
		trafficEntries: &TrafficEntryWithStatics{
			OrderedMap: orderedmap.NewWithCapacity[uint64, *TrafficEntry](128),
			Statistics: &TrafficStatistics{},
		},
		currentHistoryMetadata: HistoryMetadata{
			Key:       generateHistoryKey(),
			Alias:     "",
			CreatedAt: time.Now().UnixMilli(),
		},
	}
}

func writeTempFile(t *testing.T, pattern string, content []byte) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return file.Name()
}

func decodeTestBodyWithContentEncoding(t *testing.T, body []byte, contentEncoding string) []byte {
	t.Helper()

	reader, err := getDecodedReader(bytes.NewReader(body), contentEncoding)
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
	return decoded
}

func assertMultipartFile(t *testing.T, r *http.Request, fieldName, expectedFilename string, expectedContent []byte) {
	t.Helper()

	file, header, err := r.FormFile(fieldName)
	if err != nil {
		t.Fatalf("FormFile(%q) failed: %v", fieldName, err)
	}
	defer file.Close()

	if header.Filename != expectedFilename {
		t.Fatalf("filename for %s = %q, want %q", fieldName, header.Filename, expectedFilename)
	}

	bodyBytes, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read multipart file %q: %v", fieldName, err)
	}
	if !bytes.Equal(bodyBytes, expectedContent) {
		t.Fatalf("multipart file %q content mismatch", fieldName)
	}
}
