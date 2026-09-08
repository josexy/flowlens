package proxyservice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeSingleHAR(t *testing.T, input HARExportEntry) (harEntry, []byte) {
	t.Helper()
	var output bytes.Buffer
	if _, err := WriteHAR(&output, "test", []HARExportEntry{input}); err != nil {
		t.Fatal(err)
	}
	var document harEnvelope
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Log.Entries) != 1 {
		t.Fatalf("entries = %d", len(document.Log.Entries))
	}
	return document.Log.Entries[0], output.Bytes()
}

func TestHARTimingPhases(t *testing.T) {
	for _, test := range []struct {
		name                string
		request, response   *HTTPMessageMetrics
		send, wait, receive float64
		total               float64
	}{
		{
			name:     "microsecond precision",
			request:  &HTTPMessageMetrics{StartedAtMicros: 1_000_000, EndedAtMicros: 1_000_123, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: 1_000_456, EndedAtMicros: 1_000_999, State: HTTPMessageStateCompleted},
			send:     .123, wait: .333, receive: .543, total: .999,
		},
		{
			name:     "observed zero duration",
			request:  &HTTPMessageMetrics{StartedAtMicros: 0, EndedAtMicros: 0, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: 0, EndedAtMicros: 0, State: HTTPMessageStateCompleted},
		},
		{
			name:     "pending response",
			request:  &HTTPMessageMetrics{StartedAtMicros: 1000, EndedAtMicros: 2000, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: 3000, EndedAtMicros: -1, State: HTTPMessageStatePending},
			send:     1, wait: 1, receive: -1, total: -1,
		},
		{
			name:     "failed response has terminal timestamp",
			request:  &HTTPMessageMetrics{StartedAtMicros: 1000, EndedAtMicros: 2000, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: 3000, EndedAtMicros: 4000, State: HTTPMessageStateFailed},
			send:     1, wait: 1, receive: -1, total: -1,
		},
		{
			name:     "canceled upload with early response",
			request:  &HTTPMessageMetrics{StartedAtMicros: 1000, EndedAtMicros: 4000, State: HTTPMessageStateCanceled},
			response: &HTTPMessageMetrics{StartedAtMicros: 2000, EndedAtMicros: 3000, State: HTTPMessageStateCompleted},
			send:     -1, wait: -1, receive: 1, total: -1,
		},
		{
			name:     "overlapping completed upload and response",
			request:  &HTTPMessageMetrics{StartedAtMicros: 1000, EndedAtMicros: 3000, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: 2000, EndedAtMicros: 5000, State: HTTPMessageStateCompleted},
			send:     2, wait: -1, receive: 3, total: -1,
		},
		{
			name: "missing metrics", send: -1, wait: -1, receive: -1, total: -1,
		},
		{
			name:     "completed flag cannot replace missing timestamps",
			request:  &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, State: HTTPMessageStateCompleted},
			response: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, State: HTTPMessageStateCompleted},
			send:     -1, wait: -1, receive: -1, total: -1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := decodeSingleHAR(t, HARExportEntry{Entry: &TrafficEntry{
				Type: "http", Method: "GET", URL: "http://example.test/",
				Request: &HTTPMessage{Metrics: test.request}, Response: &HTTPMessage{Metrics: test.response},
			}})
			if got.Timings.Send != test.send || got.Timings.Wait != test.wait || got.Timings.Receive != test.receive || got.Time != test.total {
				t.Fatalf("timings = %+v, time = %g", got.Timings, got.Time)
			}
			if got.Time >= 0 && math.Abs(got.Time-(got.Timings.Send+got.Timings.Wait+got.Timings.Receive)) > 1e-9 {
				t.Fatal("total differs from timing sum")
			}
			if got.Timings.Send >= 0 && !strings.Contains(got.Timings.Comment, "connection acquisition and retry") {
				t.Fatal("combined send phase is not documented")
			}
		})
	}
}

func TestHARUsesCapturedHeaderSize(t *testing.T) {
	for _, protocol := range []string{"HTTP/1.1", "HTTP/2.0"} {
		t.Run(protocol, func(t *testing.T) {
			entry := &TrafficEntry{Type: "http", Method: "GET", URL: "http://example.test/",
				Request: &HTTPMessage{Proto: protocol, HeaderFields: []HTTPHeaderField{{Name: "x-Dup", Value: "1"}, {Name: "X-Dup", Value: ""}},
					Metrics: &HTTPMessageMetrics{HeaderSize: 123}},
				Response: &HTTPMessage{Proto: protocol, Metrics: &HTTPMessageMetrics{HeaderSize: 456}},
			}
			got, _ := decodeSingleHAR(t, HARExportEntry{Entry: entry})
			if got.Request.HeadersSize != 123 || got.Response.HeadersSize != 456 {
				t.Fatalf("captured header sizes were changed: %d / %d", got.Request.HeadersSize, got.Response.HeadersSize)
			}
			if !reflect.DeepEqual(got.Request.Headers, []harNameValue{{Name: "x-Dup", Value: "1"}, {Name: "X-Dup", Value: ""}}) {
				t.Fatalf("headers changed: %+v", got.Request.Headers)
			}
		})
	}
	for _, test := range []struct {
		name    string
		message *HTTPMessage
	}{
		{"missing message", nil},
		{"missing metrics", &HTTPMessage{HeaderFields: []HTTPHeaderField{{Name: "Host", Value: "example.test"}}}},
		{"unknown metric", &HTTPMessage{Metrics: &HTTPMessageMetrics{HeaderSize: -1}}},
		{"truncated", &HTTPMessage{HeadersTruncated: true, Metrics: &HTTPMessageMetrics{HeaderSize: 123}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := decodeSingleHAR(t, HARExportEntry{Entry: &TrafficEntry{Type: "http", Request: test.message, Response: test.message}})
			if got.Request.HeadersSize != -1 || got.Response.HeadersSize != -1 {
				t.Fatalf("unknown sizes = %d / %d", got.Request.HeadersSize, got.Response.HeadersSize)
			}
		})
	}
}

func TestHARFormParameters(t *testing.T) {
	filename := "empty.txt"
	binaryFilename := "blob.bin"
	for _, test := range []struct {
		name, contentType, body string
		want                    []harPostParam
	}{
		{
			name: "url encoded order duplicates empty and unicode", contentType: "application/x-www-form-urlencoded; charset=utf-8",
			body: "a=1&a=&flag&=value&space=x+y&path=%2F&word=%E4%B8%AD",
			want: []harPostParam{{Name: "a", Value: "1"}, {Name: "a"}, {Name: "flag"}, {Value: "value"}, {Name: "space", Value: "x y"}, {Name: "path", Value: "/"}, {Name: "word", Value: "中"}},
		},
		{
			name: "multipart fields and files", contentType: "multipart/form-data; boundary=sample",
			body: "--sample\r\nContent-Disposition: form-data; name=tag\r\n\r\none\r\n" +
				"--sample\r\nContent-Disposition: form-data; name=tag\r\n\r\n\r\n" +
				"--sample\r\nContent-Disposition: form-data; name=empty; filename=empty.txt\r\nContent-Type: text/plain\r\n\r\n\r\n" +
				"--sample\r\nContent-Disposition: form-data; name=upload; filename=blob.bin\r\nContent-Type: application/octet-stream\r\n\r\n\xff\x00\r\n--sample--\r\n",
			want: []harPostParam{{Name: "tag", Value: "one"}, {Name: "tag"}, {Name: "empty", FileName: &filename, ContentType: "text/plain"}, {Name: "upload", Value: "/wA=", FileName: &binaryFilename, ContentType: "application/octet-stream", Encoding: "base64"}},
		},
	} {
		for _, streamed := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/bytes", true: "/reader"}[streamed], func(t *testing.T) {
				body := HARBody{Data: []byte(test.body), Available: true}
				var reader *harFormReadCloser
				if streamed {
					reader = &harFormReadCloser{Reader: strings.NewReader(test.body)}
					body = HARBody{Reader: reader, Size: int64(len(test.body)), Available: true}
				}
				got, raw := decodeSingleHAR(t, harFormInput(test.contentType, body))
				if got.Request.PostData == nil || !reflect.DeepEqual(got.Request.PostData.Params, test.want) {
					t.Fatalf("postData = %+v", got.Request.PostData)
				}
				var document map[string]any
				if err := json.Unmarshal(raw, &document); err != nil {
					t.Fatal(err)
				}
				postData := document["log"].(map[string]any)["entries"].([]any)[0].(map[string]any)["request"].(map[string]any)["postData"].(map[string]any)
				if _, hasText := postData["text"]; hasText {
					t.Fatal("params and text must be mutually exclusive")
				}
				if reader != nil && !reader.closed {
					t.Fatal("form reader not closed")
				}
			})
		}
	}
}

func harFormInput(contentType string, body HARBody) HARExportEntry {
	return HARExportEntry{Entry: &TrafficEntry{
		Type: "http", Method: "POST", URL: "http://example.test/form",
		Request: &HTTPMessage{
			Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: contentType}},
			Metrics: &HTTPMessageMetrics{State: HTTPMessageStateCompleted, BodySize: body.size()},
		},
	}, RequestBody: body}
}

func TestHARFormFallbackPreservesReaderBody(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		pending, smallSizeHint  bool
	}{
		{name: "bad escape", contentType: "application/x-www-form-urlencoded", body: "good=1&bad=%zz"},
		{name: "non UTF8 value", contentType: "application/x-www-form-urlencoded", body: "value=%ff"},
		{name: "missing boundary", contentType: "multipart/form-data", body: "raw body"},
		{name: "truncated multipart", contentType: "multipart/form-data; boundary=a", body: "--a\r\nContent-Disposition: form-data; name=x\r\n\r\nvalue"},
		{name: "unrepresented part metadata", contentType: "multipart/form-data; boundary=a", body: "--a\r\nContent-Disposition: form-data; name=x\r\nX-Part-ID: 1\r\n\r\nvalue\r\n--a--\r\n"},
		{name: "pending form", contentType: "application/x-www-form-urlencoded", body: "a=partial", pending: true},
		{name: "too many fields", contentType: "application/x-www-form-urlencoded", body: strings.Repeat("a=&", harFormMaxParams+1)},
		{name: "large known size", contentType: "application/x-www-form-urlencoded", body: "a=" + strings.Repeat("x", harFormMaxBytes)},
		{name: "large underreported size", contentType: "application/x-www-form-urlencoded", body: "a=" + strings.Repeat("x", harFormMaxBytes), smallSizeHint: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &harFormReadCloser{Reader: strings.NewReader(test.body)}
			body := HARBody{Reader: reader, Size: int64(len(test.body)), Available: true}
			if test.smallSizeHint {
				body.Size = 1
			}
			input := harFormInput(test.contentType, body)
			if test.pending {
				input.Entry.Request.Metrics.State = HTTPMessageStatePending
			}
			got, _ := decodeSingleHAR(t, input)
			postData := got.Request.PostData
			if postData == nil || postData.Params != nil {
				t.Fatalf("postData = %+v", postData)
			}
			text := postData.Text
			if postData.Encoding == "base64" {
				decoded, err := base64.StdEncoding.DecodeString(text)
				if err != nil {
					t.Fatal(err)
				}
				text = string(decoded)
			}
			if text != test.body {
				t.Fatal("fallback changed the original payload")
			}
			if !reader.closed {
				t.Fatal("fallback reader not closed")
			}
		})
	}
}

type failingHARFormReader struct{}

func (failingHARFormReader) Read([]byte) (int, error) { return 0, errors.New("form read failed") }

type harFormReadCloser struct {
	io.Reader
	closed bool
}

func (r *harFormReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestHARFormReadFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "export.har")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	reader := &harFormReadCloser{Reader: io.MultiReader(strings.NewReader("a="), failingHARFormReader{})}
	_, err := WriteHARFile(target, "test", []HARExportEntry{harFormInput("application/x-www-form-urlencoded", HARBody{Reader: reader, Size: 10, Available: true})})
	if err == nil || !strings.Contains(err.Error(), "form read failed") {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("target = %q, err = %v", data, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || !reader.closed {
		t.Fatalf("files = %v, err = %v, closed = %t", files, err, reader.closed)
	}
}
