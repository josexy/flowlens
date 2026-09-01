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
}
