package proxyservice

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
)

func exportTestEntry() *TrafficEntry {
	return &TrafficEntry{
		ID: 23, Type: "https", Method: "POST", URL: "https://example.test/data?x=1", Host: "example.test", Path: "/data", StatusCode: 200, Status: "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "X-Test", Value: "first"}, {Name: "x-test", Value: ""}},
			Metrics: &HTTPMessageMetrics{StartedAtMicros: 1001, EndedAtMicros: 1003, HeaderSize: 15, BodySize: 3, State: HTTPMessageStateCompleted}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "application/octet-stream"}},
			Metrics: &HTTPMessageMetrics{StartedAtMicros: 1007, EndedAtMicros: 1012, HeaderSize: 16, BodySize: 3, State: HTTPMessageStateCompleted}},
	}
}

func exportTestSource(entries ...*TrafficEntry) func(int) (*TrafficEntry, error) {
	return func(index int) (*TrafficEntry, error) { return entries[index], nil }
}

func exportTestBodies(_ *TrafficEntry, _ string) (HARBody, HARBody, error) {
	return HARBody{Data: []byte{0, 255, 65}, Available: true, Encoding: "base64"},
		HARBody{Reader: io.NopCloser(bytes.NewReader([]byte{254, 0, 66})), Size: 3, Available: true, Encoding: "base64"}, nil
}

func TestTrafficExportFormatsPreserveBytes(t *testing.T) {
	requestHead := "POST /data?x=1 HTTP/1.1\r\nX-Test: first\r\nx-test: \r\n\r\n"
	responseHead := "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\n\r\n"
	requestBody, responseBody := string([]byte{0, 255, 65}), string([]byte{254, 0, 66})
	for _, test := range []struct {
		kind TrafficExportKind
		want string
	}{
		{TrafficExportRequestMessage, requestHead + requestBody},
		{TrafficExportRequestHeaders, requestHead},
		{TrafficExportRequestBody, requestBody},
		{TrafficExportResponseMessage, responseHead + responseBody},
		{TrafficExportResponseHeaders, responseHead},
		{TrafficExportResponseBody, responseBody},
		{TrafficExportExchange, requestHead + requestBody + "\r\n\r\n" + responseHead + responseBody},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "export")
			result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: test.kind, Path: target, TargetType: "file", TrafficIDs: []uint64{23}}, 1, exportTestSource(exportTestEntry()), exportTestBodies)
			if err != nil || result.Exported != 1 || result.Path != target || result.HeadersDegraded {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
			got, err := os.ReadFile(target)
			if err != nil || string(got) != test.want {
				t.Fatalf("output = %q, want %q, err = %v", got, test.want, err)
			}
		})
	}
}

func TestTrafficExportHeadsMatchRawFixtures(t *testing.T) {
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
			for _, request := range []bool{true, false} {
				var output bytes.Buffer
				if err := writeTrafficHTTPHead(&output, &fixture.Entry, request); err != nil {
					t.Fatal(err)
				}
				want := fixture.ResponseHead
				if request {
					want = fixture.RequestHead
				}
				if output.String() != want {
					t.Fatalf("head = %q, want %q", output.String(), want)
				}
			}
		})
	}
}

func TestTrafficExportCSVFixedColumnsAndUnknownMetrics(t *testing.T) {
	entry := exportTestEntry()
	entry.Host = "中文,\"host\"\nsecond"
	entry.Metadata = &Metadata{Process: &ProcessInfo{Status: ProcessStatusResolved, DisplayName: "进程"}, RemoteDestinationAddr: "[::1]:443"}
	tcp := &TrafficEntry{ID: 24, Type: "tcp", RawTCP: &RawTCPTunnelInfo{HostPort: "server:443", TLS: true}}
	target := filepath.Join(t.TempDir(), "all.csv")
	result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: TrafficExportCSV, Path: target, TargetType: "file"}, 2, exportTestSource(entry, tcp), nil)
	if err != nil || result.Exported != 2 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		t.Fatalf("BOM missing: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data[3:])).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := []string{"id", "method", "host", "path", "process", "status", "type", "destination", "protocol", "durationMicros", "sizeBytes"}
	if !reflect.DeepEqual(rows[0], wantHeader) || rows[1][2] != entry.Host || rows[1][4] != "进程" || rows[1][7] != "::1" || rows[1][9] != "11" || rows[1][10] != "37" || rows[2][9] != "-1" || rows[2][10] != "-1" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestTrafficExportBatchOrderAndAvailability(t *testing.T) {
	complete := exportTestEntry()
	complete.Request.HeadersTruncated = true
	complete.Request.HeaderOrderUnavailable = true
	empty := exportTestEntry()
	empty.ID = 4
	empty.Request.Metrics.BodySize = 0
	pending := exportTestEntry()
	pending.Request.Metrics.State = HTTPMessageStatePending
	failed := exportTestEntry()
	failed.Request.Metrics.State = HTTPMessageStateFailed
	missing := exportTestEntry()
	missing.ID = 5
	noResponse := exportTestEntry()
	noResponse.Response = nil
	entries := []*TrafficEntry{complete, {ID: 2, Type: "tcp"}, pending, failed, empty, missing}
	directory := t.TempDir()
	load := func(entry *TrafficEntry, directory string) (HARBody, HARBody, error) {
		if entry.ID == 4 {
			return HARBody{Available: true}, HARBody{}, nil
		}
		if entry.ID == 5 {
			return HARBody{}, HARBody{}, nil
		}
		return exportTestBodies(entry, directory)
	}
	for range 2 {
		result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: TrafficExportRequestMessage, Path: directory, TargetType: "directory"}, len(entries), exportTestSource(entries...), load)
		if err != nil || result.Exported != 2 || result.Skipped != 4 || !result.HeadersDegraded {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
		files, err := os.ReadDir(result.Path)
		if err != nil || len(files) != 2 || files[0].Name() != "000001-23-request.http" || files[1].Name() != "000005-4-request.http" {
			t.Fatalf("files = %v, err = %v", files, err)
		}
	}
	files, _ := os.ReadDir(directory)
	if len(files) != 2 {
		t.Fatalf("exports overwrote one another: %v", files)
	}
	for _, kind := range []TrafficExportKind{TrafficExportResponseHeaders, TrafficExportExchange} {
		result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: kind, Path: directory, TargetType: "directory"}, 1, exportTestSource(noResponse), load)
		if err != nil || result.Exported != 0 || result.Skipped != 1 || result.Path != "" {
			t.Fatalf("absent response: %+v, %v", result, err)
		}
	}
	files, _ = os.ReadDir(directory)
	if len(files) != 2 {
		t.Fatal("empty export left a directory")
	}
}

func TestTrafficExportHeaderWarnings(t *testing.T) {
	for _, reason := range []string{"truncated", "order-unavailable", "fields-unavailable"} {
		t.Run(reason, func(t *testing.T) {
			entry := exportTestEntry()
			switch reason {
			case "truncated":
				entry.Request.HeadersTruncated = true
			case "order-unavailable":
				entry.Request.HeaderOrderUnavailable = true
			case "fields-unavailable":
				entry.Request.HeaderFields = nil
			}
			for _, kind := range []TrafficExportKind{TrafficExportRequestHeaders, TrafficExportRequestBody, TrafficExportResponseHeaders, TrafficExportExchange, TrafficExportCSV} {
				target := filepath.Join(t.TempDir(), "export")
				result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: kind, Path: target, TargetType: "file", TrafficIDs: []uint64{entry.ID}}, 1, exportTestSource(entry), exportTestBodies)
				wantDegraded := kind == TrafficExportRequestHeaders || kind == TrafficExportExchange
				if err != nil || result.Exported != 1 || result.Skipped != 0 || result.HeadersDegraded != wantDegraded {
					t.Fatalf("kind = %s, result = %+v, err = %v", kind, result, err)
				}
			}
		})
	}
}

type exportFailingReader struct{ closed bool }

func (reader *exportFailingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (reader *exportFailingReader) Close() error             { reader.closed = true; return nil }

func TestTrafficExportFailurePreservesTargetsAndClosesReaders(t *testing.T) {
	for _, targetType := range []string{"file", "directory"} {
		t.Run(targetType, func(t *testing.T) {
			directory := t.TempDir()
			original := filepath.Join(directory, "existing")
			if err := os.WriteFile(original, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := directory
			if targetType == "file" {
				target = original
			}
			reader := &exportFailingReader{}
			_, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: TrafficExportRequestBody, Path: target, TargetType: targetType, TrafficIDs: []uint64{23}}, 1, exportTestSource(exportTestEntry()), func(*TrafficEntry, string) (HARBody, HARBody, error) {
				return HARBody{Reader: reader, Size: 10, Available: true}, HARBody{}, nil
			})
			if err == nil || !reader.closed {
				t.Fatalf("err = %v, closed = %v", err, reader.closed)
			}
			data, _ := os.ReadFile(original)
			files, _ := os.ReadDir(directory)
			if string(data) != "keep" || len(files) != 1 {
				t.Fatalf("output changed: %q, %v", data, files)
			}
		})
	}
}

func TestTrafficExportCancellationAndInvalidRequests(t *testing.T) {
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := WriteTrafficExport(ctx, TrafficExportRequest{Kind: TrafficExportRequestBody, Path: directory, TargetType: "directory"}, 1, exportTestSource(exportTestEntry()), func(entry *TrafficEntry, directory string) (HARBody, HARBody, error) {
		cancel()
		return exportTestBodies(entry, directory)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	for _, request := range []TrafficExportRequest{
		{Kind: "raw", TargetType: "file", Path: filepath.Join(directory, "bad")},
		{Kind: TrafficExportCSV, TargetType: "directory", Path: directory},
		{Kind: TrafficExportRequestBody, TargetType: "file", Path: filepath.Join(directory, "bad")},
		{Kind: TrafficExportCSV, TargetType: "file", Path: "relative.csv"},
	} {
		if _, err := WriteTrafficExport(context.Background(), request, 1, exportTestSource(exportTestEntry()), nil); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	files, _ := os.ReadDir(directory)
	if len(files) != 0 {
		t.Fatalf("temporary files remain: %v", files)
	}
}

func TestProxyTrafficExportSelectionAndMissingSide(t *testing.T) {
	service, entry, cache, requestData, _ := newPartialBodyFailureFixture(t)
	removeCachedBodyFileWithoutUpdatingIndex(t, cache, entry.ID, bodycache.KindResponse)
	target := filepath.Join(t.TempDir(), "request.bin")
	result, err := service.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportRequestBody, TargetType: "file", Path: target, TrafficIDs: []uint64{entry.ID, entry.ID}})
	if err != nil || result.Exported != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	data, _ := os.ReadFile(target)
	if !bytes.Equal(data, requestData) {
		t.Fatalf("request = %q", data)
	}
	result, err = service.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportResponseBody, TargetType: "file", Path: target, TrafficIDs: []uint64{entry.ID}})
	if err != nil || result.Exported != 0 || result.Skipped != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	data, _ = os.ReadFile(target)
	if !bytes.Equal(data, requestData) {
		t.Fatal("missing body overwrote target")
	}
	if _, err := service.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportCSV, TargetType: "file", Path: target, TrafficIDs: []uint64{999}}); err == nil {
		t.Fatal("unknown ID accepted")
	}
}

func TestTrafficExportLargeBodyStreams(t *testing.T) {
	data := bytes.Repeat([]byte{255, 0, 13, 10}, 1<<20)
	target := filepath.Join(t.TempDir(), "large.bin")
	result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: TrafficExportResponseBody, TargetType: "file", Path: target, TrafficIDs: []uint64{23}}, 1, exportTestSource(exportTestEntry()), func(*TrafficEntry, string) (HARBody, HARBody, error) {
		return HARBody{}, HARBody{Reader: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data)), Available: true}, nil
	})
	if err != nil || result.Exported != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("large output differs: %v", err)
	}
}

func TestTrafficExportHeadersDoNotLoadBodies(t *testing.T) {
	entry := exportTestEntry()
	entry.Request.Metrics.State = HTTPMessageStatePending
	target := filepath.Join(t.TempDir(), "headers.txt")
	result, err := WriteTrafficExport(context.Background(), TrafficExportRequest{Kind: TrafficExportRequestHeaders, TargetType: "file", Path: target, TrafficIDs: []uint64{23}}, 1, exportTestSource(entry), func(*TrafficEntry, string) (HARBody, HARBody, error) {
		t.Fatal("headers loaded bodies")
		return HARBody{}, HARBody{}, nil
	})
	if err != nil || result.Exported != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	data, _ := os.ReadFile(target)
	if !strings.HasSuffix(string(data), "\r\n\r\n") {
		t.Fatalf("head = %q", data)
	}
}

func TestTrafficExportRejectsReusedIDsAfterCaptureRestart(t *testing.T) {
	service, entry, _, _, _ := newPartialBodyFailureFixture(t)
	generation := service.GetTrafficExportGeneration()
	service.finalizeCaptureRestartLocked()
	replacement := service.newTrafficEntry(*exportTestEntry())
	service.storeTrafficEntry(replacement)
	if replacement.ID != entry.ID {
		t.Fatal("fixture did not reuse the previous ID")
	}
	target := filepath.Join(t.TempDir(), "capture")
	_, err := service.ExportTraffic(context.Background(), TrafficExportRequest{
		Kind: TrafficExportCSV, Path: target, TargetType: "file", TrafficIDs: []uint64{entry.ID}, CaptureGeneration: &generation,
	})
	if err == nil || !strings.Contains(err.Error(), "capture changed") {
		t.Fatalf("stale CSV error = %v", err)
	}
	_, err = service.ExportHAR(HARExportRequest{Path: target, TrafficIDs: []uint64{entry.ID}, CaptureGeneration: &generation})
	if err == nil || !strings.Contains(err.Error(), "capture changed") {
		t.Fatalf("stale HAR error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("stale export created output")
	}
}
