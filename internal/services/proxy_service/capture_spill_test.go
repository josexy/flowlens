package proxyservice

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type spillFixture struct {
	entry *TrafficEntry
	body  func() *trafficBodyViewInner
}

func spillFixtures() []spillFixture {
	started := time.Date(2026, 11, 17, 9, 30, 0, 123456000, time.UTC)
	readerBody := func(request, response string, binary bool) func() *trafficBodyViewInner {
		return func() *trafficBodyViewInner {
			return &trafficBodyViewInner{
				RequestBodyReader:  io.NopCloser(strings.NewReader(request)),
				RequestBodySize:    int64(len(request)),
				ResponseBodyReader: io.NopCloser(strings.NewReader(response)),
				ResponseBodySize:   int64(len(response)),
				ResponseBodyBinary: binary,
			}
		}
	}
	return []spillFixture{
		{
			entry: &TrafficEntry{
				ID: 7, Type: "https", StartedAt: started, Method: "POST",
				URL: "https://spill.test/api", Host: "spill.test", Path: "/api", StatusCode: 201, Status: "201 Created",
				Request: &HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HTTPHeaderField{{Name: "content-type", Value: "application/json"}},
					Metrics: &HTTPMessageMetrics{StartedAtMicros: started.UnixMicro(), EndedAtMicros: started.UnixMicro() + 10, HeaderSize: 40, BodySize: 13, State: HTTPMessageStateCompleted}},
				Response: &HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HTTPHeaderField{{Name: "x-dup", Value: "1"}, {Name: "x-dup", Value: ""}},
					TrailerFields: []HTTPHeaderField{{Name: "grpc-status", Value: "0"}},
					Metrics:       &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: 2048, State: HTTPMessageStateCompleted}},
			},
			body: readerBody(`{"spill":true}`, strings.Repeat("\x00\x01binary", 256), true),
		},
		{
			entry: &TrafficEntry{
				ID: 9, Type: "wss", StartedAt: started, Method: "GET", URL: "wss://spill.test/socket", Host: "spill.test", Path: "/socket", StatusCode: 101,
				Request:  &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "Upgrade", Value: "websocket"}}},
				Response: &HTTPMessage{Proto: "HTTP/1.1"},
			},
			body: func() *trafficBodyViewInner {
				return &trafficBodyViewInner{WsMsgsTruncated: true, WebSocketMessages: []*WebSocketMessage{
					{Direction: "send", MsgType: "text", Data: "hello", DataSize: 5},
					{Direction: "receive", MsgType: "binary", Data: "\x00\xff\x10", DataSize: 3, Error: &TrafficError{Timestamp: started, Error: "closed"}},
				}}
			},
		},
		{
			entry: &TrafficEntry{
				ID: 8, Type: "tcp", StartedAt: started, URL: "tcp://db.spill.test:5432", Host: "db.spill.test:5432",
				RawTCP: &RawTCPTunnelInfo{Source: RawTCPTunnelSourceSOCKS5, HostPort: "db.spill.test:5432", TLS: true},
			},
			body: func() *trafficBodyViewInner { return &trafficBodyViewInner{} },
		},
	}
}

// directHBIN encodes fixture with hbinWriteEntry into its own file and decodes
// it again: the result spilling must reproduce.
func directHBIN(t *testing.T, fixture spillFixture) (*TrafficEntry, *TrafficBodyView) {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "direct.hbin"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var index bytes.Buffer
	if err := hbinWriteEntry(&index, file, fixture.entry, fixture.body()); err != nil {
		t.Fatalf("hbinWriteEntry: %v", err)
	}
	return decodeHBINAt(t, file, index.Bytes(), 0)
}

func decodeHBINAt(t *testing.T, r io.ReaderAt, index []byte, base int64) (*TrafficEntry, *TrafficBodyView) {
	t.Helper()
	header := base + int64(binary.BigEndian.Uint32(index[8:12]))
	body := base + int64(binary.BigEndian.Uint32(index[12:16]))
	entry, err := DecodeTrafficEntryWithVersion(io.NewSectionReader(r, header, body-header), hbinVersionCurrent)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	view, err := DecodeTrafficBodyWithVersion(io.NewSectionReader(r, body, 1<<40), hbinVersionCurrent)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return entry, view
}

func requireSpilledEqualsDirect(t *testing.T, store *captureSpillStore, fixture spillFixture) {
	t.Helper()
	wantEntry, wantBody := directHBIN(t, fixture)
	gotEntry, err := store.Entry(fixture.entry.ID)
	if err != nil {
		t.Fatalf("Entry %d: %v", fixture.entry.ID, err)
	}
	gotBody, err := store.Body(fixture.entry.ID)
	if err != nil {
		t.Fatalf("Body %d: %v", fixture.entry.ID, err)
	}
	if !reflect.DeepEqual(gotEntry, wantEntry) {
		t.Fatalf("spilled entry %d differs:\n got %+v\nwant %+v", fixture.entry.ID, gotEntry, wantEntry)
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("spilled body %d differs:\n got %+v\nwant %+v", fixture.entry.ID, gotBody, wantBody)
	}
}

func TestCaptureSpillStoreRoundTripsEntries(t *testing.T) {
	store := newCaptureSpillStore(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	fixtures := spillFixtures()
	// IDs 7, 9, 8: the last one arrives out of order.
	for _, fixture := range fixtures {
		if err := store.Append(fixture.entry, fixture.body()); err != nil {
			t.Fatalf("Append %d: %v", fixture.entry.ID, err)
		}
	}
	if ids := store.IDs(); !reflect.DeepEqual(ids, []uint64{7, 8, 9}) || store.Len() != 3 {
		t.Fatalf("IDs = %v", ids)
	}
	for _, fixture := range fixtures {
		requireSpilledEqualsDirect(t, store, fixture)
	}
	if _, err := store.Entry(10); err == nil {
		t.Fatal("Entry of an unknown ID returned nil")
	}
}

func TestCaptureSpillStoreRotatesSegments(t *testing.T) {
	dir := t.TempDir()
	store := newCaptureSpillStore(dir)
	store.segmentLimit = 1
	fixtures := spillFixtures()
	for _, fixture := range fixtures {
		if err := store.Append(fixture.entry, fixture.body()); err != nil {
			t.Fatalf("Append %d: %v", fixture.entry.ID, err)
		}
	}
	segments, _ := filepath.Glob(filepath.Join(dir, "spill-*.hbin"))
	if len(segments) != len(fixtures) {
		t.Fatalf("segments = %v, want one per entry", segments)
	}
	for _, fixture := range fixtures {
		requireSpilledEqualsDirect(t, store, fixture)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if segments, _ := filepath.Glob(filepath.Join(dir, "spill-*.hbin")); len(segments) != len(fixtures) {
		t.Fatal("Close removed segments")
	}
	store = newCaptureSpillStore(dir)
	store.segmentLimit = 1
	if err := store.Append(fixtures[0].entry, fixtures[0].body()); err != nil {
		t.Fatal(err)
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if store.Len() != 0 || store.Has(fixtures[0].entry.ID) {
		t.Fatal("Reset kept entries")
	}
}

func TestCaptureSpillStoreDeleteZeroesTheEntry(t *testing.T) {
	dir := t.TempDir()
	store := newCaptureSpillStore(dir)
	t.Cleanup(func() { _ = store.Close() })
	fixtures := spillFixtures()
	for _, fixture := range fixtures {
		if err := store.Append(fixture.entry, fixture.body()); err != nil {
			t.Fatal(err)
		}
	}
	record, segment, err := store.lookup(7)
	if err != nil {
		t.Fatal(err)
	}
	kind, ok := store.Delete(7)
	if !ok || kind != spillKindHTTP {
		t.Fatalf("Delete = %v, %v", kind, ok)
	}
	if _, ok := store.Delete(7); ok {
		t.Fatal("second Delete succeeded")
	}
	data, err := os.ReadFile(segment.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[record.header:record.end], make([]byte, record.end-record.header)) {
		t.Fatal("deleted entry bytes were not zeroed")
	}
	if store.Has(7) || store.Len() != 2 {
		t.Fatal("deleted entry is still indexed")
	}
	requireSpilledEqualsDirect(t, store, fixtures[1])
	requireSpilledEqualsDirect(t, store, fixtures[2])
}

func TestCaptureSpillStoreRawCopyDecodesAtRebasedOffsets(t *testing.T) {
	store := newCaptureSpillStore(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	fixtures := spillFixtures()
	for _, fixture := range fixtures {
		if err := store.Append(fixture.entry, fixture.body()); err != nil {
			t.Fatal(err)
		}
	}
	// Copy every entry after an unrelated prefix, as the history flush does.
	var copied bytes.Buffer
	copied.WriteString("prefix-written-by-the-history-flush")
	type rebased struct {
		id     uint64
		header int64
		body   int64
	}
	var placed []rebased
	for _, id := range store.IDs() {
		header := int64(copied.Len())
		bodyRel, n, err := store.CopyRaw(&copied, id)
		if err != nil {
			t.Fatalf("CopyRaw %d: %v", id, err)
		}
		if n != int64(copied.Len())-header || bodyRel <= 0 || bodyRel >= n {
			t.Fatalf("CopyRaw %d = bodyRel %d n %d", id, bodyRel, n)
		}
		placed = append(placed, rebased{id: id, header: header, body: header + bodyRel})
	}
	reader := bytes.NewReader(copied.Bytes())
	byID := map[uint64]spillFixture{}
	for _, fixture := range fixtures {
		byID[fixture.entry.ID] = fixture
	}
	for _, place := range placed {
		index := make([]byte, 16)
		binary.BigEndian.PutUint32(index[8:], uint32(place.header))
		binary.BigEndian.PutUint32(index[12:], uint32(place.body))
		gotEntry, gotBody := decodeHBINAt(t, reader, index, 0)
		wantEntry, wantBody := directHBIN(t, byID[place.id])
		if !reflect.DeepEqual(gotEntry, wantEntry) || !reflect.DeepEqual(gotBody, wantBody) {
			t.Fatalf("raw copy of %d does not decode at its rebased offsets", place.id)
		}
	}
}
