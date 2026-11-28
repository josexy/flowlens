package proxyservice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
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

// newSpillTestService returns a service with a real body-cache session and
// spill store.
func newSpillTestService(t *testing.T) *ProxyService {
	t.Helper()
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	// A running capture gets a new body-cache session, with its spill store,
	// when it restarts.
	svc.running = true
	svc.ensureBodyCacheLocked()
	if svc.captureSpillStore() == nil {
		t.Fatal("spill store was not created with the body cache")
	}
	t.Cleanup(func() {
		svc.bodyCacheMu.Lock()
		svc.closeCaptureSpillNoLock()
		svc.bodyCacheMu.Unlock()
	})
	return svc
}

// storeSettledEntry stores a finished exchange and reports whether the
// current capture accepted it.
func storeSettledEntry(svc *ProxyService, i int) (uint64, bool) {
	entry := svc.newTrafficEntry(TrafficEntry{
		Type: "https", Method: http.MethodGet, URL: fmt.Sprintf("https://filler.test/%d", i), Host: "filler.test", Path: fmt.Sprintf("/%d", i),
		StatusCode: 204, Status: "204 No Content",
		Request:  &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: 0, State: HTTPMessageStateCompleted}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: 0, State: HTTPMessageStateCompleted}},
	})
	return entry.ID, svc.storeTrafficEntry(entry)
}

func addSettledEntries(t *testing.T, svc *ProxyService, count int) []uint64 {
	t.Helper()
	ids := make([]uint64, 0, count)
	for i := range count {
		id, stored := storeSettledEntry(svc, i)
		if !stored {
			t.Fatalf("entry %d was rejected", id)
		}
		ids = append(ids, id)
	}
	return ids
}

func spillAll(svc *ProxyService) {
	for svc.spillCaptureBatch() {
	}
}

func requireConsistentCapture(t *testing.T, svc *ProxyService) {
	t.Helper()
	store := svc.captureSpillStore()
	for _, id := range store.IDs() {
		if svc.trafficEntries.Contains(id) {
			t.Fatalf("entry %d is both in memory and spilled", id)
		}
		if _, ok := svc.trafficBodies.Load(id); ok {
			t.Fatalf("spilled entry %d kept its bodies in memory", id)
		}
		if _, ok := svc.trafficWsMsgs.Load(id); ok {
			t.Fatalf("spilled entry %d kept its WebSocket messages in memory", id)
		}
		if _, err := store.Entry(id); err != nil {
			t.Fatalf("spilled entry %d cannot be read: %v", id, err)
		}
	}
	stats := svc.GetStatistics()
	if total := int64(svc.trafficEntries.Len() + store.Len()); stats.Total != total {
		t.Fatalf("statistics total %d, entries in memory and spilled %d", stats.Total, total)
	}
	if stats.TotalHTTP+stats.TotalWS+stats.TotalTCP != stats.Total {
		t.Fatalf("statistics by kind do not add up: %+v", stats)
	}
}

func TestSpillerKeepsTheWindowAndUnfinishedEntries(t *testing.T) {
	svc := newSpillTestService(t)
	ws, releaseWS := svc.registerHeldTrafficEntry(newRawTCPMetadataContext(), TrafficEntry{Type: "ws", Method: http.MethodGet, URL: "ws://held.test/"})
	addSettledEntries(t, svc, 20000)
	spillAll(svc)

	if got := svc.trafficEntries.Len(); got != captureMemoryWindow+1 {
		t.Fatalf("in-memory entries = %d, want the %d newest plus the open WebSocket", got, captureMemoryWindow)
	}
	if !svc.trafficEntries.Contains(ws.ID) {
		t.Fatal("an unfinished WebSocket entry older than the window was spilled")
	}
	if got := svc.captureSpillStore().Len(); got != 20001-captureMemoryWindow-1 {
		t.Fatalf("spilled entries = %d", got)
	}
	if stats := svc.GetStatistics(); stats.Total != 20001 || stats.TotalWS != 1 {
		t.Fatalf("statistics = %+v, want every entry of the session", stats)
	}
	if tail := svc.GetTraffic(); len(tail) != captureMemoryWindow || tail[len(tail)-1].ID != 20001 {
		t.Fatalf("GetTraffic returned %d entries", len(tail))
	}
	requireConsistentCapture(t, svc)

	releaseWS()
	spillAll(svc)
	if svc.trafficEntries.Contains(ws.ID) || !svc.captureSpillStore().Has(ws.ID) {
		t.Fatal("the WebSocket entry was not spilled once its connection closed")
	}
	if got := svc.trafficEntries.Len(); got != captureMemoryWindow {
		t.Fatalf("in-memory entries = %d, want %d", got, captureMemoryWindow)
	}
	requireConsistentCapture(t, svc)

	// A late update of a spilled entry must not bring it back into memory.
	ws.StatusCode = 500
	if svc.storeTrafficEntry(ws) || svc.trafficEntries.Contains(ws.ID) {
		t.Fatal("a late update resurrected a spilled entry")
	}
}

func TestSpillerGoroutineBoundsTheWindow(t *testing.T) {
	svc := newSpillTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	svc.startCaptureSpiller(ctx)
	t.Cleanup(func() {
		cancel()
		svc.waitCaptureSpiller()
	})
	addSettledEntries(t, svc, 8000)
	deadline := time.Now().Add(5 * time.Second)
	for svc.trafficEntries.Len() > captureMemoryWindow+captureSpillBatch {
		if time.Now().After(deadline) {
			t.Fatalf("spiller left %d entries in memory", svc.trafficEntries.Len())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	svc.waitCaptureSpiller()
	requireConsistentCapture(t, svc)
}

func TestSpillerRacesWithClearRestartAndDelete(t *testing.T) {
	svc := newSpillTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	svc.startCaptureSpiller(ctx)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				// A clear or restart rejects entries created before it, as
				// for a late exchange of the previous session.
				for i := range 300 {
					storeSettledEntry(svc, i)
				}
			}
		}
	})
	wg.Go(func() {
		random := rand.New(rand.NewPCG(1, 2))
		for {
			select {
			case <-stop:
				return
			default:
			}
			if last := svc.trafficEntries.Back(); last != nil {
				id := random.Uint64N(last.Key()) + 1
				_, _ = svc.GetTrafficBodyView(id)
				svc.DeleteTraffic([]uint64{id, id + 1})
			}
			time.Sleep(100 * time.Microsecond)
		}
	})
	for round := range 6 {
		time.Sleep(40 * time.Millisecond)
		var err error
		if round%2 == 0 {
			err = svc.ClearTraffic()
		} else {
			err = svc.RestartCapture(false)
		}
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	close(stop)
	wg.Wait()
	cancel()
	svc.waitCaptureSpiller()
	spillAll(svc)
	requireConsistentCapture(t, svc)
}

// captureSpillCandidates captures an HTTP exchange with bodies and a WebSocket
// connection through the interceptors.
func captureSpillCandidates(t *testing.T, svc *ProxyService) (httpID, wsID uint64) {
	t.Helper()
	resp, err := interceptPost(t, svc, consumingInvoker(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, strings.Repeat("response line\n", 2048)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	httpID, lifecycle := newestLifecycle(t, svc)
	requireSettled(t, lifecycle)
	wsID = captureWebSocketSession(t, svc,
		newTestCaptureFrame(mitmproxy.Send, 1, []byte("ping")),
		newTestCaptureFrame(mitmproxy.Receive, 2, []byte(strings.Repeat("\x01\x02", 16<<10))),
	)
	return httpID, wsID
}

type spillActionResults struct {
	view      *TrafficBodyView
	recovered RequestBodyRecoveryResult
	resend    *TrafficEntry
	resendRaw []byte
	har       []byte
	csv       []byte
}

func spillActions(t *testing.T, svc *ProxyService, id uint64, dir string) spillActionResults {
	t.Helper()
	var result spillActionResults
	var err error
	if result.view, err = svc.GetTrafficBodyView(id); err != nil {
		t.Fatalf("GetTrafficBodyView %d: %v", id, err)
	}
	if result.resend, result.resendRaw, err = svc.resendRequestSource(id); err != nil {
		t.Fatalf("resendRequestSource %d: %v", id, err)
	}
	if result.resend.Type == "https" || result.resend.Type == "http" {
		if result.recovered, err = svc.RecoverRequestBodyForEditing(result.resend.URL, result.resend.Request.HeaderFields, *result.view); err != nil {
			t.Fatalf("RecoverRequestBodyForEditing %d: %v", id, err)
		}
	}
	harPath := filepath.Join(dir, fmt.Sprintf("%d.har", id))
	if _, err := svc.ExportHAR(HARExportRequest{Path: harPath, TrafficIDs: []uint64{id}}); err != nil {
		t.Fatalf("ExportHAR %d: %v", id, err)
	}
	csvPath := filepath.Join(dir, fmt.Sprintf("%d.csv", id))
	if _, err := svc.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportCSV, Path: csvPath, TargetType: "file", TrafficIDs: []uint64{id}}); err != nil {
		t.Fatalf("ExportTraffic %d: %v", id, err)
	}
	if result.har, err = os.ReadFile(harPath); err != nil {
		t.Fatal(err)
	}
	if result.csv, err = os.ReadFile(csvPath); err != nil {
		t.Fatal(err)
	}
	return result
}

// encodedEntryHeader is an entry's persisted form. Entries compare equal by it
// even when their timestamps carry different locations.
func encodedEntryHeader(t *testing.T, entry *TrafficEntry) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := hbinWriteEntryHeader(&encoded, entry); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestSpilledEntryActionsMatchTheEntryBeforeSpilling(t *testing.T) {
	svc := newSpillTestService(t)
	httpID, wsID := captureSpillCandidates(t, svc)
	before := map[uint64]spillActionResults{
		httpID: spillActions(t, svc, httpID, t.TempDir()),
		wsID:   spillActions(t, svc, wsID, t.TempDir()),
	}
	addSettledEntries(t, svc, captureMemoryWindow+10)
	spillAll(svc)
	for _, id := range []uint64{httpID, wsID} {
		if svc.trafficEntries.Contains(id) || !svc.captureSpillStore().Has(id) {
			t.Fatalf("entry %d was not spilled", id)
		}
		after := spillActions(t, svc, id, t.TempDir())
		want := before[id]
		if !reflect.DeepEqual(after.view, want.view) {
			t.Fatalf("entry %d body view changed after spilling", id)
		}
		if !bytes.Equal(encodedEntryHeader(t, after.resend), encodedEntryHeader(t, want.resend)) || string(after.resendRaw) != string(want.resendRaw) {
			t.Fatalf("entry %d resend source changed after spilling", id)
		}
		if !reflect.DeepEqual(after.recovered, want.recovered) {
			t.Fatalf("entry %d edit recovery changed after spilling", id)
		}
		if string(after.har) != string(want.har) {
			t.Fatalf("entry %d HAR export changed after spilling:\n%s\n---\n%s", id, after.har, want.har)
		}
		if string(after.csv) != string(want.csv) {
			t.Fatalf("entry %d CSV export changed after spilling:\n%s\n---\n%s", id, after.csv, want.csv)
		}
	}
	if len(before[wsID].view.WebSocketMessages) != 2 || before[httpID].view.ResponseBody == "" {
		t.Fatal("fixtures did not capture bodies and messages")
	}

	stats := svc.GetStatistics()
	svc.DeleteTraffic([]uint64{httpID, wsID})
	if svc.captureSpillStore().Has(httpID) || svc.captureSpillStore().Has(wsID) {
		t.Fatal("deleted spilled entries are still stored")
	}
	if after := svc.GetStatistics(); after.Total != stats.Total-2 || after.TotalHTTP != stats.TotalHTTP-1 || after.TotalWS != stats.TotalWS-1 {
		t.Fatalf("statistics after deleting spilled entries = %+v, before %+v", after, stats)
	}
	if _, err := svc.GetTrafficBodyView(httpID); err == nil {
		t.Fatal("a deleted spilled entry is still readable")
	}
	requireConsistentCapture(t, svc)
}

func TestExportOfAPartlySpilledCaptureListsEveryEntryInOrder(t *testing.T) {
	svc := newSpillTestService(t)
	addSettledEntries(t, svc, captureMemoryWindow+700)
	spillAll(svc)
	if svc.captureSpillStore().Len() == 0 {
		t.Fatal("nothing was spilled")
	}
	path := filepath.Join(t.TempDir(), "all.csv")
	if _, err := svc.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportCSV, Path: path, TargetType: "file"}); err != nil {
		t.Fatalf("ExportTraffic: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rows = rows[1:]
	if len(rows) != captureMemoryWindow+700 {
		t.Fatalf("exported %d rows, want %d", len(rows), captureMemoryWindow+700)
	}
	for i, row := range rows {
		if id, _ := strconv.Atoi(row[0]); id != i+1 {
			t.Fatalf("row %d has ID %s", i, row[0])
		}
	}

	harPath := filepath.Join(t.TempDir(), "all.har")
	result, err := svc.ExportHAR(HARExportRequest{Path: harPath})
	if err != nil || result.Exported != captureMemoryWindow+700 {
		t.Fatalf("ExportHAR = %+v, %v", result, err)
	}
}

func TestSaveDuringALargeDownloadStreamsTheBody(t *testing.T) {
	svc := newSpillTestService(t)
	svc.bodyCacheMu.RLock()
	cache := svc.bodyCache
	svc.bodyCacheMu.RUnlock()
	entry := svc.newTrafficEntry(TrafficEntry{
		Type: "https", Method: http.MethodGet, URL: "https://download.test/large", Host: "download.test", Path: "/large", StatusCode: 200,
		Request:  &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: 0, State: HTTPMessageStateCompleted}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "application/octet-stream"}}},
	})
	svc.storeTrafficEntry(entry)
	response := newCapturedBody(entry.ID, "resp", 1024, cache)
	t.Cleanup(response.Abort)
	bodies := &TrafficBodies{liveState: 1, responseBody: response}
	svc.trafficBodies.Store(entry.ID, bodies)
	const received = 256 << 20
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 64<<10)
	for written := 0; written < received; written += len(chunk) {
		bodies.lockRespBody.Lock()
		response.Write(chunk)
		bodies.lockRespBody.Unlock()
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("save: %v", err)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > received/4 {
		t.Fatalf("saving a %d-byte download in progress allocated %d bytes", received, allocated)
	}
	saved := readFlushedHistoryBodies(t, svc)[entry.ID]
	if len(saved.ResponseBody) != received || saved.ResponseBody[:16] != "0123456789abcdef" {
		t.Fatalf("saved %d response bytes, want the %d received so far", len(saved.ResponseBody), received)
	}

	// The download keeps streaming after the save.
	bodies.lockRespBody.Lock()
	response.Write([]byte("tail"))
	bodies.lockRespBody.Unlock()
	view, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil || len(view.ResponseBody) != received+4 {
		t.Fatalf("body after the save = %d bytes, %v", len(view.ResponseBody), err)
	}
}

func TestGetTrafficReturnsTheLiveWindowWithoutTruncatingHistory(t *testing.T) {
	svc := newTestProxyService(t, nil)
	for id := uint64(1); id <= liveTrafficWindow+2; id++ {
		svc.trafficEntries.Set(id, &TrafficEntry{ID: id, Type: "http"})
	}

	window := svc.GetTraffic()
	if len(window) != liveTrafficWindow {
		t.Fatalf("GetTraffic length = %d, want %d", len(window), liveTrafficWindow)
	}
	if window[0].ID != 3 || window[len(window)-1].ID != liveTrafficWindow+2 {
		t.Fatalf("GetTraffic IDs = %d..%d, want 3..%d", window[0].ID, window[len(window)-1].ID, liveTrafficWindow+2)
	}

	all := svc.getAllTraffic()
	if len(all) != liveTrafficWindow+2 {
		t.Fatalf("complete history snapshot length = %d, want %d", len(all), liveTrafficWindow+2)
	}
}
