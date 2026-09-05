package proxyservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

func emitHTTPExchangeTiming(
	exchange *captureExchange,
	phase mitmproxy.HTTPExchangeTimingPhase,
	timestamp time.Time,
	attempt int,
	complete bool,
	err error,
) {
	exchange.observeHTTPExchangeTiming(mitmproxy.HTTPExchangeTimingEvent{
		Phase:     phase,
		Timestamp: timestamp,
		Attempt:   attempt,
		Complete:  complete,
		Error:     err,
	})
}

func startCaptureAttempt(exchange *captureExchange, started time.Time, attempt int, bodyless bool) {
	exchange.setRequestBodyless(bodyless)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestStarted, started, attempt, false, nil)
}

func TestTransferCountingBodyCompletesAtEOF(t *testing.T) {
	var size int64
	var complete bool
	var readErr error
	body := observeTransferBody(
		io.NopCloser(bytes.NewReader([]byte("payload"))),
		7,
		func(gotSize int64, gotComplete bool, gotErr error) {
			size, complete, readErr = gotSize, gotComplete, gotErr
		},
	)

	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatalf("drain body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if size != 7 || !complete || readErr != nil {
		t.Fatalf("completion = size %d complete %t err %v", size, complete, readErr)
	}
}

func TestTransferCountingBodyCloseBeforeEOFIsCanceled(t *testing.T) {
	var size int64
	var complete bool
	body := observeTransferBody(
		io.NopCloser(bytes.NewReader([]byte("payload"))),
		7,
		func(gotSize int64, gotComplete bool, _ error) {
			size, complete = gotSize, gotComplete
		},
	)

	buffer := make([]byte, 3)
	if _, err := io.ReadFull(body, buffer); err != nil {
		t.Fatalf("read partial body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if size != 3 || complete {
		t.Fatalf("completion = size %d complete %t", size, complete)
	}
}

func TestResponseHasNoEntityBody(t *testing.T) {
	wrappedBody := io.NopCloser(bytes.NewReader(nil))
	for _, test := range []struct {
		name   string
		method string
		status int
		body   io.ReadCloser
		want   bool
	}{
		{name: "head", method: http.MethodHead, status: http.StatusOK, body: wrappedBody, want: true},
		{name: "informational", method: http.MethodGet, status: http.StatusEarlyHints, body: wrappedBody, want: true},
		{name: "no content", method: http.MethodGet, status: http.StatusNoContent, body: wrappedBody, want: true},
		{name: "not modified", method: http.MethodGet, status: http.StatusNotModified, body: wrappedBody, want: true},
		{name: "no body sentinel", method: http.MethodGet, status: http.StatusOK, body: http.NoBody, want: true},
		{name: "ordinary response", method: http.MethodGet, status: http.StatusOK, body: wrappedBody, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := responseHasNoEntityBody(test.method, &http.Response{StatusCode: test.status, Body: test.body})
			if got != test.want {
				t.Fatalf("responseHasNoEntityBody = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCaptureExchangePublishesCompletedResponseSnapshot(t *testing.T) {
	svc := newTestProxyService(t, nil)
	now := time.Now()
	entry := svc.newTrafficEntry(TrafficEntry{
		Type:   "https",
		Method: http.MethodGet,
		URL:    "https://example.test/",
		Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{
			{Name: ":method", Value: "GET"},
			{Name: ":path", Value: "/"},
		}},
	})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	startCaptureAttempt(exchange, now, 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, now.Add(500*time.Microsecond), 1, true, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, now.Add(time.Millisecond), 1, false, nil)
	exchange.responseHeaders(&http.Response{
		StatusCode:    200,
		Status:        "200 OK",
		Proto:         "HTTP/2.0",
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Body:          io.NopCloser(bytes.NewReader(nil)),
		ContentLength: -1,
	})
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseEnded, now.Add(2*time.Millisecond), 1, true, nil)
	exchange.responseBodyFinished(7, true, nil)

	stored, ok := svc.trafficEntries.Get(entry.ID)
	if !ok {
		t.Fatal("completed entry not stored")
	}
	if stored == entry {
		t.Fatal("stored entry must be an immutable clone")
	}
	if stored.Request == nil || stored.Request.Metrics == nil || stored.Request.Metrics.State != HTTPMessageStateCompleted {
		t.Fatalf("request metrics = %+v", stored.Request)
	}
	if stored.Response == nil || stored.Response.Metrics == nil {
		t.Fatalf("response metrics = %+v", stored.Response)
	}
	if stored.Response.Metrics.State != HTTPMessageStateCompleted || stored.Response.Metrics.BodySize != 7 {
		t.Fatalf("response metrics = %+v", stored.Response.Metrics)
	}
	if stored.Response.Metrics.StartedAtMicros < stored.Request.Metrics.EndedAtMicros ||
		stored.Response.Metrics.EndedAtMicros < stored.Response.Metrics.StartedAtMicros {
		t.Fatalf("invalid timing order: request=%+v response=%+v", stored.Request.Metrics, stored.Response.Metrics)
	}
	if !svc.historyDirty.Load() {
		t.Fatal("terminal update should dirty history")
	}
}

func TestCaptureExchangeUsesInitialEntryAndTypedPatches(t *testing.T) {
	svc := newTestProxyService(t, nil)
	var fullEntries []*TrafficEntry
	var patches []TrafficEntryPatch
	svc.emitTrafficHook = func(entry *TrafficEntry) {
		fullEntries = append(fullEntries, entry)
	}
	svc.emitTrafficPatchHook = func(patch TrafficEntryPatch) {
		patches = append(patches, patch)
	}

	started := time.Now()
	entry := svc.newTrafficEntry(TrafficEntry{
		Type:    "https",
		Method:  http.MethodGet,
		Request: &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: "Accept", Value: "*/*"}}},
	})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	startCaptureAttempt(exchange, started, 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(time.Microsecond), 1, true, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, started.Add(2*time.Microsecond), 1, false, nil)
	exchange.responseHeaders(&http.Response{
		StatusCode: http.StatusNoContent,
		Status:     "204 No Content",
		Proto:      "HTTP/2.0",
		Body:       http.NoBody,
	})
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseEnded, started.Add(3*time.Microsecond), 1, true, nil)
	exchange.markBodylessResponseSize()

	if len(fullEntries) != 1 {
		t.Fatalf("full traffic events = %d, want only the initial request snapshot", len(fullEntries))
	}
	if len(patches) != 2 {
		t.Fatalf("traffic patches = %d, want response headers and terminal metrics", len(patches))
	}
	responsePatch := patches[0]
	if responsePatch.TrafficID != entry.ID || responsePatch.ResponseHeaders == nil || responsePatch.Metrics == nil {
		t.Fatalf("response-header patch = %+v", responsePatch)
	}
	if responsePatch.ResponseHeaders.StatusCode != http.StatusNoContent ||
		responsePatch.ResponseHeaders.Proto != "HTTP/2.0" {
		t.Fatalf("response headers = %+v", responsePatch.ResponseHeaders)
	}
	last := patches[len(patches)-1]
	if last.TrafficID != entry.ID || last.Metrics == nil ||
		last.Metrics.Request == nil || last.Metrics.Response == nil {
		t.Fatalf("last metrics patch = %+v", last)
	}
	if last.Metrics.Response.State != HTTPMessageStateCompleted || last.Metrics.Response.BodySize != 0 {
		t.Fatalf("last response metrics = %+v", last.Metrics.Response)
	}
	if fullEntries[0].Revision == 0 || responsePatch.Revision <= fullEntries[0].Revision ||
		last.Revision <= responsePatch.Revision {
		t.Fatalf("event revisions = full:%d response:%d metrics:%d", fullEntries[0].Revision, responsePatch.Revision, last.Revision)
	}
}

func TestMetricSnapshotReusesImmutableCollections(t *testing.T) {
	entry := &TrafficEntry{
		Metadata: &Metadata{
			TLS: &TLSState{SupportedALPN: []string{"h2", "http/1.1"}},
		},
		Request: &HTTPMessage{
			HeaderFields: []HTTPHeaderField{{Name: "Accept", Value: "*/*"}},
			Metrics:      &HTTPMessageMetrics{State: HTTPMessageStatePending},
		},
	}

	snapshot := cloneTrafficEntryForMetrics(entry)
	if &snapshot.Request.HeaderFields[0] != &entry.Request.HeaderFields[0] {
		t.Fatal("metric snapshot copied immutable header backing storage")
	}
	if snapshot.Request == entry.Request || snapshot.Request.Metrics == entry.Request.Metrics {
		t.Fatal("metric snapshot must copy mutable message and metrics values")
	}
	entry.Request.Metrics.BodySize = 99
	if snapshot.Request.Metrics.BodySize == 99 {
		t.Fatal("metric snapshot changed with the working entry")
	}
}

func TestTypedPatchSnapshotsCopyOnlyChangedCollections(t *testing.T) {
	entry := &TrafficEntry{
		Metadata: &Metadata{
			TLS:     &TLSState{SupportedALPN: []string{"h2", "http/1.1"}},
			Process: &ProcessInfo{Status: ProcessStatusPending},
		},
		Request: &HTTPMessage{
			HeaderFields: []HTTPHeaderField{{Name: "Accept", Value: "*/*"}},
			Metrics:      &HTTPMessageMetrics{State: HTTPMessageStateCompleted},
		},
		Response: &HTTPMessage{
			HeaderFields:  []HTTPHeaderField{{Name: "Content-Type", Value: "text/plain"}},
			TrailerFields: []HTTPHeaderField{{Name: "X-Trace", Value: "one"}},
			Metrics:       &HTTPMessageMetrics{State: HTTPMessageStateCompleted},
		},
	}

	headerSnapshot := cloneTrafficEntryForResponseHeaders(entry)
	if &headerSnapshot.Request.HeaderFields[0] != &entry.Request.HeaderFields[0] {
		t.Fatal("response-header snapshot copied immutable request headers")
	}
	if &headerSnapshot.Response.HeaderFields[0] == &entry.Response.HeaderFields[0] {
		t.Fatal("response-header snapshot shared the newly published response headers")
	}
	if headerSnapshot.Metadata == entry.Metadata || headerSnapshot.Metadata.TLS != entry.Metadata.TLS {
		t.Fatal("response-header snapshot did not reuse immutable TLS data safely")
	}
	entry.Response.HeaderFields[0].Value = "mutated"
	if headerSnapshot.Response.HeaderFields[0].Value != "text/plain" {
		t.Fatal("response-header snapshot changed with the working entry")
	}

	trailerSnapshot := cloneTrafficEntryForResponseTrailers(entry)
	if &trailerSnapshot.Response.HeaderFields[0] != &entry.Response.HeaderFields[0] {
		t.Fatal("response-trailer snapshot copied immutable response headers")
	}
	if &trailerSnapshot.Response.TrailerFields[0] == &entry.Response.TrailerFields[0] {
		t.Fatal("response-trailer snapshot shared the newly published trailers")
	}
	entry.Response.TrailerFields[0].Value = "mutated"
	if trailerSnapshot.Response.TrailerFields[0].Value != "one" {
		t.Fatal("response-trailer snapshot changed with the working entry")
	}

	processSnapshot := cloneTrafficEntryForProcess(headerSnapshot)
	if processSnapshot.Request != headerSnapshot.Request || processSnapshot.Response != headerSnapshot.Response {
		t.Fatal("process snapshot copied immutable HTTP messages")
	}
	if processSnapshot.Metadata == headerSnapshot.Metadata {
		t.Fatal("process snapshot shared its mutable metadata container")
	}
	if processSnapshot.Metadata.Process != headerSnapshot.Metadata.Process {
		t.Fatal("process snapshot copied the immutable prior process value")
	}
}

var benchmarkTrafficEntrySink *TrafficEntry
var benchmarkTrafficJSONSink []byte

func BenchmarkTrafficSnapshotClone(b *testing.B) {
	fields := make([]HTTPHeaderField, 128)
	for index := range fields {
		fields[index] = HTTPHeaderField{Name: "X-Benchmark", Value: "snapshot-value"}
	}
	entry := &TrafficEntry{
		Metadata: &Metadata{
			TLS: &TLSState{
				SupportedALPN:         []string{"h2", "http/1.1"},
				SupportedVersion:      []string{"TLS 1.3", "TLS 1.2"},
				SupportedCipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
			},
		},
		Request:  &HTTPMessage{HeaderFields: fields, Metrics: &HTTPMessageMetrics{State: HTTPMessageStatePending}},
		Response: &HTTPMessage{HeaderFields: fields, Metrics: &HTTPMessageMetrics{State: HTTPMessageStatePending}},
	}
	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkTrafficEntrySink = cloneTrafficEntryForAttribution(entry)
			if benchmarkTrafficEntrySink == nil {
				b.Fatal("nil snapshot")
			}
		}
	})
	b.Run("metrics", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkTrafficEntrySink = cloneTrafficEntryForMetrics(entry)
			if benchmarkTrafficEntrySink == nil {
				b.Fatal("nil snapshot")
			}
		}
	})
	b.Run("response_headers", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkTrafficEntrySink = cloneTrafficEntryForResponseHeaders(entry)
			if benchmarkTrafficEntrySink == nil {
				b.Fatal("nil snapshot")
			}
		}
	})
	b.Run("process", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkTrafficEntrySink = cloneTrafficEntryForProcess(entry)
			if benchmarkTrafficEntrySink == nil {
				b.Fatal("nil snapshot")
			}
		}
	})
}

func BenchmarkTrafficEventJSON(b *testing.B) {
	requestFields := make([]HTTPHeaderField, 128)
	responseFields := make([]HTTPHeaderField, 128)
	for index := range requestFields {
		requestFields[index] = HTTPHeaderField{Name: "X-Request", Value: "request-value"}
		responseFields[index] = HTTPHeaderField{Name: "X-Response", Value: "response-value"}
	}
	entry := &TrafficEntry{
		ID:         1,
		Revision:   4,
		Type:       "https",
		Method:     http.MethodGet,
		URL:        "https://example.test/",
		Host:       "example.test",
		Path:       "/",
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Metadata: &Metadata{
			TLS: &TLSState{SupportedALPN: []string{"h2", "http/1.1"}},
			Certificate: &ServerCertificate{
				DNSNames: []string{"example.test", "www.example.test"},
			},
			Process: &ProcessInfo{Status: ProcessStatusResolved, PID: 42},
		},
		Request: &HTTPMessage{
			Proto:        "HTTP/2.0",
			HeaderFields: requestFields,
			Metrics:      &HTTPMessageMetrics{State: HTTPMessageStateCompleted},
		},
		Response: &HTTPMessage{
			Proto:        "HTTP/2.0",
			HeaderFields: responseFields,
			Metrics:      &HTTPMessageMetrics{State: HTTPMessageStateCompleted},
		},
	}
	benchmarks := map[string]any{
		"full":             entry,
		"response_headers": newTrafficResponseHeadersPatch(entry),
		"metrics":          newTrafficMetricsPatch(entry),
		"process":          newTrafficProcessPatch(entry),
	}
	for name, payload := range benchmarks {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				encoded, err := json.Marshal(payload)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkTrafficJSONSink = encoded
			}
		})
	}
}

func TestCaptureExchangeConsumesMitmproxyHighResolutionTimestamps(t *testing.T) {
	svc := newTestProxyService(t, nil)
	base := time.Date(2026, time.August, 12, 20, 0, 0, 123_000_000, time.Local)
	timestamps := []time.Time{
		base,
		base.Add(125 * time.Microsecond),
		base.Add(275 * time.Millisecond),
		base.Add(275*time.Millisecond + 175*time.Microsecond),
	}
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	startCaptureAttempt(exchange, timestamps[0], 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, timestamps[1], 1, true, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, timestamps[2], 1, false, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseEnded, timestamps[3], 1, true, nil)
	exchange.responseHeaders(&http.Response{
		StatusCode: http.StatusNoContent,
		Status:     "204 No Content",
		Proto:      "HTTP/2.0",
		Body:       http.NoBody,
	})
	exchange.markBodylessResponseSize()
	if got, want := entry.Request.Metrics.StartedAtMicros, timestamps[0].UnixMicro(); got != want {
		t.Fatalf("request start = %d, want %d", got, want)
	}
	if got, want := entry.Request.Metrics.EndedAtMicros, timestamps[1].UnixMicro(); got != want {
		t.Fatalf("request end = %d, want %d", got, want)
	}
	if got, want := entry.Response.Metrics.StartedAtMicros, timestamps[2].UnixMicro(); got != want {
		t.Fatalf("response start = %d, want %d", got, want)
	}
	if got, want := entry.Response.Metrics.EndedAtMicros, timestamps[3].UnixMicro(); got != want {
		t.Fatalf("response end = %d, want %d", got, want)
	}
}

func TestCaptureExchangePreservesEarlyResponseStartForSameAttempt(t *testing.T) {
	svc := newTestProxyService(t, nil)
	started := time.Now()
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	startCaptureAttempt(exchange, started, 1, true)
	earlyResponse := started.Add(time.Millisecond)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, earlyResponse, 1, false, nil)
	exchange.responseHeaders(&http.Response{
		StatusCode: 413,
		Status:     "413 Content Too Large",
		Proto:      "HTTP/1.1",
		Header:     http.Header{"Content-Length": {"0"}},
		Body:       http.NoBody,
	})
	if entry.Request.Metrics.EndedAtMicros != -1 || entry.Request.Metrics.State != HTTPMessageStatePending {
		t.Fatalf("request completed before WroteRequest: %+v", entry.Request.Metrics)
	}
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(2*time.Millisecond), 1, true, nil)

	if entry.Response == nil || entry.Response.Metrics == nil {
		t.Fatalf("response metrics = %+v", entry.Response)
	}
	if got := entry.Response.Metrics.StartedAtMicros; got != earlyResponse.UnixMicro() {
		t.Fatalf("response start = %d, want %d", got, earlyResponse.UnixMicro())
	}
}

func TestCaptureExchangeRetryResetsFailedAttemptTiming(t *testing.T) {
	svc := newTestProxyService(t, nil)
	started := time.Now()
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	startCaptureAttempt(exchange, started, 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, started.Add(time.Millisecond), 1, false, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(1500*time.Microsecond), 1, false, errors.New("stale connection"))

	startCaptureAttempt(exchange, started.Add(2*time.Millisecond), 2, true)
	if entry.Request.Metrics.EndedAtMicros != -1 || entry.Request.Metrics.State != HTTPMessageStatePending {
		t.Fatalf("retried request metrics = %+v", entry.Request.Metrics)
	}
	if entry.Response.Metrics.StartedAtMicros != -1 || entry.Response.Metrics.State != HTTPMessageStatePending {
		t.Fatalf("retried response metrics = %+v", entry.Response.Metrics)
	}

	finalResponse := started.Add(2 * time.Millisecond)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, finalResponse, 2, false, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(3*time.Millisecond), 2, true, nil)
	if got := entry.Response.Metrics.StartedAtMicros; got != finalResponse.UnixMicro() {
		t.Fatalf("final response start = %d, want %d", got, finalResponse.UnixMicro())
	}
}

func TestCaptureExchangeRetryUsesFinalAttemptBodySize(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	started := time.Now()
	startCaptureAttempt(exchange, started, 1, false)

	request := &http.Request{
		ContentLength: 5,
		GetBody: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte("final"))), nil
		},
	}
	exchange.observeRetryRequestBodies(request)

	exchange.requestBodyFinished(3, true, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(time.Millisecond), 1, false, errors.New("stale connection"))
	startCaptureAttempt(exchange, started.Add(2*time.Millisecond), 2, false)

	body, err := request.GetBody()
	if err != nil {
		t.Fatalf("GetBody: %v", err)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatalf("read retry body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close retry body: %v", err)
	}
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(3*time.Millisecond), 2, true, nil)

	if got := entry.Request.Metrics.BodySize; got != 5 {
		t.Fatalf("final request body size = %d, want 5", got)
	}
	if entry.Request.Metrics.State != HTTPMessageStateCompleted {
		t.Fatalf("final request state = %s, want completed", entry.Request.Metrics.State)
	}
}

func TestStateForCaptureError(t *testing.T) {
	if got := stateForCaptureError(context.Background(), errors.New("broken")); got != HTTPMessageStateFailed {
		t.Fatalf("failed state = %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := stateForCaptureError(ctx, io.ErrClosedPipe); got != HTTPMessageStateCanceled {
		t.Fatalf("canceled state = %q", got)
	}
}

func TestCaptureExchangeCancelBeforeResponsePersistsCanceledState(t *testing.T) {
	svc := newTestProxyService(t, nil)
	var emittedPatch TrafficEntryPatch
	svc.emitTrafficPatchHook = func(patch TrafficEntryPatch) { emittedPatch = patch }
	ctx, cancel := context.WithCancel(context.Background())
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, ctx, entry)
	started := time.Now()
	startCaptureAttempt(exchange, started, 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(time.Microsecond), 1, true, nil)
	cancel()
	exchange.fail(context.Canceled)

	if entry.Response == nil || entry.Response.Metrics == nil {
		t.Fatalf("response metrics = %+v", entry.Response)
	}
	metrics := entry.Response.Metrics
	if metrics.State != HTTPMessageStateCanceled || metrics.StartedAtMicros != -1 ||
		metrics.EndedAtMicros != -1 || metrics.HeaderSize != -1 || metrics.BodySize != -1 {
		t.Fatalf("canceled response metrics = %+v", metrics)
	}
	if emittedPatch.Error == nil || emittedPatch.Metrics == nil ||
		emittedPatch.Metrics.Response == nil ||
		emittedPatch.Metrics.Response.State != HTTPMessageStateCanceled {
		t.Fatalf("canceled failure patch = %+v", emittedPatch)
	}
}

func TestCaptureExchangeFirstByteKeepsHeaderSizeUnknown(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	started := time.Now()
	startCaptureAttempt(exchange, started, 1, true)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, started.Add(time.Microsecond), 1, false, nil)
	if entry.Response == nil || entry.Response.Metrics == nil || entry.Response.Metrics.HeaderSize != -1 {
		t.Fatalf("first-byte response metrics = %+v", entry.Response)
	}
}

func TestMarkHistoryGenerationFlushedPreservesConcurrentUpdates(t *testing.T) {
	for range 1_000 {
		svc := &ProxyService{}
		svc.flushGeneration.Store(1)
		svc.historyDirty.Store(true)
		var wg sync.WaitGroup
		wg.Go(func() {
			svc.markHistoryGenerationFlushed(1)
		})
		svc.markHistoryDirty()
		wg.Wait()

		if current, flushed := svc.flushGeneration.Load(), svc.lastFlushGeneration.Load(); current > flushed && !svc.historyDirty.Load() {
			t.Fatalf("generation %d advanced past flushed %d with clean history", current, flushed)
		}
	}
}

func TestDeleteTrafficEntryMarksPersistedHistoryDirty(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "http"})
	if !svc.storeTrafficEntry(entry) {
		t.Fatal("storeTrafficEntry returned false")
	}
	svc.markHistoryGenerationFlushed(svc.flushGeneration.Load())
	if svc.historyDirty.Load() {
		t.Fatal("history should be clean after simulated flush")
	}

	svc.deleteTrafficEntry(entry.ID)
	if !svc.historyDirty.Load() || svc.flushGeneration.Load() <= svc.lastFlushGeneration.Load() {
		t.Fatalf(
			"delete did not dirty history: current=%d flushed=%d dirty=%t",
			svc.flushGeneration.Load(),
			svc.lastFlushGeneration.Load(),
			svc.historyDirty.Load(),
		)
	}
}

func TestDeletePendingTrafficEntryCannotBeRepublishedByLateCallbacks(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, context.Background(), entry)
	started := time.Now()
	startCaptureAttempt(exchange, started, 1, true)
	svc.deleteTrafficEntry(entry.ID)

	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeRequestEnded, started.Add(time.Microsecond), 1, true, nil)
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseStarted, started.Add(2*time.Microsecond), 1, false, nil)
	exchange.responseHeaders(&http.Response{
		StatusCode: 204,
		Status:     "204 No Content",
		Proto:      "HTTP/1.1",
		Body:       http.NoBody,
	})
	emitHTTPExchangeTiming(exchange, mitmproxy.HTTPExchangeResponseEnded, started.Add(3*time.Microsecond), 1, true, nil)
	exchange.markBodylessResponseSize()
	if _, ok := svc.trafficEntries.Get(entry.ID); ok {
		t.Fatal("late exchange callbacks republished a deleted entry")
	}
	if got := svc.GetStatistics().Total; got != 0 {
		t.Fatalf("traffic total = %d, want 0", got)
	}
}

func TestDeleteTrafficEntryBetweenStoreTombstoneChecksCannotRepublish(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "https", Request: &HTTPMessage{}})
	if !svc.storeTrafficEntry(entry) {
		t.Fatal("initial storeTrafficEntry returned false")
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("persist initial traffic: %v", err)
	}
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatal(err)
	}
	hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(svc.currentHistoryMetadata.Key))
	hidxPath := filepath.Join(historyDir, fs.GetHIdxFileName(svc.currentHistoryMetadata.Key))
	t.Cleanup(func() {
		_ = os.Remove(hbinPath)
		_ = os.Remove(hidxPath)
	})

	passedPrecheck := make(chan struct{})
	continueStore := make(chan struct{})
	svc.storeTrafficPrecheckHook = func() {
		close(passedPrecheck)
		<-continueStore
	}

	storeResult := make(chan bool, 1)
	go func() {
		entry.Request.Metrics = &HTTPMessageMetrics{State: HTTPMessageStateCompleted}
		storeResult <- svc.storeTrafficEntry(entry)
	}()
	<-passedPrecheck

	deleteDone := make(chan struct{})
	go func() {
		svc.deleteTrafficEntry(entry.ID)
		close(deleteDone)
	}()
	select {
	case <-deleteDone:
	case <-time.After(time.Second):
		t.Fatal("deleteTrafficEntry blocked before the late store acquired the attribution lock")
	}
	close(continueStore)

	select {
	case stored := <-storeResult:
		if stored {
			t.Fatal("storeTrafficEntry accepted an entry deleted between its tombstone checks")
		}
	case <-time.After(time.Second):
		t.Fatal("storeTrafficEntry did not return after deletion")
	}
	if _, ok := svc.trafficEntries.Get(entry.ID); ok {
		t.Fatal("late store republished the deleted entry")
	}
	if got := svc.GetStatistics().Total; got != 0 {
		t.Fatalf("traffic total = %d, want 0", got)
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flush deleted traffic: %v", err)
	}
	for _, path := range []string{hbinPath, hidxPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted traffic survived in persisted history at %s: %v", path, err)
		}
	}
}

func TestFlushAfterDeletingLastEntryRemovesPersistedHistory(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "http"})
	if !svc.storeTrafficEntry(entry) {
		t.Fatal("storeTrafficEntry returned false")
	}
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("initial flush: %v", err)
	}
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatal(err)
	}
	hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(svc.currentHistoryMetadata.Key))
	hidxPath := filepath.Join(historyDir, fs.GetHIdxFileName(svc.currentHistoryMetadata.Key))
	t.Cleanup(func() {
		_ = os.Remove(hbinPath)
		_ = os.Remove(hidxPath)
	})
	if _, err := os.Stat(hbinPath); err != nil {
		t.Fatalf("stat initial hbin: %v", err)
	}

	svc.deleteTrafficEntry(entry.ID)
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("empty flush: %v", err)
	}
	for _, path := range []string{hbinPath, hidxPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("persisted empty history still exists at %s: %v", path, err)
		}
	}
}
