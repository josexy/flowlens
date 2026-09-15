package proxyservice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

const importTestEntry = `{"startedDateTime":"2026-09-15T01:00:00.123456Z","time":25,"request":{"method":"GET","url":"https://example.com/a?q=1&q=2","httpVersion":"h2","headers":[{"name":"X-Test","value":"a"},{"name":"x-test","value":""}],"bodySize":0},"response":{"status":200,"statusText":"OK","httpVersion":"h2","headers":[{"name":"Content-Type","value":"text/plain"},{"name":"Content-Encoding","value":"gzip"}],"bodySize":3,"content":{"size":5,"text":"hello"}},"timings":{"blocked":0,"dns":2,"connect":6,"ssl":4,"send":3,"wait":5,"receive":9}}`

func importTestArchive(entries ...string) string {
	return `{"log":{"version":"1.2","entries":[` + strings.Join(entries, ",") + `]}}`
}

func TestHARImportBrowserEntry(t *testing.T) {
	entry, body, diagnostics, err := convertHARImportEntry([]byte(importTestEntry), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer body.closeReqBodyReaderSafely()
	defer body.closeRspBodyReaderSafely()
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if entry.ID != 7 || entry.Type != "https" || entry.Path != "/a?q=1&q=2" || entry.Request.Proto != "HTTP/2.0" {
		t.Fatalf("entry: %+v", entry)
	}
	if !entry.Request.HeaderOrderUnavailable || !reflect.DeepEqual(entry.Request.HeaderFields, []HTTPHeaderField{{Name: "X-Test", Value: "a"}, {Name: "x-test", Value: ""}}) {
		t.Fatal(entry.Request)
	}
	if entry.Request.Metrics.HeaderSize != logicalHTTPRequestHeaderSize(entry) || entry.Response.Metrics.BodySize != 3 {
		t.Fatal("wrong size semantics")
	}
	base := entry.StartedAt.UnixMicro()
	if entry.Request.Metrics.EndedAtMicros != base+11000 || entry.Response.Metrics.StartedAtMicros != base+16000 || entry.Response.Metrics.EndedAtMicros != base+25000 {
		t.Fatalf("metrics: %+v %+v", entry.Request.Metrics, entry.Response.Metrics)
	}
	phases := harConnectionPhases(entry.Metadata.ConnectionTimings)
	if phases[0][1]-phases[0][0] != 2000 || phases[1][1]-phases[1][0] != 2000 || phases[2][1]-phases[2][0] != 4000 {
		t.Fatal(phases)
	}
	data, _ := io.ReadAll(body.ResponseBodyReader)
	if string(data) != "hello" || body.RequestBodyUnavailable || body.ResponseBodyUnavailable {
		t.Fatal("decoded HAR body should not be decompressed again")
	}
}

func TestHARImportBodyRepresentations(t *testing.T) {
	tests := []struct {
		name, request, response, req, rsp string
		reqMissing, rspMissing            bool
	}{
		{"empty", `{"bodySize":0}`, `{"content":{"text":"","size":0}}`, "", "", false, false},
		{"missing", `{"bodySize":12}`, `{"content":{"size":12}}`, "", "", true, true},
		{"unknown", `{}`, `{"content":{}}`, "", "", true, true},
		{"binary", `{"postData":{"text":"AP8=","_encoding":"base64"}}`, `{"content":{"text":"AP8=","encoding":"base64"}}`, "\x00\xff", "\x00\xff", false, false},
		{"bad_base64", `{"postData":{"text":"?","_encoding":"base64"}}`, `{"content":{"text":"?","encoding":"base64"}}`, "", "", true, true},
		{"urlencoded", `{"postData":{"mimeType":"application/x-www-form-urlencoded","params":[{"name":"x","value":"a b"},{"name":"x","value":""}]}}`, `{"content":{"text":""}}`, "x=a+b&x=", "", false, false},
		{"multipart", `{"postData":{"mimeType":"multipart/form-data","params":[{"name":"file","fileName":"C:/secret.txt"}]}}`, `{"content":{"text":""}}`, "", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input map[string]any
			_ = json.Unmarshal([]byte(importTestEntry), &input)
			var req, rsp map[string]any
			_ = json.Unmarshal([]byte(tt.request), &req)
			_ = json.Unmarshal([]byte(tt.response), &rsp)
			req["method"], req["url"], rsp["status"] = "POST", "https://example.com/", 200
			input["request"], input["response"] = req, rsp
			raw, _ := json.Marshal(input)
			_, body, _, err := convertHARImportEntry(raw, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer body.closeReqBodyReaderSafely()
			defer body.closeRspBodyReaderSafely()
			a, _ := io.ReadAll(body.RequestBodyReader)
			b, _ := io.ReadAll(body.ResponseBodyReader)
			if string(a) != tt.req || string(b) != tt.rsp || body.RequestBodyUnavailable != tt.reqMissing || body.ResponseBodyUnavailable != tt.rspMissing {
				t.Fatalf("body: %q %q %+v", a, b, body)
			}
		})
	}
}

func TestHARImportUnavailableAndExtendedTimings(t *testing.T) {
	for _, tt := range []struct {
		name           string
		edit           func(map[string]any)
		reqEnd, rspEnd int64
	}{
		{"missing", func(v map[string]any) { delete(v, "time"); delete(v, "timings") }, -1, -1},
		{"inconsistent", func(v map[string]any) { v["time"] = 1 }, -1, -1},
		{"failed", func(v map[string]any) {
			v["response"].(map[string]any)["status"] = 0
			v["_error"] = "connection closed"
		}, 11000, -1},
		{"extensions", func(v map[string]any) {
			v["time"] = nil
			request := v["request"].(map[string]any)
			response := v["response"].(map[string]any)
			request["_startTimestamp"], request["_endTimestamp"], request["_status"] = 100, 150, "completed"
			response["_startTimestamp"], response["_endTimestamp"], response["_status"] = 200, 300, "completed"
		}, 150, 300},
		{"canceled_extension", func(v map[string]any) {
			response := v["response"].(map[string]any)
			response["_startTimestamp"], response["_endTimestamp"], response["_status"] = 200, 300, "canceled"
		}, 11000, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var input map[string]any
			_ = json.Unmarshal([]byte(importTestEntry), &input)
			tt.edit(input)
			raw, _ := json.Marshal(input)
			entry, body, _, err := convertHARImportEntry(raw, 1)
			if err != nil {
				t.Fatal(err)
			}
			body.closeReqBodyReaderSafely()
			body.closeRspBodyReaderSafely()
			base := entry.StartedAt.UnixMicro()
			reqEnd, rspEnd := entry.Request.Metrics.EndedAtMicros, entry.Response.Metrics.EndedAtMicros
			if tt.name != "extensions" {
				if reqEnd >= 0 {
					reqEnd -= base
				}
				if rspEnd >= 0 {
					rspEnd -= base
				}
			}
			if reqEnd != tt.reqEnd || rspEnd != tt.rspEnd {
				t.Fatalf("endpoints=%d,%d want=%d,%d", reqEnd, rspEnd, tt.reqEnd, tt.rspEnd)
			}
		})
	}
}

func TestHARImportTransactionAndLimits(t *testing.T) {
	for _, tt := range []struct {
		name, raw         string
		limits            harImportLimits
		fail              bool
		imported, skipped int
	}{
		{"valid", "\xef\xbb\xbf" + importTestArchive(importTestEntry, "{}", importTestEntry), defaultHARImportLimits, false, 2, 1},
		{"empty", importTestArchive(), defaultHARImportLimits, false, 0, 0},
		{"invalid_entries", importTestArchive("{}", "null"), defaultHARImportLimits, false, 0, 2},
		{"unsupported_date", importTestArchive(strings.Replace(importTestEntry, "2026-09-15", "2500-09-15", 1)), defaultHARImportLimits, false, 0, 1},
		{"har_1_1", strings.Replace(importTestArchive(importTestEntry), `"version":"1.2"`, `"version":"1.1"`, 1), defaultHARImportLimits, false, 1, 0},
		{"truncated", strings.TrimSuffix(importTestArchive(importTestEntry), "}"), defaultHARImportLimits, true, 0, 0},
		{"trailing", importTestArchive(importTestEntry) + "{}", defaultHARImportLimits, true, 0, 0},
		{"missing_entries", `{"log":{}}`, defaultHARImportLimits, true, 0, 0},
		{"version_after_entries", `{"log":{"entries":[` + importTestEntry + `],"version":"9"}}`, defaultHARImportLimits, true, 0, 0},
		{"duplicate_entries", `{"log":{"entries":[],"entries":[]}}`, defaultHARImportLimits, true, 0, 0},
		{"entry_limit", importTestArchive(importTestEntry, importTestEntry), harImportLimits{1 << 20, 32 << 10, 1}, true, 0, 0},
		{"value_limit", importTestArchive(importTestEntry), harImportLimits{1 << 20, 100, 100}, true, 0, 0},
		{"file_limit", importTestArchive(importTestEntry), harImportLimits{100, 32 << 10, 100}, true, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			result, err := importHARHistory(context.Background(), strings.NewReader(tt.raw), dir, HistoryMetadata{Key: "import-test", CreatedAt: time.Now().UnixMilli()}, tt.limits, os.Rename)
			if (err != nil) != tt.fail {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			files, _ := os.ReadDir(dir)
			if tt.fail || tt.imported == 0 {
				if len(files) != 0 {
					t.Fatalf("partial files: %v", files)
				}
				return
			}
			if len(files) != 2 || result.Imported != tt.imported || result.Skipped != tt.skipped || result.Metadata.FormatVersion != 2 {
				t.Fatalf("%v %+v", files, result)
			}
			idx, _ := os.ReadFile(filepath.Join(dir, "import-test.hidx"))
			if binary.BigEndian.Uint32(idx[:4]) != uint32(tt.imported) {
				t.Fatal("index count not patched")
			}
			data, _ := os.ReadFile(filepath.Join(dir, "import-test.hbin"))
			md, err := DecodeHistoryMetadata(bytes.NewReader(data))
			if err != nil || md.Total != tt.imported {
				t.Fatal(md, err)
			}
			entry, err := DecodeTrafficEntryWithVersion(bytes.NewReader(data[binary.BigEndian.Uint32(idx[12:16]):]), 2)
			if err != nil || entry.ID != 1 {
				t.Fatal(entry, err)
			}
			view, err := DecodeTrafficBody(bytes.NewReader(data[binary.BigEndian.Uint32(idx[16:20]):]))
			if err != nil || view.ResponseBody != "hello" {
				t.Fatal(view, err)
			}
		})
	}
}

func TestHARImportPartialExtensionsKeepUnknownValues(t *testing.T) {
	var input map[string]any
	_ = json.Unmarshal([]byte(importTestEntry), &input)
	response := input["response"].(map[string]any)
	response["_status"], response["_startTimestamp"], response["_endTimestamp"] = "canceled", -1, -1
	input["_connectionTimings"] = map[string]any{"dnsStartedAtMicros": 100, "dnsEndedAtMicros": 150, "connectStartedAtMicros": 200}
	raw, _ := json.Marshal(input)
	entry, body, _, err := convertHARImportEntry(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer body.closeReqBodyReaderSafely()
	defer body.closeRspBodyReaderSafely()
	if m := entry.Response.Metrics; m.State != HTTPMessageStateCanceled || m.StartedAtMicros != -1 || m.EndedAtMicros != -1 {
		t.Fatalf("cancellation lost: %+v", m)
	}
	c := entry.Metadata.ConnectionTimings
	if c.DNSStartedAtMicros != 100 || c.DNSEndedAtMicros != 150 || c.ConnectStartedAtMicros != -1 || c.ConnectEndedAtMicros != -1 || c.TLSStartedAtMicros != -1 || c.TLSEndedAtMicros != -1 {
		t.Fatalf("absent or incomplete phases must stay unknown: %+v", c)
	}
}

func TestHARImportRollbackAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmtBool(cancel), func(t *testing.T) {
			dir := t.TempDir()
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			if cancel {
				stop()
			}
			calls := 0
			_, err := importHARHistory(ctx, strings.NewReader(importTestArchive(importTestEntry)), dir, HistoryMetadata{Key: "rollback"}, defaultHARImportLimits, func(a, b string) error {
				calls++
				if calls == 2 {
					return errors.New("injected rename failure")
				}
				return os.Rename(a, b)
			})
			if err == nil {
				t.Fatal("expected failure")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatalf("partial history: %v", files)
			}
		})
	}
}
func fmtBool(value bool) string {
	if value {
		return "cancel"
	}
	return "commit_failure"
}

type harImportCancelReader struct{ cancel context.CancelFunc }

func (r harImportCancelReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, context.Canceled
}

func TestHARImportReadFailureAndMidFileCancellationRollBack(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmtBool(cancel), func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			want := errors.New("injected read failure")
			var tail io.Reader = iotest.ErrReader(want)
			if cancel {
				want, tail = context.Canceled, harImportCancelReader{stop}
			}
			// Leave another entry pending after a valid entry has been written.
			source := io.MultiReader(strings.NewReader(`{"log":{"entries":[`+importTestEntry+`,`), tail)
			dir := t.TempDir()
			_, err := ImportHARHistory(ctx, source, dir, HistoryMetadata{Key: "partial"})
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatalf("partial files survived: %v", files)
			}
		})
	}
}

type oversizedHistorySeeker struct {
	io.Writer
	offset int64
}

func (s oversizedHistorySeeker) Seek(int64, int) (int64, error) { return s.offset, nil }
func TestHBINRejectsOffsetOverflow(t *testing.T) {
	err := encodeTrafficEntry(io.Discard, oversizedHistorySeeker{io.Discard, int64(^uint32(0)) + 1}, &TrafficEntry{}, nil)
	if err == nil || !strings.Contains(err.Error(), "offset") {
		t.Fatal(err)
	}
}

func TestHARImportMissingBodyPersistsAndBlocksRecovery(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Replace(importTestEntry, `"bodySize":0`, `"bodySize":42`, 1)
	result, err := ImportHARHistory(context.Background(), strings.NewReader(importTestArchive(raw)), dir, HistoryMetadata{Key: "missing"})
	if err != nil || result.MissingBodies != 1 {
		t.Fatal(result, err)
	}
	index, _ := os.ReadFile(filepath.Join(dir, "missing.hidx"))
	data, _ := os.ReadFile(filepath.Join(dir, "missing.hbin"))
	view, err := DecodeTrafficBody(bytes.NewReader(data[binary.BigEndian.Uint32(index[16:20]):]))
	if err != nil || !view.RequestBodyUnavailable || view.ResponseBodyUnavailable {
		t.Fatal(view, err)
	}
	if _, _, err := DecodeTrafficRequestBody(bytes.NewReader(data[binary.BigEndian.Uint32(index[16:20]):])); err == nil {
		t.Fatal("missing body accepted for resend")
	}
	if _, err := (&ProxyService{}).RecoverRequestBodyForEditing("https://example.com", nil, *view); err == nil {
		t.Fatal("missing body accepted for editor")
	}
}
