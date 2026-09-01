package proxyservice

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/compresspool"
)

func TestWriteHARLogicalSizeProfileGolden(t *testing.T) {
	requestFields := []HTTPHeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":authority", Value: "ifconfig.co"},
		{Name: ":path", Value: "/json"},
		{Name: ":scheme", Value: "https"},
		{Name: "user-agent", Value: "curl/8.10.1"},
		{Name: "accept", Value: "*/*"},
	}
	responseFields := []HTTPHeaderField{
		{Name: ":status", Value: "200"},
		{Name: "date", Value: "Wed, 12 Aug 2026 09:56:02 GMT"},
		{Name: "content-type", Value: "application/json"},
		{Name: "content-length", Value: "425"},
		{Name: "server", Value: "cloudflare"},
		{Name: "nel", Value: `{"report_to":"cf-nel","success_fraction":0.0,"max_age":604800}`},
		{Name: "cf-cache-status", Value: "DYNAMIC"},
		{Name: "report-to", Value: `{"group":"cf-nel","max_age":604800,"endpoints":[{"url":"https://a.nel.cloudflare.com/report/v4?s=7pfuxztdjX027zNu%2B9SuWNezryy3CWUx2pDiA5B3hccBsyaV50VyZrPBmJUYguU2ZaAdi7wctnLvPTN%2FzZXE1S1FppyTYYxrmtoNVPFoHTxckPO5Sh%2BOIIW43MZrlg%3D%3D"}]}`},
		{Name: "cf-ray", Value: "a29e9b9b8f94339e-AMS"},
		{Name: "alt-svc", Value: `h3=":443"; ma=86400`},
	}
	if got := logicalHARHeaderSize(requestFields); got != 107 {
		t.Fatalf("request logical header size = %d, want 107", got)
	}
	if got := logicalHARHeaderSize(responseFields); got != 531 {
		t.Fatalf("response logical header size = %d, want 531", got)
	}

	requestStart := int64(1786528561341480)
	responseEnd := int64(1786528562077416)
	connectionTime := time.UnixMilli(1786528560782)
	entry := &TrafficEntry{
		ID:         42,
		Type:       "https",
		StartedAt:  time.UnixMicro(requestStart),
		Method:     "GET",
		URL:        "https://ifconfig.co/json",
		Host:       "ifconfig.co",
		Path:       "/json",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{
			Proto:        "HTTP/2.0",
			HeaderFields: requestFields,
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: requestStart,
				EndedAtMicros:   1786528561342064,
				HeaderSize:      107,
				BodySize:        0,
				State:           HTTPMessageStateCompleted,
			},
		},
		Response: &HTTPMessage{
			Proto:        "HTTP/2.0",
			HeaderFields: responseFields,
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: 1786528562069874,
				EndedAtMicros:   responseEnd,
				HeaderSize:      531,
				BodySize:        425,
				State:           HTTPMessageStateCompleted,
			},
		},
		Metadata: &Metadata{
			LocalSourceAddr:               "127.0.0.1:63975",
			LocalDestinationAddr:          "127.0.0.1:9080",
			RemoteSourceAddr:              "192.0.2.20:55123",
			RemoteDestinationAddr:         "104.21.54.91:443",
			LocalConnectionEstablishedAt:  connectionTime,
			RemoteConnectionEstablishedAt: connectionTime,
			Process: &ProcessInfo{
				PID:            25420,
				DisplayName:    "curlx",
				ProcessName:    "curlx.exe",
				ExecutablePath: `D:\curl\curlx.exe`,
			},
		},
	}

	var output bytes.Buffer
	result, err := WriteHAR(&output, "1.2.0-test", []HARExportEntry{{
		Entry:        entry,
		RequestBody:  HARBody{Available: true},
		ResponseBody: HARBody{Data: bytes.Repeat([]byte("x"), 425), Available: true},
	}})
	if err != nil {
		t.Fatalf("WriteHAR: %v", err)
	}
	if result != (HARWriteResult{Exported: 1}) {
		t.Fatalf("result = %#v", result)
	}
	if bytes.Contains(output.Bytes(), []byte("\n  ")) {
		t.Fatal("HAR output is unexpectedly indented")
	}
	for _, field := range []string{`"_id"`, `"_uid"`, `"_cid"`, `"_sid"`} {
		if bytes.Contains(output.Bytes(), []byte(field)) {
			t.Fatalf("HAR output unexpectedly contains private ID field %s", field)
		}
	}

	var document harEnvelope
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode HAR: %v", err)
	}
	if document.Log.Version != "1.2" || document.Log.Creator != (harCreator{Name: "FlowLens", Version: "1.2.0-test"}) {
		t.Fatalf("log header = %#v", document.Log)
	}
	if len(document.Log.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(document.Log.Entries))
	}
	got := document.Log.Entries[0]
	if got.StartedDateTime != "2026-08-12T09:56:01.341Z" || got.Time != 736 {
		t.Fatalf("entry timing = %q/%d, want 736ms", got.StartedDateTime, got.Time)
	}
	if got.Timings != (harTimings{Send: -1, Wait: -1, Receive: -1}) {
		t.Fatalf("timings = %#v", got.Timings)
	}
	if got.Request.HeadersSize != 107 || got.Request.BodySize != 0 || got.Request.StartTimestamp != requestStart {
		t.Fatalf("request = %#v", got.Request)
	}
	if got.Response.HeadersSize != 531 || got.Response.BodySize != 425 || got.Response.Content.Size != 425 {
		t.Fatalf("response sizes = header %d, body %d, content %d", got.Response.HeadersSize, got.Response.BodySize, got.Response.Content.Size)
	}
	if got.Response.Content.Compression == nil || *got.Response.Content.Compression != 0 {
		t.Fatalf("compression = %v, want 0", got.Response.Content.Compression)
	}
	if got.Response.StatusText != "" || got.Response.EndTimestamp != responseEnd || got.Response.StatusValue != "completed" {
		t.Fatalf("response timing/status = %#v", got.Response)
	}
	if got.ServerIPAddress != "104.21.54.91" || got.ServerPort == nil || *got.ServerPort != 443 || got.ClientAddress != "127.0.0.1" {
		t.Fatalf("addresses = %#v", got)
	}
	if got.Connection != "1" || got.CTime == nil || *got.CTime != connectionTime.UnixMilli() || got.STime == nil || *got.STime != connectionTime.UnixMilli() {
		t.Fatalf("standard connection/timestamps = %#v", got)
	}
	if got.App == nil || got.App.Name != "curlx" || got.App.ID != "curlx.exe" || got.App.PID != 25420 {
		t.Fatalf("app = %#v", got.App)
	}
}

func TestWriteHARBinaryAndIncompleteFallbacks(t *testing.T) {
	requestFields := []HTTPHeaderField{
		{Name: "Content-Type", Value: "application/octet-stream"},
		{Name: "Cookie", Value: "a=1; empty="},
		{Name: "Authorization", Value: "Bearer secret"},
	}
	responseFields := []HTTPHeaderField{
		{Name: "Content-Type", Value: "application/octet-stream"},
		{Name: "Set-Cookie", Value: "sid=abc; Path=/; HttpOnly; Secure"},
	}
	entry := &TrafficEntry{
		Type:       "https",
		Method:     "POST",
		URL:        "https://example.test/upload?a=1&a=&plus=x+y&encoded=%2F",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: requestFields, Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 1_000_000,
			EndedAtMicros:   1_000_100,
			HeaderSize:      logicalHARHeaderSize(requestFields),
			BodySize:        2,
			State:           HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: responseFields, Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 1_001_000,
			EndedAtMicros:   -1,
			HeaderSize:      logicalHARHeaderSize(responseFields),
			BodySize:        2,
			State:           HTTPMessageStatePending,
		}},
	}
	missingEntry := &TrafficEntry{
		Type:       "http",
		Method:     "GET",
		URL:        "http://example.test/missing",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 2_000_000, EndedAtMicros: 2_000_010, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 2_000_020, EndedAtMicros: 2_000_030, HeaderSize: 0, BodySize: 5, State: HTTPMessageStateCompleted,
		}},
	}

	var output bytes.Buffer
	result, err := WriteHAR(&output, "test", []HARExportEntry{
		{
			Entry:        entry,
			RequestBody:  HARBody{Data: []byte{0xff, 0x00}, Encoding: "base64", Available: true},
			ResponseBody: HARBody{Data: []byte{0xff, 0x7f}, Available: true},
		},
		{Entry: missingEntry, RequestBody: HARBody{Available: true}},
		{Entry: &TrafficEntry{Type: "tcp"}},
	})
	if err != nil {
		t.Fatalf("WriteHAR: %v", err)
	}
	if result != (HARWriteResult{Exported: 2, Skipped: 1, MissingBodies: 1}) {
		t.Fatalf("result = %#v", result)
	}

	var document harEnvelope
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode HAR: %v", err)
	}
	got := document.Log.Entries[0]
	if got.Time != -1 || got.Response.BodySize != -1 || got.Response.EndTimestamp != -1 || got.Response.StatusValue != "pending" {
		t.Fatalf("incomplete response = %#v", got.Response)
	}
	if got.Request.PostData == nil || got.Request.PostData.Text != "/wA=" || got.Request.PostData.Encoding != "base64" {
		t.Fatalf("binary postData = %#v", got.Request.PostData)
	}
	if len(got.Request.QueryString) != 4 || got.Request.QueryString[1] != (harNameValue{Name: "a", Value: ""}) || got.Request.QueryString[2].Value != "x y" || got.Request.QueryString[3].Value != "/" {
		t.Fatalf("queryString = %#v", got.Request.QueryString)
	}
	if len(got.Request.Cookies) != 2 || got.Request.Cookies[1] != (harCookie{Name: "empty", Value: ""}) {
		t.Fatalf("request cookies = %#v", got.Request.Cookies)
	}
	if len(got.Response.Cookies) != 1 || !got.Response.Cookies[0].HTTPOnly || !got.Response.Cookies[0].Secure {
		t.Fatalf("response cookies = %#v", got.Response.Cookies)
	}
	if got.Response.Content.Text == nil || *got.Response.Content.Text != "/38=" || got.Response.Content.Encoding != "base64" || got.Response.Content.Size != 2 || got.Response.Content.Compression != nil {
		t.Fatalf("partial binary content = %#v", got.Response.Content)
	}
	if got.Request.Headers[2].Name != "Authorization" || got.Request.Headers[2].Value != "Bearer secret" {
		t.Fatalf("credentials were not preserved: %#v", got.Request.Headers)
	}
	missing := document.Log.Entries[1]
	if missing.Response.BodySize != 5 || missing.Response.Content.Size != -1 || missing.Response.Content.Text != nil {
		t.Fatalf("missing body response = %#v", missing.Response)
	}
}

func TestWriteHARStreamsReaderBodiesAndClosesThem(t *testing.T) {
	bodyData := bytes.Repeat([]byte("streamed <body> & data\n"), 4096)
	reader := &trackingHARReadCloser{Reader: bytes.NewReader(bodyData)}
	entry := &TrafficEntry{
		Type:       "http",
		Method:     "GET",
		URL:        "http://example.test/stream",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 1, EndedAtMicros: 2, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 3, EndedAtMicros: 4, HeaderSize: 0, BodySize: int64(len(bodyData)), State: HTTPMessageStateCompleted,
		}},
	}
	var output bytes.Buffer
	_, err := WriteHAR(&output, "test", []HARExportEntry{{
		Entry:        entry,
		RequestBody:  HARBody{Available: true},
		ResponseBody: HARBody{Reader: reader, Size: int64(len(bodyData)), Available: true},
	}})
	if err != nil {
		t.Fatalf("WriteHAR: %v", err)
	}
	if !reader.closed {
		t.Fatal("streaming body reader was not closed")
	}
	var document harEnvelope
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode HAR: %v", err)
	}
	text := document.Log.Entries[0].Response.Content.Text
	if text == nil || *text != string(bodyData) {
		t.Fatal("streamed HAR response body changed")
	}
}

func TestDecodeTrafficHARBodyReadersSkipsWebSocketFramesAndRemovesSpool(t *testing.T) {
	requestData := bytes.Repeat([]byte("request"), 200_000)
	responseData := append(bytes.Repeat([]byte("response"), 200_000), 0xff)
	var compressed bytes.Buffer
	encoder, err := compresspool.AcquireZstdEncoder(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(encoder, binary.BigEndian, uint8(0)); err != nil {
		t.Fatal(err)
	}
	if err := hbinWriteBytes(encoder, requestData); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(encoder, binary.BigEndian, uint8(0)); err != nil {
		t.Fatal(err)
	}
	if err := hbinWriteBytes(encoder, responseData); err != nil {
		t.Fatal(err)
	}
	// Claim one WebSocket frame without writing it. The HAR-only decoder must
	// stop after the handshake bodies instead of trying to materialize frames.
	if err := binary.Write(encoder, binary.BigEndian, uint32(1)); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	compresspool.ReleaseZstdEncoder(encoder)

	var record bytes.Buffer
	if err := binary.Write(&record, binary.BigEndian, uint32(compressed.Len())); err != nil {
		t.Fatal(err)
	}
	record.Write(compressed.Bytes())
	temporaryDir := t.TempDir()
	requestBody, responseBody, err := DecodeTrafficHARBodyReaders(&record, temporaryDir)
	if err != nil {
		t.Fatalf("DecodeTrafficHARBodyReaders: %v", err)
	}
	defer requestBody.close()
	defer responseBody.close()
	gotRequest, err := io.ReadAll(requestBody.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gotResponse, err := io.ReadAll(responseBody.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotRequest, requestData) || !bytes.Equal(gotResponse, responseData) {
		t.Fatal("stream-decoded HAR bodies changed")
	}
	if responseBody.Encoding != "base64" {
		t.Fatalf("invalid UTF-8 response encoding = %q, want base64", responseBody.Encoding)
	}
	requestBody.close()
	responseBody.close()
	temps, err := filepath.Glob(filepath.Join(temporaryDir, ".flowlens-har-body-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("HAR body spool files remain: %v", temps)
	}
}

type trackingHARReadCloser struct {
	*bytes.Reader
	closed bool
}

func (r *trackingHARReadCloser) Close() error {
	r.closed = true
	return nil
}

func BenchmarkWriteHARLargeReaderBody(b *testing.B) {
	benchmarkWriteHARLargeReaderBody(b, "")
}

func BenchmarkWriteHARLargeBase64ReaderBody(b *testing.B) {
	benchmarkWriteHARLargeReaderBody(b, "base64")
}

func benchmarkWriteHARLargeReaderBody(b *testing.B, encoding string) {
	bodyData := bytes.Repeat([]byte("0123456789abcdef"), 1<<20) // 16 MiB
	entry := &TrafficEntry{
		Type:       "http",
		Method:     "GET",
		URL:        "http://example.test/large",
		StatusCode: 200,
		Request: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			BodySize: int64(len(bodyData)), State: HTTPMessageStateCompleted,
		}},
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(bodyData)))
	b.ResetTimer()
	for range b.N {
		_, err := WriteHAR(io.Discard, "bench", []HARExportEntry{{
			Entry:       entry,
			RequestBody: HARBody{Available: true},
			ResponseBody: HARBody{
				Reader:    io.NopCloser(bytes.NewReader(bodyData)),
				Size:      int64(len(bodyData)),
				Encoding:  encoding,
				Available: true,
			},
		}})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestHARBodyExpectedDoesNotTreatUnknownSizeAsMissing(t *testing.T) {
	if harBodyExpected(&HTTPMessage{Metrics: &HTTPMessageMetrics{BodySize: -1}}) {
		t.Fatal("unknown body size must not be reported as a missing cached body")
	}
	if harBodyExpected(&HTTPMessage{Metrics: &HTTPMessageMetrics{BodySize: 0}}) {
		t.Fatal("empty body must not be reported as a missing cached body")
	}
	if !harBodyExpected(&HTTPMessage{Metrics: &HTTPMessageMetrics{BodySize: 1}}) {
		t.Fatal("observed non-empty body should require a cached payload")
	}
}

func TestWriteHARExportsWebSocketHandshakeAndSkipsRawTCP(t *testing.T) {
	ws := &TrafficEntry{
		Type:       "wss",
		Method:     "GET",
		URL:        "wss://example.test/socket",
		StatusCode: 101,
		Status:     "101 Switching Protocols",
		Request: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 10, EndedAtMicros: 20, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 30, EndedAtMicros: 40, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
	}
	var output bytes.Buffer
	result, err := WriteHAR(&output, "test", []HARExportEntry{
		{Entry: ws, RequestBody: HARBody{Available: true}, ResponseBody: HARBody{Available: true}},
		{Entry: &TrafficEntry{Type: "tcp", RawTCP: &RawTCPTunnelInfo{HostPort: "example.test:443"}}},
	})
	if err != nil {
		t.Fatalf("WriteHAR: %v", err)
	}
	if result.Exported != 1 || result.Skipped != 1 {
		t.Fatalf("result = %#v", result)
	}
	var document harEnvelope
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("decode HAR: %v", err)
	}
	if len(document.Log.Entries) != 1 || document.Log.Entries[0].Response.Status != 101 || document.Log.Entries[0].Response.StatusText != "Switching Protocols" {
		t.Fatalf("websocket handshake = %#v", document.Log.Entries)
	}
}

func TestWriteHARFileAtomicallyReplacesDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "capture.har")
	if err := os.WriteFile(target, []byte("old contents"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	result, err := WriteHARFile(target, "test", nil)
	if err != nil {
		t.Fatalf("WriteHARFile: %v", err)
	}
	if result != (HARWriteResult{}) {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	var document harEnvelope
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("target is not HAR JSON: %v", err)
	}
	if document.Log.Entries == nil || len(document.Log.Entries) != 0 {
		t.Fatalf("empty entries encoded as %#v, want []", document.Log.Entries)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".capture.har.*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary files remain: %v", temps)
	}
}

func TestHARFileWriterStreamsAndCommitsOnlyOnClose(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "stream.har")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	writer, err := NewHARFileWriter(target, `version "quoted"`)
	if err != nil {
		t.Fatalf("NewHARFileWriter: %v", err)
	}
	defer writer.Abort()

	if err := writer.WriteEntry(HARExportEntry{Entry: &TrafficEntry{Type: "tcp"}}); err != nil {
		t.Fatalf("WriteEntry raw TCP: %v", err)
	}
	entry := &TrafficEntry{
		Type:       "http",
		Method:     "GET",
		URL:        "http://example.test/",
		StatusCode: 204,
		Status:     "204 No Content",
		Request: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 1_000, EndedAtMicros: 2_000, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 3_000, EndedAtMicros: 4_000, HeaderSize: 0, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
	}
	if err := writer.WriteEntry(HARExportEntry{
		Entry:        entry,
		RequestBody:  HARBody{Available: true},
		ResponseBody: HARBody{Available: true},
	}); err != nil {
		t.Fatalf("WriteEntry HTTP: %v", err)
	}
	beforeClose, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target before Close: %v", err)
	}
	if string(beforeClose) != "existing" {
		t.Fatalf("target was replaced before Close: %q", beforeClose)
	}

	result, err := writer.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if result != (HARWriteResult{Exported: 1, Skipped: 1}) {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read committed target: %v", err)
	}
	var document harEnvelope
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode committed HAR: %v", err)
	}
	if document.Log.Creator.Version != `version "quoted"` || len(document.Log.Entries) != 1 {
		t.Fatalf("document = %#v", document)
	}
}

func TestHARFileWriterAbortAndWriteFailurePreserveDestination(t *testing.T) {
	newWriter := func(t *testing.T, name string) (*HARFileWriter, string) {
		t.Helper()
		target := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
			t.Fatalf("seed target: %v", err)
		}
		writer, err := NewHARFileWriter(target, "test")
		if err != nil {
			t.Fatalf("NewHARFileWriter: %v", err)
		}
		return writer, target
	}

	t.Run("abort", func(t *testing.T) {
		writer, target := newWriter(t, "abort.har")
		temporary := writer.temporary
		if err := writer.Abort(); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if _, err := writer.Close(); !errors.Is(err, errHARExportAborted) {
			t.Fatalf("Close after Abort error = %v", err)
		}
		assertHARExportPreservedTarget(t, target, temporary)
	})

	t.Run("write failure", func(t *testing.T) {
		writer, target := newWriter(t, "failure.har")
		temporary := writer.temporary
		if err := writer.file.Close(); err != nil {
			t.Fatalf("close temporary file: %v", err)
		}
		entry := &TrafficEntry{Type: "http", Method: "GET", URL: "http://example.test/"}
		if err := writer.WriteEntry(HARExportEntry{Entry: entry}); err == nil {
			t.Fatal("WriteEntry unexpectedly succeeded on closed file")
		}
		if _, err := writer.Close(); err == nil {
			t.Fatal("Close unexpectedly succeeded after write failure")
		}
		assertHARExportPreservedTarget(t, target, temporary)
	})
}

func assertHARExportPreservedTarget(t *testing.T, target, temporary string) {
	t.Helper()
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "existing" {
		t.Fatalf("target changed to %q", data)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file still exists or stat failed: %v", err)
	}
}
