package historyservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportHARStreamsV1(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryWithVersion(t, historyDir, "export-v1", 1)

	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}

	target := filepath.Join(t.TempDir(), "selected.har")
	result, err := service.ExportHAR(HARExportRequest{
		Key:        "export-v1",
		Path:       target,
		TrafficIDs: []uint64{9001, 9001},
	})
	if err != nil {
		t.Fatalf("ExportHAR(v1): %v", err)
	}
	if result.Exported != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var document struct {
		Log struct {
			Creator struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"creator"`
			Entries []json.RawMessage `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal HAR: %v", err)
	}
	if document.Log.Creator.Name != "FlowLens" || document.Log.Creator.Version == "" || len(document.Log.Entries) != 1 {
		t.Fatalf("HAR document = %+v", document.Log)
	}
	var entry struct {
		StartedDateTime string  `json:"startedDateTime"`
		Time            float64 `json:"time"`
		Timings         struct {
			Send    float64 `json:"send"`
			Wait    float64 `json:"wait"`
			Receive float64 `json:"receive"`
		} `json:"timings"`
		Request struct {
			HeadersSize int64 `json:"headersSize"`
		} `json:"request"`
	}
	if err := json.Unmarshal(document.Log.Entries[0], &entry); err != nil {
		t.Fatal(err)
	}
	// The independently encoded HBIN v1 fixture has a completed HTTP/2 request
	// and no response. HAR must retain precision without inventing completion.
	if entry.StartedDateTime != "2026-08-12T13:16:01.341480Z" || entry.Time != -1 ||
		entry.Timings.Send != .584 || entry.Timings.Wait != -1 || entry.Timings.Receive != -1 ||
		entry.Request.HeadersSize != 48 {
		t.Fatalf("history HAR mapping = %+v", entry)
	}
}
