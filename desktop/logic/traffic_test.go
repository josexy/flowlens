package logic

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func rawTCPEntry() *TrafficEntry {
	return &TrafficEntry{
		Type: "tcp", Host: "[2001:db8::1]:8443", URL: "tcp://[2001:db8::1]:8443",
		RawTCP: &proxyservice.RawTCPTunnelInfo{Source: "http_connect", HostPort: "[2001:db8::1]:8443", TLS: true},
	}
}

func metrics(started, ended, header, body int64, state proxyservice.HTTPMessageState) *proxyservice.HTTPMessageMetrics {
	return &proxyservice.HTTPMessageMetrics{StartedAtMicros: started, EndedAtMicros: ended, HeaderSize: header, BodySize: body, State: state}
}

func TestTrafficTypeGuards(t *testing.T) {
	if !IsRawTCP(rawTCPEntry()) || !IsWebSocket(&TrafficEntry{Type: "wss"}) || !IsHTTP(&TrafficEntry{Type: "https"}) || IsHTTP(&TrafficEntry{Type: "unknown"}) {
		t.Fatal("type guards")
	}
}

func TestEvictionWatermark(t *testing.T) {
	watermark := AdvanceEvictionWatermark(0)
	if IsTrafficEntryEvicted(2, watermark) || IsTrafficEntryEvicted(1, watermark) {
		t.Fatal("nothing evicted yet")
	}
	watermark = AdvanceEvictionWatermark(0, 1, 2)
	if watermark != 2 || !IsTrafficEntryEvicted(1, watermark) || !IsTrafficEntryEvicted(2, watermark) || IsTrafficEntryEvicted(3, watermark) || AdvanceEvictionWatermark(watermark, 1) != watermark {
		t.Fatalf("watermark %d", watermark)
	}
}

func TestParseHostPort(t *testing.T) {
	for input, want := range map[string]HostPort{
		"example.com:443":    {"example.com", "443"},
		"127.0.0.1:8080":     {"127.0.0.1", "8080"},
		"[2001:db8::1]:8443": {"2001:db8::1", "8443"},
		"2001:db8::1":        {"2001:db8::1", ""},
	} {
		if got := ParseHostPort(input); got != want {
			t.Errorf("%q: got %+v", input, got)
		}
	}
}

func TestRawTCPDisplayHelpers(t *testing.T) {
	entry := rawTCPEntry()
	entry.Metadata = &proxyservice.Metadata{TLS: &proxyservice.TLSState{SelectedALPN: "mqtt"}}
	if TrafficTarget(entry) != "[2001:db8::1]:8443" || TrafficCategoryHost(entry) != "2001:db8::1" || TrafficTargetPort(entry) != "8443" || TrafficProtocol(entry) != "mqtt" || TrafficTypeLabel(entry) != "TCP/TLS" {
		t.Fatal("display helpers")
	}
}

func TestRawTCPSearch(t *testing.T) {
	entry := rawTCPEntry()
	for query, want := range map[string]bool{"2001:DB8": true, "8443": true, "HTTP_CONNECT": true, "websocket": false} {
		if got := TrafficMatchesSearch(entry, query); got != want {
			t.Errorf("%q: got %v", query, got)
		}
	}
}

func TestCapabilitiesGateRawTCP(t *testing.T) {
	if CapabilitiesOf(rawTCPEntry()) != (TrafficCapabilities{}) {
		t.Fatal("raw TCP supports nothing")
	}
	if !CapabilitiesOf(&TrafficEntry{Type: "http"}).CanResend || !CapabilitiesOf(&TrafficEntry{Type: "ws"}).CanEditRequest || CapabilitiesOf(&TrafficEntry{Type: "ws"}).CanResend {
		t.Fatal("capabilities")
	}
}

func TestTableMetrics(t *testing.T) {
	entry := &TrafficEntry{
		Type:     "https",
		Request:  &proxyservice.HTTPMessage{Metrics: metrics(1_000_000, 1_000_100, 107, 0, "")},
		Response: &proxyservice.HTTPMessage{Metrics: metrics(1_000_200, 1_000_584, 531, 425, "")},
	}
	if got, ok := TrafficTotalDurationMicros(entry); !ok || got != 584 {
		t.Fatalf("duration %d %v", got, ok)
	}
	if got, ok := TrafficTotalSizeBytes(entry); !ok || got != 1063 {
		t.Fatalf("size %d %v", got, ok)
	}
	pending := &TrafficEntry{
		Type:     "https",
		Request:  &proxyservice.HTTPMessage{Metrics: metrics(1, 2, 10, 0, "")},
		Response: &proxyservice.HTTPMessage{Metrics: metrics(3, -1, -1, -1, "")},
	}
	if _, ok := TrafficTotalDurationMicros(pending); ok {
		t.Fatal("pending duration")
	}
	if _, ok := TrafficTotalSizeBytes(pending); ok {
		t.Fatal("pending size")
	}
	if _, ok := TrafficTotalDurationMicros(&TrafficEntry{Type: "tcp"}); ok {
		t.Fatal("raw TCP duration")
	}
}

func TestProcessCategoryIdentity(t *testing.T) {
	process := func(p proxyservice.ProcessInfo) *TrafficEntry {
		p.Status = proxyservice.ProcessStatusResolved
		return &TrafficEntry{Type: "https", Metadata: &proxyservice.Metadata{Process: &p}}
	}
	if got := TrafficProcessCategory(process(proxyservice.ProcessInfo{AppID: "com.x", DisplayName: "B", PID: 1})).Key; got != "app:com.x" {
		t.Fatal(got)
	}
	if got := TrafficProcessCategory(process(proxyservice.ProcessInfo{ExecutablePath: "/x", ProcessName: "b"})).Key; got != "exe:/x" {
		t.Fatal(got)
	}
	if got := TrafficProcessCategory(process(proxyservice.ProcessInfo{ProcessName: " b "})); got.Key != "name:b" || got.Label != "b" {
		t.Fatalf("%+v", got)
	}
	if got := TrafficProcessCategory(process(proxyservice.ProcessInfo{})).Key; got != ProcessCategoryUnavailableKey {
		t.Fatal(got)
	}
	if got := TrafficProcessCategory(&TrafficEntry{Type: "https"}).Key; got != ProcessCategoryUnavailableKey {
		t.Fatal(got)
	}
}

func TestCategoryFiltersAndAcrossFacets(t *testing.T) {
	entry := &TrafficEntry{Type: "https", Host: "a.test"}
	if !TrafficMatchesCategoryFilters(entry, nil, nil) || !TrafficMatchesCategoryFilters(entry, map[string]bool{"a.test": true}, nil) || TrafficMatchesCategoryFilters(entry, map[string]bool{"b.test": true}, nil) {
		t.Fatal("hosts")
	}
	if !TrafficMatchesCategoryFilters(entry, map[string]bool{"a.test": true}, map[string]bool{ProcessCategoryUnavailableKey: true}) {
		t.Fatal("unavailable process")
	}
	if TrafficMatchesCategoryFilters(entry, map[string]bool{"a.test": true}, map[string]bool{"app:x": true}) {
		t.Fatal("AND across facets")
	}
}

func patchedEntry() *TrafficEntry {
	return &TrafficEntry{
		ID: 42, Revision: 1, Type: "https", StatusCode: 200, Status: "200 OK",
		Metadata: &proxyservice.Metadata{},
		Request:  &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HeaderField{{Name: "accept", Value: "*/*"}}, Metrics: metrics(1, -1, 11, -1, proxyservice.HTTPMessageStatePending)},
	}
}

func TestPatchUpdatesSelectedFieldsAndRejectsStaleRevisions(t *testing.T) {
	entry := patchedEntry()
	updated := ApplyTrafficPatch(entry, &TrafficPatch{
		TrafficID: 42, Revision: 2,
		ResponseHeaders: &proxyservice.TrafficResponseHeadersPatch{StatusCode: 204, Status: "204 No Content", Proto: "HTTP/2.0", HeaderFields: []HeaderField{{Name: "content-type", Value: "text/plain"}}},
		Metrics: &proxyservice.TrafficMetricsPatch{
			Request:  metrics(1, 2, 11, 0, proxyservice.HTTPMessageStateCompleted),
			Response: metrics(3, -1, 26, -1, proxyservice.HTTPMessageStatePending),
		},
	})
	if updated.Revision != 2 || updated.StatusCode != 204 || updated.Request.Metrics.State != proxyservice.HTTPMessageStateCompleted || updated.Response.Metrics.StartedAtMicros != 3 {
		t.Fatalf("updated %+v", updated)
	}
	if &updated.Request.HeaderFields[0] != &entry.Request.HeaderFields[0] || updated.Metadata != entry.Metadata {
		t.Fatal("untouched parts should be shared")
	}
	if entry.StatusCode != 200 || entry.Request.Metrics.State != proxyservice.HTTPMessageStatePending {
		t.Fatal("original entry changed")
	}
	if stale := ApplyTrafficPatch(updated, &TrafficPatch{TrafficID: 42, Revision: 1, Process: &proxyservice.ProcessInfo{}}); stale != updated {
		t.Fatal("stale patch applied")
	}
}

func TestStartPatchReplacesUnknownTime(t *testing.T) {
	entry := &TrafficEntry{ID: 1, Revision: 1}
	started := time.Date(2026, 9, 10, 8, 0, 0, 123456000, time.UTC)
	updated := ApplyTrafficPatch(entry, &TrafficPatch{TrafficID: 1, Revision: 2, StartedAt: &started})
	if !updated.StartedAt.Equal(started) || !entry.StartedAt.IsZero() {
		t.Fatal("started at")
	}
	if got := ApplyTrafficPatch(updated, &TrafficPatch{TrafficID: 1, Revision: 3}); !got.StartedAt.Equal(started) {
		t.Fatal("kept")
	}
}

func TestConnectionTimingKeepsProcessMetadata(t *testing.T) {
	process := &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, PID: 10}
	entry := &TrafficEntry{ID: 42, Revision: 1, Metadata: &proxyservice.Metadata{Process: process}}
	timings := &proxyservice.HTTPConnectionTimings{}
	updated := ApplyTrafficPatch(entry, &TrafficPatch{TrafficID: 42, Revision: 2, Metrics: &proxyservice.TrafficMetricsPatch{Connection: timings}})
	if updated.Metadata.ConnectionTimings != timings || updated.Metadata.Process != process || entry.Metadata.ConnectionTimings != nil {
		t.Fatal("metadata")
	}
}

func TestPatchesPreserveUnrelatedData(t *testing.T) {
	headers := []HeaderField{{Name: "content-type", Value: "text/plain"}}
	entry := &TrafficEntry{ID: 7, Revision: 1, Metadata: &proxyservice.Metadata{}, Request: &proxyservice.HTTPMessage{}, Response: &proxyservice.HTTPMessage{HeaderFields: headers}}
	withProcess := ApplyTrafficPatch(entry, &TrafficPatch{TrafficID: 7, Revision: 2, Process: &proxyservice.ProcessInfo{PID: 42}})
	if withProcess.Metadata.Process.PID != 42 || withProcess.Request != entry.Request || withProcess.Response != entry.Response {
		t.Fatal("process patch")
	}
	withTrailers := ApplyTrafficPatch(withProcess, &TrafficPatch{TrafficID: 7, Revision: 3, ResponseTrailers: &proxyservice.TrafficResponseTrailersPatch{TrailerFields: []HeaderField{{Name: "x-trace", Value: "done"}}}})
	if &withTrailers.Response.HeaderFields[0] != &headers[0] || withTrailers.Response.TrailerFields[0].Value != "done" {
		t.Fatal("trailers patch")
	}
	failed := ApplyTrafficPatch(withTrailers, &TrafficPatch{TrafficID: 7, Revision: 4, Error: &proxyservice.TrafficError{Error: "connection reset"}, Metrics: &proxyservice.TrafficMetricsPatch{Response: metrics(1, 2, 26, -1, proxyservice.HTTPMessageStateFailed)}})
	if failed.Error.Error != "connection reset" || failed.Response.Metrics.State != proxyservice.HTTPMessageStateFailed || &failed.Response.HeaderFields[0] != &headers[0] {
		t.Fatal("failure patch")
	}
}

func TestTerminalTransitions(t *testing.T) {
	state := func(s proxyservice.HTTPMessageState) *proxyservice.HTTPMessage {
		return &proxyservice.HTTPMessage{Metrics: &proxyservice.HTTPMessageMetrics{State: s}}
	}
	pending := &TrafficEntry{Request: state(proxyservice.HTTPMessageStatePending), Response: state(proxyservice.HTTPMessageStatePending)}
	terminal := &TrafficEntry{Request: state(proxyservice.HTTPMessageStateCompleted), Response: state(proxyservice.HTTPMessageStateFailed)}
	if got := NewTerminalMessageSides(pending, terminal); !slices.Equal(got, []HTTPMessageSide{SideRequest, SideResponse}) {
		t.Fatal(got)
	}
	if got := NewTerminalMessageSides(terminal, terminal); len(got) != 0 {
		t.Fatal(got)
	}
	canceled := &TrafficEntry{Request: state(proxyservice.HTTPMessageStateCanceled), Response: pending.Response}
	if got := NewTerminalMessageSides(pending, canceled); !slices.Equal(got, []HTTPMessageSide{SideRequest}) {
		t.Fatal(got)
	}
}

func TestTerminalRefreshWaitsForLoadAndMergesDuplicates(t *testing.T) {
	var queue TerminalBodyRefreshQueue
	queue.Activate(42)
	for _, side := range []HTTPMessageSide{SideRequest, SideRequest, SideResponse} {
		if queue.Request(42, []HTTPMessageSide{side}, true) {
			t.Fatal("should wait for the load")
		}
	}
	if !queue.CompleteLoad(42) || queue.CompleteLoad(42) {
		t.Fatal("complete once")
	}
}

func TestTerminalRefreshIsolatesEntries(t *testing.T) {
	var queue TerminalBodyRefreshQueue
	queue.Activate(1)
	queue.Request(1, []HTTPMessageSide{SideRequest}, true)
	queue.Activate(2)
	if queue.CompleteLoad(1) || queue.Request(1, []HTTPMessageSide{SideResponse}, false) || !queue.Request(2, []HTTPMessageSide{SideResponse}, false) {
		t.Fatal("isolation")
	}
}

func tableEntry(id uint64, status int) *TrafficEntry {
	return &TrafficEntry{ID: id, Revision: 1, Type: "http", Method: "GET", Host: "example.test", StatusCode: status}
}

func ids(entries []*TrafficEntry) []uint64 {
	out := make([]uint64, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func TestLatestEdge(t *testing.T) {
	desc := TrafficSort{ColumnID, SortDesc}
	asc := TrafficSort{ColumnID, SortAsc}
	if TrafficLatestEdge(desc) != EdgeStart || TrafficLatestEdge(asc) != EdgeEnd || TrafficLatestEdge(TrafficSort{}) != EdgeEnd ||
		TrafficLatestEdge(TrafficSort{ColumnDuration, SortDesc}) != EdgeUnknown || TrafficLatestEdge(TrafficSort{ColumnHost, SortAsc}) != EdgeUnknown {
		t.Fatal("edges")
	}
	for _, test := range []struct {
		sort               TrafficSort
		first, last, count int
		want               bool
	}{
		{desc, 0, 14, 5000, true}, {desc, 1, 15, 5000, false},
		{asc, 4985, 4999, 5000, true}, {asc, 0, 14, 5000, false}, {asc, 0, 14, 15, true},
		{desc, 0, -1, 0, false}, {TrafficSort{ColumnSize, SortDesc}, 0, 14, 5000, false},
	} {
		if got := IsAtLatestEdge(test.sort, test.first, test.last, test.count); got != test.want {
			t.Errorf("%+v: got %v", test, got)
		}
	}
}

func TestPayloadUpdatesReuseSortOrderFor10000Rows(t *testing.T) {
	sorter := NewTrafficTableSorter()
	entries := make([]*TrafficEntry, 10000)
	for i := range entries {
		entries[i] = tableEntry(uint64(i+1), i%3+200)
	}
	sort := TrafficSort{ColumnStatusCode, SortAsc}
	first := sorter.Sort(entries, sort)
	if sorter.computed != 10000 {
		t.Fatalf("computed %d", sorter.computed)
	}
	next := slices.Clone(entries)
	replacement := tableEntry(2, 201)
	replacement.Status = "updated content"
	next[1] = replacement
	second := sorter.Sort(next, sort)
	if sorter.computed != 10001 {
		t.Fatalf("unchanged entries must not recompute sort values: %d", sorter.computed)
	}
	if !slices.Equal(ids(first), ids(second)) {
		t.Fatal("order changed")
	}
	shown := slices.IndexFunc(second, func(e *TrafficEntry) bool { return e.ID == 2 })
	if second[shown] != replacement {
		t.Fatal("cached order must show new content")
	}
}

func TestSortingHandlesChangesTiesAndColumns(t *testing.T) {
	sorter := NewTrafficTableSorter()
	entries := []*TrafficEntry{tableEntry(3, 200), tableEntry(2, 500), tableEntry(1, 200)}
	if got := ids(sorter.Sort(entries, TrafficSort{ColumnStatusCode, SortAsc})); !slices.Equal(got, []uint64{1, 3, 2}) {
		t.Fatal(got)
	}
	if got := ids(sorter.Sort(entries, TrafficSort{ColumnStatusCode, SortDesc})); !slices.Equal(got, []uint64{2, 1, 3}) {
		t.Fatal(got)
	}
	updated := []*TrafficEntry{entries[0], tableEntry(2, 100), tableEntry(4, 404)}
	if got := ids(sorter.Sort(updated, TrafficSort{ColumnStatusCode, SortAsc})); !slices.Equal(got, []uint64{2, 3, 4}) {
		t.Fatal(got)
	}
	if got := ids(sorter.Sort(updated, TrafficSort{ColumnID, SortDesc})); !slices.Equal(got, []uint64{4, 3, 2}) {
		t.Fatal(got)
	}
	if got := sorter.Sort(updated, TrafficSort{}); !reflect.DeepEqual(got, updated) {
		t.Fatal("unsorted")
	}
}

func TestIncompleteMetricsStayLast(t *testing.T) {
	sorter := NewTrafficTableSorter()
	complete := tableEntry(2, 0)
	complete.Request = &proxyservice.HTTPMessage{Metrics: metrics(100, 110, 0, 0, proxyservice.HTTPMessageStateCompleted)}
	complete.Response = &proxyservice.HTTPMessage{Metrics: metrics(100, 120, 0, 0, proxyservice.HTTPMessageStateCompleted)}
	entries := []*TrafficEntry{tableEntry(1, 0), complete, tableEntry(3, 0)}
	for _, order := range []SortOrder{SortAsc, SortDesc} {
		if got := ids(sorter.Sort(entries, TrafficSort{ColumnDuration, order})); !slices.Equal(got, []uint64{2, 1, 3}) {
			t.Fatalf("%s: %v", order, got)
		}
	}
}

func TestSortCycle(t *testing.T) {
	if NextSortOrder(SortNone) != SortAsc || NextSortOrder(SortAsc) != SortDesc || NextSortOrder(SortDesc) != SortNone {
		t.Fatal("cycle")
	}
}

func TestSortedTextIgnoresCase(t *testing.T) {
	sorter := NewTrafficTableSorter()
	a, b, c := tableEntry(1, 0), tableEntry(2, 0), tableEntry(3, 0)
	a.Host, b.Host, c.Host = "b.test", "A.test", "c.test"
	got := sorter.Sort([]*TrafficEntry{a, b, c}, TrafficSort{ColumnHost, SortAsc})
	if !strings.EqualFold(got[0].Host, "a.test") {
		t.Fatalf("first %q", got[0].Host)
	}
}

func withContentType(entry *TrafficEntry, value string) *TrafficEntry {
	entry.Response = &proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HeaderField{{Name: "content-type", Value: value}}}
	return entry
}

func TestFilterTabs(t *testing.T) {
	json := withContentType(tableEntry(1, 200), "application/json; charset=utf-8")
	sse := withContentType(tableEntry(2, 200), "text/event-stream; x=1")
	for tab, want := range map[string]bool{
		TabAll: true, TabJSON: true, TabXML: false, TabHTTP: true, TabHTTPS: false, TabHTTP2: true, TabHTTP1: false,
		Tab2xx: true, Tab5xx: false, TabSSE: false,
	} {
		if got := TrafficMatchesTab(json, tab); got != want {
			t.Errorf("%s: got %v", tab, got)
		}
	}
	if !TrafficMatchesTab(sse, TabSSE) {
		t.Fatal("sse")
	}
	if TrafficMatchesTab(tableEntry(3, 0), Tab2xx) {
		t.Fatal("pending entries have no status class")
	}
	if !TrafficMatchesTab(&TrafficEntry{Type: "wss"}, TabWebSocket) || !TrafficMatchesTab(rawTCPEntry(), TabTCP) {
		t.Fatal("protocol tabs")
	}
}

func TestColumnOrderHelpers(t *testing.T) {
	full := []string{"id", "method", "host", "path", "process"}
	got := MergeVisibleColumnOrder(full, []string{"method", "id", "host", "path"})
	if !slices.Equal(got, []string{"method", "id", "host", "path", "process"}) {
		t.Fatal(got)
	}
	// The hidden process column keeps its slot.
	got = MergeVisibleColumnOrder([]string{"id", "process", "method"}, []string{"method", "id"})
	if !slices.Equal(got, []string{"method", "process", "id"}) {
		t.Fatal(got)
	}
	complete := CompleteColumnOrder([]string{"size", "bogus", "id", "size"})
	if len(complete) != len(TrafficColumnKeys) || complete[0] != "size" || complete[1] != "id" || complete[2] != "method" {
		t.Fatal(complete)
	}
}

func TestBuildCurl(t *testing.T) {
	entry := &TrafficEntry{Method: "POST", URL: "https://x.test/a'b", Request: &proxyservice.HTTPMessage{HeaderFields: []HeaderField{{Name: ":authority", Value: "x"}, {Name: "A", Value: "b"}}}}
	want := "curl -X POST 'https://x.test/a'\\''b' \\\n  -H 'A: b' \\\n  --data-raw 'hi'"
	if got := BuildCurl(entry, "hi", ""); got != want {
		t.Fatalf("got %q", got)
	}
	if got := BuildCurl(entry, "aGk=", "base64"); !strings.HasPrefix(got, "printf %s 'aGk=' | base64 -d | curl") || !strings.HasSuffix(got, "--data-binary @-") {
		t.Fatalf("got %q", got)
	}
	if got := BuildCurl(&TrafficEntry{Method: "GET", URL: "u"}, "", ""); got != "curl 'u'" {
		t.Fatalf("got %q", got)
	}
}

func TestCopyValues(t *testing.T) {
	entry := rawTCPEntry()
	entry.Metadata = &proxyservice.Metadata{RemoteDestinationAddr: "1.2.3.4:443", LocalSourceAddr: "[::1]:9"}
	for field, want := range map[CopyField]string{
		CopyTarget: "[2001:db8::1]:8443", CopyServerIP: "1.2.3.4", CopyClientIP: "::1", CopyServerAddr: "1.2.3.4:443", CopyClientAddr: "[::1]:9",
	} {
		if got := TrafficCopyValue(entry, field); got != want {
			t.Errorf("%s: got %q", field, got)
		}
	}
}
