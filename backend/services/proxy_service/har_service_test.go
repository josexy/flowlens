package proxyservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProxyServiceExportHARAllAndSelected(t *testing.T) {
	service := newTestProxyService(t, nil)
	started := time.Date(2026, 8, 12, 17, 56, 1, 341480000, time.Local)
	httpEntry := service.newTrafficEntry(TrafficEntry{
		Type:       "https",
		StartedAt:  started,
		Method:     "GET",
		URL:        "https://ifconfig.co/json",
		Host:       "ifconfig.co",
		Path:       "/json",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HTTPHeaderField{
			{Name: ":method", Value: "GET"},
		}, Metrics: &HTTPMessageMetrics{
			StartedAtMicros: started.UnixMicro(),
			EndedAtMicros:   started.Add(time.Millisecond).UnixMicro(),
			HeaderSize:      14,
			BodySize:        0,
			State:           HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HTTPHeaderField{
			{Name: ":status", Value: "200"},
		}, Metrics: &HTTPMessageMetrics{
			StartedAtMicros: started.Add(700 * time.Millisecond).UnixMicro(),
			EndedAtMicros:   started.Add(736 * time.Millisecond).UnixMicro(),
			HeaderSize:      14,
			BodySize:        0,
			State:           HTTPMessageStateCompleted,
		}},
	})
	service.storeTrafficEntry(httpEntry)
	rawEntry := service.newTrafficEntry(TrafficEntry{Type: "tcp", URL: "tcp://example:443"})
	service.storeTrafficEntry(rawEntry)

	allTarget := filepath.Join(t.TempDir(), "all.har")
	result, err := service.ExportHAR(HARExportRequest{Path: allTarget})
	if err != nil {
		t.Fatalf("ExportHAR(all): %v", err)
	}
	if result.Exported != 1 || result.Skipped != 1 || result.MissingBodies != 0 {
		t.Fatalf("all result = %+v", result)
	}

	selectedTarget := filepath.Join(t.TempDir(), "selected.har")
	result, err = service.ExportHAR(HARExportRequest{
		Path:       selectedTarget,
		TrafficIDs: []uint64{httpEntry.ID, httpEntry.ID},
	})
	if err != nil {
		t.Fatalf("ExportHAR(selected): %v", err)
	}
	if result.Exported != 1 || result.Skipped != 0 {
		t.Fatalf("selected result = %+v", result)
	}
	data, err := os.ReadFile(selectedTarget)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var document struct {
		Log struct {
			Entries []json.RawMessage `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(document.Log.Entries) != 1 {
		t.Fatalf("entry count = %d", len(document.Log.Entries))
	}
}

func TestProxyServiceExportHARKeepsEntryWhenCapturedBodyIsUnavailable(t *testing.T) {
	service := newTestProxyService(t, nil)
	started := time.Date(2026, 8, 12, 18, 0, 0, 0, time.Local)
	entry := service.newTrafficEntry(TrafficEntry{
		Type:       "https",
		StartedAt:  started,
		Method:     "GET",
		URL:        "https://example.test/data",
		Host:       "example.test",
		Path:       "/data",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: started.UnixMicro(),
			EndedAtMicros:   started.Add(time.Millisecond).UnixMicro(),
			HeaderSize:      0,
			BodySize:        0,
			State:           HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Proto: "HTTP/1.1", Metrics: &HTTPMessageMetrics{
			StartedAtMicros: started.Add(2 * time.Millisecond).UnixMicro(),
			EndedAtMicros:   started.Add(3 * time.Millisecond).UnixMicro(),
			HeaderSize:      0,
			BodySize:        5,
			State:           HTTPMessageStateCompleted,
		}},
	})
	service.storeTrafficEntry(entry)

	result, err := service.ExportHAR(HARExportRequest{
		Path: filepath.Join(t.TempDir(), "missing-body.har"),
	})
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}
	if result.Exported != 1 || result.Skipped != 0 || result.MissingBodies != 1 {
		t.Fatalf("result = %+v", result)
	}
}
