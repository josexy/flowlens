package proxyservice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func TestFillRequestHTTPMessagePreservesHTTP1HeaderLines(t *testing.T) {
	const wire = "GET /socket HTTP/1.1\r\n" +
		"hOsT: example.test\r\n" +
		"A: first\r\n" +
		"B: middle\r\n" +
		"a: last\r\n" +
		"Upgrade: websocket\r\n\r\n"
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(wire)))
	if err != nil {
		t.Fatal(err)
	}

	entry := &TrafficEntry{Type: "ws"}
	new(ProxyService).fillRequestHTTPMessage(req, entry)
	want := []HTTPHeaderField{
		{Name: "hOsT", Value: "example.test"},
		{Name: "A", Value: "first"},
		{Name: "B", Value: "middle"},
		{Name: "a", Value: "last"},
		{Name: "Upgrade", Value: "websocket"},
	}
	if !reflect.DeepEqual(entry.Request.HeaderFields, want) {
		t.Fatalf("header fields:\n got %#v\nwant %#v", entry.Request.HeaderFields, want)
	}
	if entry.Request.HeadersTruncated {
		t.Fatal("HTTP/1 request headers unexpectedly marked truncated")
	}
	if entry.Request.HeaderOrderUnavailable {
		t.Fatal("HTTP/1 request header order unexpectedly marked unavailable")
	}
}

func TestFillResponseHTTPMessageUsesFinalBlockAndIgnoresInformational(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.WriteResponseHeaderBlock(w, http.HeaderBlock{
			Kind:       http.HeaderBlockInformational,
			ProtoMajor: 1,
			StatusCode: 103,
			Fields:     []http.HeaderField{{Name: "EaRlY", Value: "ignore"}},
		}); err != nil {
			t.Errorf("write informational response: %v", err)
			return
		}
		if err := http.WriteResponseHeaderBlock(w, http.HeaderBlock{
			Kind:       http.HeaderBlockInitial,
			ProtoMajor: 1,
			StatusCode: 200,
			Fields: []http.HeaderField{
				{Name: "X-B", Value: "one"},
				{Name: "Content-Length", Value: "0"},
				{Name: "x-A", Value: "two"},
			},
		}); err != nil {
			t.Errorf("write final response: %v", err)
		}
	}))
	defer server.Close()

	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	entry := &TrafficEntry{}
	new(ProxyService).fillResponseHTTPMessage(response, entry)
	want := []HTTPHeaderField{
		{Name: "X-B", Value: "one"},
		{Name: "Content-Length", Value: "0"},
		{Name: "x-A", Value: "two"},
	}
	if !reflect.DeepEqual(entry.Response.HeaderFields, want) {
		t.Fatalf("final response fields:\n got %#v\nwant %#v", entry.Response.HeaderFields, want)
	}
}

func TestFillResponseHTTPMessagePreservesHTTP2PseudoHeaders(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.WriteResponseHeaderBlock(w, http.HeaderBlock{
			Kind: http.HeaderBlockInitial,
			Fields: []http.HeaderField{
				{Name: ":status", Value: "204"},
				{Name: "x-b", Value: "one"},
				{Name: "x-a", Value: "two"},
			},
		}); err != nil {
			t.Errorf("write HTTP/2 response: %v", err)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	entry := &TrafficEntry{}
	new(ProxyService).fillResponseHTTPMessage(response, entry)
	want := []HTTPHeaderField{
		{Name: ":status", Value: "204"},
		{Name: "x-b", Value: "one"},
		{Name: "x-a", Value: "two"},
	}
	if !reflect.DeepEqual(entry.Response.HeaderFields, want) {
		t.Fatalf("HTTP/2 response fields:\n got %#v\nwant %#v", entry.Response.HeaderFields, want)
	}
}

func TestFillResponseTrailersPreservesHTTP2WireBlock(t *testing.T) {
	errCh := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok")); err != nil {
			errCh <- err
			return
		}
		errCh <- http.SetResponseTrailerBlock(w, http.HeaderBlock{
			Kind: http.HeaderBlockTrailer,
			Fields: []http.HeaderField{
				{Name: "grpc-status", Value: "0"},
				{Name: "x-order", Value: "first"},
				{Name: "grpc-message", Value: ""},
				{Name: "x-order", Value: "second"},
			},
		})
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	entry := &TrafficEntry{Response: &HTTPMessage{Proto: response.Proto}}
	if !newTestProxyService(t, &settingservice.ProxyConfig{}).fillResponseTrailers(response, entry) {
		t.Fatal("fillResponseTrailers returned false")
	}
	want := []HTTPHeaderField{
		{Name: "grpc-status", Value: "0"},
		{Name: "x-order", Value: "first"},
		{Name: "grpc-message", Value: ""},
		{Name: "x-order", Value: "second"},
	}
	if !reflect.DeepEqual(entry.Response.TrailerFields, want) {
		t.Fatalf("HTTP/2 trailer fields:\n got %#v\nwant %#v", entry.Response.TrailerFields, want)
	}
	if entry.Response.TrailersTruncated {
		t.Fatal("HTTP/2 trailers unexpectedly marked truncated")
	}
	if entry.Response.TrailerOrderUnavailable {
		t.Fatal("HTTP/2 trailer order unexpectedly marked unavailable")
	}
}

func TestFillResponseTrailersPreservesHTTP1WireBlock(t *testing.T) {
	errCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.WriteResponseHeaderBlock(w, http.HeaderBlock{
			Kind:       http.HeaderBlockInitial,
			ProtoMajor: 1,
			StatusCode: http.StatusOK,
			Fields: []http.HeaderField{
				{Name: "Transfer-Encoding", Value: "chunked"},
				{Name: "Trailer", Value: "x-First, X-Empty, x-Last"},
			},
		}); err != nil {
			errCh <- err
			return
		}
		if _, err := w.Write([]byte("ok")); err != nil {
			errCh <- err
			return
		}
		errCh <- http.SetResponseTrailerBlock(w, http.HeaderBlock{
			Kind:       http.HeaderBlockTrailer,
			ProtoMajor: 1,
			Fields: []http.HeaderField{
				{Name: "x-First", Value: "one"},
				{Name: "X-Empty", Value: ""},
				{Name: "x-Last", Value: "two"},
			},
		})
	}))
	defer server.Close()

	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	entry := &TrafficEntry{Response: &HTTPMessage{Proto: response.Proto}}
	if !newTestProxyService(t, &settingservice.ProxyConfig{}).fillResponseTrailers(response, entry) {
		t.Fatal("fillResponseTrailers returned false")
	}
	want := []HTTPHeaderField{
		{Name: "x-First", Value: "one"},
		{Name: "X-Empty", Value: ""},
		{Name: "x-Last", Value: "two"},
	}
	if !reflect.DeepEqual(entry.Response.TrailerFields, want) {
		t.Fatalf("HTTP/1 trailer fields:\n got %#v\nwant %#v", entry.Response.TrailerFields, want)
	}
	if entry.Response.TrailersTruncated || entry.Response.TrailerOrderUnavailable {
		t.Fatalf("HTTP/1 trailer state = truncated:%v unavailable:%v", entry.Response.TrailersTruncated, entry.Response.TrailerOrderUnavailable)
	}
}

func TestFillRequestHTTPMessagePreservesHTTP2PseudoHeaders(t *testing.T) {
	captured := make(chan *HTTPMessage, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry := &TrafficEntry{}
		new(ProxyService).fillRequestHTTPMessage(r, entry)
		captured <- entry.Request
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/path?q=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request, err = http.WithRequestHeaderBlocks(request, http.HeaderBlock{
		Kind: http.HeaderBlockInitial,
		Fields: []http.HeaderField{
			{Name: ":method", Value: "GET"},
			{Name: ":authority", Value: strings.TrimPrefix(server.URL, "https://")},
			{Name: ":scheme", Value: "https"},
			{Name: ":path", Value: "/path?q=1"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	message := <-captured
	if message.Proto != "HTTP/2.0" {
		t.Fatalf("request protocol = %q", message.Proto)
	}
	if len(message.HeaderFields) < 4 {
		t.Fatalf("HTTP/2 request fields = %#v", message.HeaderFields)
	}
	gotNames := []string{
		message.HeaderFields[0].Name,
		message.HeaderFields[1].Name,
		message.HeaderFields[2].Name,
		message.HeaderFields[3].Name,
	}
	wantNames := []string{":method", ":authority", ":scheme", ":path"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("HTTP/2 pseudo-header names = %#v, want %#v", gotNames, wantNames)
	}
}

func TestInitialHeaderFieldsPreservesEmptyPresenceAndTruncation(t *testing.T) {
	fields, truncated := initialHeaderFields([]http.HeaderBlock{{
		Kind:      http.HeaderBlockInitial,
		Truncated: true,
	}})
	if fields == nil || len(fields) != 0 {
		t.Fatalf("empty captured HeaderBlock = %#v, want non-nil empty", fields)
	}
	if !truncated {
		t.Fatal("truncated HeaderBlock flag was lost")
	}
}

func TestCompleteInitialHeaderFieldsMergesTruncatedFallback(t *testing.T) {
	fields, truncated, unavailable := completeInitialHeaderFields(
		[]http.HeaderBlock{{
			Kind:      http.HeaderBlockInitial,
			Truncated: true,
			Fields: []http.HeaderField{
				{Name: "X-First", Value: "one"},
			},
		}},
		[]HTTPHeaderField{
			{Name: "X-First", Value: "one"},
			{Name: "X-Last", Value: "two"},
		},
	)
	want := []HTTPHeaderField{
		{Name: "X-First", Value: "one"},
		{Name: "X-Last", Value: "two"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("completed fields:\n got %#v\nwant %#v", fields, want)
	}
	if !truncated {
		t.Fatal("completed fields lost truncation state")
	}
	if unavailable {
		t.Fatal("partially captured wire order marked entirely unavailable")
	}
}

func TestCompleteInitialHeaderFieldsUsesDeterministicFallbackWhenMissing(t *testing.T) {
	fallback := headerFieldsFromMap(map[string][]string{
		"X-Z": {"last"},
		"x-a": {"first", "second"},
	})
	want := []HTTPHeaderField{
		{Name: "x-a", Value: "first"},
		{Name: "x-a", Value: "second"},
		{Name: "X-Z", Value: "last"},
	}
	if !reflect.DeepEqual(fallback, want) {
		t.Fatalf("deterministic fallback:\n got %#v\nwant %#v", fallback, want)
	}

	fields, truncated, unavailable := completeInitialHeaderFields(nil, fallback)
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("completed fallback:\n got %#v\nwant %#v", fields, want)
	}
	if truncated {
		t.Fatal("missing wire block incorrectly marked truncated")
	}
	if !unavailable {
		t.Fatal("missing wire block did not mark order unavailable")
	}
}

func TestCloneTrafficHTTPMessageDeepCopiesHeaderFields(t *testing.T) {
	source := &HTTPMessage{
		HeaderFields: []HTTPHeaderField{{Name: "A", Value: "one"}},
	}
	clone := cloneTrafficHTTPMessage(source)
	clone.HeaderFields[0].Name = "changed"
	if source.HeaderFields[0].Name != "A" {
		t.Fatalf("clone shares header state with source: source=%#v clone=%#v", source, clone)
	}

	empty := cloneTrafficHTTPMessage(&HTTPMessage{HeaderFields: []HTTPHeaderField{}})
	if empty.HeaderFields == nil {
		t.Fatal("deep clone lost non-nil empty HeaderFields presence")
	}
}

// TestRawPanelMatchesLogicalHeaderSizeFixtures in internal/desktop/logic loads
// these same fixtures and compares the complete Raw panel text and displayed
// totals, so protocol formatting cannot silently drift.
func TestLogicalHeaderSizesMatchRawDisplayAndV1HAR(t *testing.T) {
	data, err := os.ReadFile("testdata/logical_header_sizes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name         string
		Entry        TrafficEntry
		RequestHead  string
		ResponseHead string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			entry := &fixture.Entry
			before, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			requestSize := logicalHTTPRequestHeaderSize(entry)
			responseSize := logicalHTTPResponseHeaderSize(entry)
			if requestSize != int64(len(fixture.RequestHead)) || responseSize != int64(len(fixture.ResponseHead)) {
				t.Fatalf("head sizes = %d / %d, want %d / %d", requestSize, responseSize, len(fixture.RequestHead), len(fixture.ResponseHead))
			}
			after, err := json.Marshal(entry)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("header calculation mutated the captured entry")
			}
			entry.Request.Metrics = newPendingHTTPMessageMetrics(requestSize)
			entry.Response.Metrics = newPendingHTTPMessageMetrics(responseSize)
			var encoded bytes.Buffer
			if err := hbinWriteEntryHeader(&encoded, entry); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeTrafficEntryWithVersion(&encoded, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(entry.Request.Metrics, decoded.Request.Metrics) ||
				!reflect.DeepEqual(entry.Response.Metrics, decoded.Response.Metrics) {
				t.Fatal("HBIN v1 changed the captured sizes")
			}
			got, raw := decodeSingleHAR(t, HARExportEntry{Entry: decoded})
			if got.Request.HeadersSize != requestSize || got.Response.HeadersSize != responseSize {
				t.Fatalf("HAR sizes = %d / %d", got.Request.HeadersSize, got.Response.HeadersSize)
			}
			if bytes.Contains(raw, []byte("_logicalHeadersSize")) {
				t.Fatal("HAR contains the removed duplicate size field")
			}
		})
	}
}

func TestLogicalHeaderSizesRequireCompleteHead(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*TrafficEntry)
	}{
		{"missing message", func(e *TrafficEntry) { e.Request = nil; e.Response = nil }},
		{"missing fields", func(e *TrafficEntry) { e.Request.HeaderFields = nil; e.Response.HeaderFields = nil }},
		{"truncated", func(e *TrafficEntry) { e.Request.HeadersTruncated = true; e.Response.HeadersTruncated = true }},
		{"missing protocol", func(e *TrafficEntry) { e.Request.Proto = ""; e.Response.Proto = "" }},
		{"missing start line", func(e *TrafficEntry) { e.Method = ""; e.Status = ""; e.StatusCode = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := &TrafficEntry{Method: "GET", URL: "https://example.test/", Status: "200 OK", StatusCode: 200,
				Request:  &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{}},
				Response: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{}},
			}
			test.change(entry)
			if logicalHTTPRequestHeaderSize(entry) != -1 || logicalHTTPResponseHeaderSize(entry) != -1 {
				t.Fatal("incomplete head must have unknown size")
			}
		})
	}
}

func TestLogicalHeaderSizeMatchesJSONUTF8Replacement(t *testing.T) {
	entry := &TrafficEntry{Method: "GET", URL: "https://example.test/",
		Request: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "X-Text", Value: "\xff\xff"}}},
	}
	if got, want := logicalHTTPRequestHeaderSize(entry), int64(len("GET / HTTP/1.1\r\nX-Text: \ufffd\ufffd\r\n\r\n")); got != want {
		t.Fatalf("head size = %d, want %d", got, want)
	}
}
