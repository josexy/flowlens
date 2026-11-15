package proxyservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newHARTimingTestEntry(id uint64, start int64, timings *HTTPConnectionTimings) *TrafficEntry {
	return &TrafficEntry{
		ID: id, Type: "https", Method: "GET", URL: fmt.Sprintf("https://example.test/%d", id),
		StartedAt: time.UnixMicro(start),
		Metadata: &Metadata{
			LocalSourceAddr: "127.0.0.1:12345", LocalDestinationAddr: "127.0.0.1:8080",
			LocalConnectionEstablishedAt: time.UnixMicro(50), ConnectionTimings: timings,
		},
		Request: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			StartedAtMicros: start, EndedAtMicros: start + 1000, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			StartedAtMicros: start + 2000, EndedAtMicros: start + 3000, BodySize: 0, State: HTTPMessageStateCompleted,
		}},
	}
}

func assertHARConnectionOwner(t *testing.T, data []byte) {
	t.Helper()
	var document harEnvelope
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, entry := range document.Log.Entries {
		if entry.Timings.DNS == nil || entry.Timings.Connect == nil || entry.Timings.SSL == nil {
			t.Fatal("connection timing fields missing")
		}
		if entry.Request.URL == "https://example.test/1" {
			if *entry.Timings.DNS != .1 || *entry.Timings.Connect != .4 || *entry.Timings.SSL != .2 || entry.Time != 3.9 {
				t.Fatalf("first connection timings = %+v, time = %g", entry.Timings, entry.Time)
			}
		} else if *entry.Timings.DNS != -1 || *entry.Timings.Connect != -1 || *entry.Timings.SSL != -1 || entry.Time != 3 {
			t.Fatalf("reused connection charged again: %+v, time = %g", entry.Timings, entry.Time)
		}
	}
}

func TestHARConnectionOwnershipDoesNotDependOnExportOrder(t *testing.T) {
	timings := &HTTPConnectionTimings{100, 200, 200, 400, 400, 600}
	first := HARExportEntry{Entry: newHARTimingTestEntry(1, 1000, timings)}
	second := HARExportEntry{Entry: newHARTimingTestEntry(2, 5000, timings)}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			inputs := []HARExportEntry{first, second}
			if reverse {
				inputs = []HARExportEntry{second, first}
			}
			var output bytes.Buffer
			if _, err := WriteHAR(&output, "test", inputs); err != nil {
				t.Fatal(err)
			}
			assertHARConnectionOwner(t, output.Bytes())
			target := filepath.Join(t.TempDir(), "connection.har")
			if _, err := WriteHARFile(target, "test", inputs); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			assertHARConnectionOwner(t, data)
			if !bytes.Equal(output.Bytes(), data) {
				t.Fatal("file and memory exports differ")
			}
			var document harEnvelope
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			if len(document.Log.Entries) != 2 || document.Log.Entries[0].Request.URL != inputs[0].Entry.URL {
				t.Fatal("export reordered the selected entries")
			}
		})
	}
}

func TestProxyHARSelectionKeepsUnselectedConnectionOwner(t *testing.T) {
	service := newTestProxyService(t, nil)
	timings := &HTTPConnectionTimings{100, 200, 200, 400, 400, 600}
	first := service.newTrafficEntry(*newHARTimingTestEntry(1, 1000, timings))
	second := service.newTrafficEntry(*newHARTimingTestEntry(2, 5000, timings))
	service.storeTrafficEntry(second)
	service.storeTrafficEntry(first)
	for _, ids := range [][]uint64{nil, {second.ID}, {second.ID, first.ID}} {
		target := filepath.Join(t.TempDir(), "selected.har")
		result, err := service.ExportHAR(HARExportRequest{Path: target, TrafficIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		want := len(ids)
		if want == 0 {
			want = 2
		}
		if result.Exported != want {
			t.Fatalf("exported=%d want=%d", result.Exported, want)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		assertHARConnectionOwner(t, data)
	}
}
