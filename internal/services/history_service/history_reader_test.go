package historyservice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/pkg/fs"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestHistorySummaryCancelsWhileSkippingCookie(t *testing.T) {
	var encoded bytes.Buffer
	writeHistoryValue(t, &encoded, uint64(1))
	writeHistoryString(t, &encoded, "https")
	writeHistoryValue(t, &encoded, int64(0)) // StartedAt
	for _, value := range []string{"GET", "https://example.test/", "example.test", "/"} {
		writeHistoryString(t, &encoded, value)
	}
	writeHistoryValue(t, &encoded, int32(200))
	writeHistoryString(t, &encoded, "OK")
	writeHistoryValue(t, &encoded, uint8(0)) // Metadata
	writeHistoryValue(t, &encoded, uint8(1)) // Request
	writeHistoryString(t, &encoded, "HTTP/1.1")
	writeHistoryHeaderFields(t, &encoded, []proxyservice.HTTPHeaderField{{Name: "Cookie", Value: strings.Repeat("x", 1<<20)}})
	writeHistoryHeaderFields(t, &encoded, nil)
	for range 4 { // Request metrics, response, error, connection timings.
		writeHistoryValue(t, &encoded, uint8(0))
	}
	if _, err := proxyservice.DecodeTrafficEntrySummaryWithVersion(bytes.NewReader(encoded.Bytes()), 3); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancelHistoryReader{reader: bytes.NewReader(encoded.Bytes()), cancel: cancel}
	reader := bufio.NewReader(historyContextReader{ctx: ctx, reader: source})
	entry, err := proxyservice.DecodeTrafficEntrySummaryWithVersion(reader, 3)
	if entry != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("entry=%v, error=%v", entry, err)
	}
	if source.bytesRead > 8192 {
		t.Fatalf("read %d bytes after cancellation during the skipped field", source.bytesRead)
	}
}

type cancelHistoryReader struct {
	reader    io.Reader
	cancel    context.CancelFunc
	bytesRead int
}

func (r *cancelHistoryReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += n
	r.cancel()
	return n, err
}

func TestStagedHistoryReadsPreserveVersionsAndSurviveMissingBody(t *testing.T) {
	directory := setupHistoryTestStorage(t)
	for _, version := range []uint16{1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			key := fmt.Sprintf("staged-%d", version)
			writeHistoryWithVersion(t, directory, key, version)
			s := New(nil, nil)
			if err := s.refreshHistories(); err != nil {
				t.Fatal(err)
			}
			full, err := s.GetHistoryDetail(context.Background(), key, 9001)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := s.GetHistoryEntry(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(entry, full.Entry) {
				t.Fatalf("metadata differs: %v", err)
			}
			body, err := s.GetHistoryBody(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(body, full.Body) {
				t.Fatalf("body differs: %v", err)
			}
			index, err := s.historyIndex(key)
			if err != nil {
				t.Fatal(err)
			}
			position, _ := index.entries.Get(9001)
			if err := os.Truncate(filepath.Join(directory, fs.GetHBinFileName(key)), int64(position.bodyIndex)); err != nil {
				t.Fatal(err)
			}
			entry, err = s.GetHistoryEntry(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(entry, full.Entry) {
				t.Fatalf("metadata unnecessarily depends on the body: %v", err)
			}
			if _, err := s.GetHistoryBody(context.Background(), key, 9001); err == nil {
				t.Fatal("missing body accepted")
			}
			partial, err := s.GetHistoryDetail(context.Background(), key, 9001)
			if err == nil || !reflect.DeepEqual(partial.Entry, full.Entry) {
				t.Fatal("complete action read changed its partial-error behavior")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := s.GetHistoryEntry(ctx, key, 9001); !errors.Is(err, context.Canceled) {
				t.Fatalf("metadata cancellation: %v", err)
			}
			if _, err := s.GetHistoryBody(ctx, key, 9001); !errors.Is(err, context.Canceled) {
				t.Fatalf("body cancellation: %v", err)
			}
			if _, err := s.GetHistoryEntry(context.Background(), key, 999); err == nil {
				t.Fatal("missing metadata accepted")
			}
		})
	}
}

func TestHistorySummariesAndDetailsReadSupportedVersions(t *testing.T) {
	directory := setupHistoryTestStorage(t)
	for _, version := range []uint16{1, 2, 3} {
		key := fmt.Sprintf("version-%d", version)
		writeHistoryWithVersion(t, directory, key, version)
	}
	writeRawTCPHistory(t, directory, "tcp")
	s := New(nil, nil)
	if err := s.refreshHistories(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version-1", "version-2", "version-3", "tcp"} {
		full, err := s.GetHistory(key)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.ListHistoryEntries(context.Background(), key)
		if err != nil || len(rows) != len(full) {
			t.Fatalf("rows=%d, error=%v", len(rows), err)
		}
		for i, row := range rows {
			want := historySummaryReference(full[i])
			if !reflect.DeepEqual(row, want) {
				t.Fatal("selective decoding differs from the previous summary")
			}
			if row.Request != nil && len(row.Request.HeaderFields) != 0 {
				t.Fatal("summary retained request headers")
			}
			if !reflect.DeepEqual(row.Metadata.Process, full[i].Metadata.Process) || !reflect.DeepEqual(row.Metadata.ConnectionTimings, full[i].Metadata.ConnectionTimings) {
				t.Fatal("summary lost process or timings")
			}
			detail, err := s.GetHistoryDetail(context.Background(), key, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(detail.Entry, full[i]) {
				t.Fatal("detail differs from complete history record")
			}
			if row.Type == "tcp" && detail.Body != nil {
				t.Fatal("TCP loaded HTTP bodies")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListHistoryEntries(ctx, "version-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list: %v", err)
	}
	if _, err := s.GetHistoryDetail(ctx, "version-1", 9001); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled detail: %v", err)
	}
	if _, err := s.GetHistoryDetail(context.Background(), "version-1", 99); err == nil {
		t.Fatal("missing record accepted")
	}
}

func TestHistorySummaryPreservesTableSemantics(t *testing.T) {
	var full, summaries []*logic.TrafficEntry
	for i, contentType := range []string{"application/json", "text/event-stream", "image/png", "text/html", "application/octet-stream"} {
		entry := &proxyservice.TrafficEntry{ID: uint64(i + 1), Type: "https", Method: "POST", Host: fmt.Sprintf("host%d.test", i), URL: fmt.Sprintf("https://host%d.test/path", i), Path: "/path", StatusCode: 200 + i, Status: "OK",
			Request:  &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []proxyservice.HTTPHeaderField{{Name: "Cookie", Value: "large"}, {Name: "Content-Type", Value: contentType}}, Metrics: &proxyservice.HTTPMessageMetrics{StartedAtMicros: 100, HeaderSize: 90, BodySize: -1, State: proxyservice.HTTPMessageStateCompleted}},
			Response: &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Extra", Value: "large"}, {Name: "content-TYPE", Value: contentType}, {Name: "Content-Type", Value: "ignored"}}, TrailerFields: []proxyservice.HTTPHeaderField{{Name: "X-Trailer", Value: "kept in full detail"}}, HeadersTruncated: i == 0, HeaderOrderUnavailable: true, Metrics: &proxyservice.HTTPMessageMetrics{EndedAtMicros: int64(200 + i), HeaderSize: 60, BodySize: int64(i), State: proxyservice.HTTPMessageStateCompleted}},
			Metadata: &proxyservice.Metadata{RemoteDestinationAddr: fmt.Sprintf("127.0.0.%d:443", i+1), LocalSourceAddr: "127.0.0.1:1000", Certificate: &proxyservice.ServerCertificate{SerialNumber: "certificate"}, TLS: &proxyservice.TLSState{SelectedALPN: "h2", SupportedVersion: []string{"TLS 1.3"}}, Process: &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, DisplayName: fmt.Sprint(i), ExecutablePath: fmt.Sprint(i)}}}
		full = append(full, entry)
		copy := historySummaryReference(entry)
		if copy.Metadata.Certificate != nil || copy.Metadata.TLS != nil || len(copy.Response.HeaderFields) != 1 || copy.Response.TrailerFields != nil {
			t.Fatal("summary retained full details")
		}
		summaries = append(summaries, copy)
		for _, tab := range logic.TrafficFilterTabs {
			if logic.TrafficMatchesTab(entry, tab) != logic.TrafficMatchesTab(copy, tab) {
				t.Fatalf("filter %s differs", tab)
			}
		}
		for _, query := range []string{"host", "path", "missing"} {
			if logic.TrafficMatchesSearch(entry, query) != logic.TrafficMatchesSearch(copy, query) {
				t.Fatal("search differs")
			}
		}
		if logic.TrafficProcessCategory(entry) != logic.TrafficProcessCategory(copy) || logic.CapabilitiesOf(entry) != logic.CapabilitiesOf(copy) {
			t.Fatal("category or actions differ")
		}
		for _, field := range []logic.CopyField{logic.CopyTarget, logic.CopyURL, logic.CopyDomain, logic.CopyPath, logic.CopyServerIP, logic.CopyClientIP, logic.CopyServerAddr, logic.CopyClientAddr} {
			if logic.TrafficCopyValue(entry, field) != logic.TrafficCopyValue(copy, field) {
				t.Fatalf("copy %s differs", field)
			}
		}
		for _, group := range logic.TrafficExportGroups {
			for _, format := range group {
				if logic.CanExportTraffic(entry, format) != logic.CanExportTraffic(copy, format) || logic.TrafficExportExtension(format, entry) != logic.TrafficExportExtension(format, copy) {
					t.Fatalf("export %s differs", format)
				}
			}
		}
	}
	for _, column := range logic.TrafficColumnKeys {
		for _, order := range []logic.SortOrder{logic.SortAsc, logic.SortDesc} {
			sort := logic.TrafficSort{Key: column, Order: order}
			a, b := logic.NewTrafficTableSorter().Sort(full, sort), logic.NewTrafficTableSorter().Sort(summaries, sort)
			for i := range a {
				if a[i].ID != b[i].ID {
					t.Fatalf("sort %s/%s differs", column, order)
				}
			}
		}
	}
	for _, entry := range full {
		if len(entry.Response.HeaderFields) != 3 || entry.Metadata.Certificate == nil {
			t.Fatal("summary changed original")
		}
	}
	tcp := &proxyservice.TrafficEntry{Type: "tcp", Metadata: &proxyservice.Metadata{TLS: &proxyservice.TLSState{SelectedALPN: "h2", SupportedALPN: []string{"h2", "http/1.1"}}}}
	wantProtocol := logic.TrafficProtocol(tcp)
	tcp = historySummaryReference(tcp)
	if logic.TrafficProtocol(tcp) != wantProtocol || len(tcp.Metadata.TLS.SupportedALPN) != 0 {
		t.Fatal("TCP summary lost ALPN or retained full TLS")
	}
}

func TestHistorySummaryRetainedMemory(t *testing.T) {
	directory := setupHistoryTestStorage(t)
	key := testHistoryKeys(1)[0]
	func() {
		const count = 2000
		entry := `{"startedDateTime":"2020-01-01T00:00:00Z","time":3,"request":{"method":"GET","url":"https://example.test/a","httpVersion":"HTTP/1.1","headers":[{"name":"Cookie","value":"` + strings.Repeat("x", 4096) + `"}],"bodySize":0},"response":{"status":200,"statusText":"OK","httpVersion":"HTTP/1.1","headers":[],"bodySize":0,"content":{"text":"","size":0}},"timings":{"send":1,"wait":1,"receive":1}}`
		input := `{"log":{"version":"1.2","entries":[` + strings.Repeat(entry+",", count-1) + entry + `]}}`
		result, err := proxyservice.ImportHARHistory(context.Background(), strings.NewReader(input), directory, proxyservice.HistoryMetadata{Key: key})
		if err != nil || result.Imported != count {
			t.Fatalf("import: %+v, %v", result, err)
		}
	}()
	s := New(nil, nil)
	if err := s.refreshHistories(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.historyIndex(key); err != nil {
		t.Fatal(err)
	}
	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	type memoryUse struct{ retained, allocated int64 }
	measure := func(summary bool) memoryUse {
		before := heap()
		var beforeRead, afterRead runtime.MemStats
		runtime.ReadMemStats(&beforeRead)
		entries, err := s.readHistory(context.Background(), key, summary)
		runtime.ReadMemStats(&afterRead)
		if err != nil || len(entries) != 2000 {
			t.Fatalf("read: %d, %v", len(entries), err)
		}
		delta := int64(heap()) - int64(before)
		runtime.KeepAlive(entries)
		return memoryUse{retained: delta, allocated: int64(afterRead.TotalAlloc - beforeRead.TotalAlloc)}
	}
	full, summary := measure(false), measure(true)
	t.Logf("2000 records, 4 KiB Cookie: retained full %.2f MiB, summary %.2f MiB; allocated full %.2f MiB, summary %.2f MiB", float64(full.retained)/(1<<20), float64(summary.retained)/(1<<20), float64(full.allocated)/(1<<20), float64(summary.allocated)/(1<<20))
	if full.retained < 7<<20 || summary.retained > full.retained/2 {
		t.Fatal("summary did not release large row headers")
	}
	if full.allocated-summary.allocated < 6<<20 {
		t.Fatal("history list still allocates discarded Cookie payloads")
	}
	// Details loaded later still contain the exact header bytes.
	detail, err := s.GetHistoryDetail(context.Background(), key, 1)
	if err != nil || detail.Entry == nil || !slices.ContainsFunc(detail.Entry.Request.HeaderFields, func(f proxyservice.HTTPHeaderField) bool { return f.Name == "Cookie" && len(f.Value) == 4096 }) {
		t.Fatalf("full detail lost Cookie: %v", err)
	}
}

// Independent reference for the previous decode-then-strip behavior. Keep it
// test-only: production must skip these fields before allocating their payloads.
func historySummaryReference(entry *proxyservice.TrafficEntry) *proxyservice.TrafficEntry {
	result := *entry
	for _, message := range []**proxyservice.HTTPMessage{&result.Request, &result.Response} {
		if *message == nil {
			continue
		}
		copy := **message
		copy.HeaderFields, copy.TrailerFields = nil, nil
		for _, field := range (*message).HeaderFields {
			if strings.EqualFold(field.Name, "Content-Type") {
				copy.HeaderFields = []proxyservice.HTTPHeaderField{field}
				break
			}
		}
		*message = &copy
	}
	if entry.Metadata != nil {
		metadata := *entry.Metadata
		metadata.Certificate, metadata.TLS = nil, nil
		if entry.Type == "tcp" && entry.Metadata.TLS != nil {
			metadata.TLS = &proxyservice.TLSState{SelectedALPN: entry.Metadata.TLS.SelectedALPN}
		}
		result.Metadata = &metadata
	}
	return &result
}
