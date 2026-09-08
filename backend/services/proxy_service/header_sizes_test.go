package proxyservice

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The frontend loads these same fixtures and compares the complete Raw panel
// text and displayed totals, so protocol formatting cannot silently drift.
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
