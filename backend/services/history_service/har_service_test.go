package historyservice

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func TestHistoryHARSelectionKeepsUnselectedConnectionOwner(t *testing.T) {
	storage := setupHistoryTestStorage(t)
	const key = "reused-v2"
	writeHistoryWithVersion(t, storage, key, 2)
	binPath, indexPath := filepath.Join(storage, key+".hbin"), filepath.Join(storage, key+".hidx")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	headerOffset := binary.BigEndian.Uint32(index[12:16])
	bodyOffset := binary.BigEndian.Uint32(index[16:20])
	// Append a second independently indexed entry on the same connection, with
	// later HTTP metrics but identical connection setup timestamps.
	header := append([]byte(nil), data[headerOffset:bodyOffset]...)
	body := append([]byte(nil), data[bodyOffset:]...)
	binary.BigEndian.PutUint64(header[:8], 9002)
	for _, stamp := range []uint64{1_786_540_561_341_480, 1_786_540_561_342_064} {
		original := binary.BigEndian.AppendUint64(nil, stamp)
		if bytes.Count(header, original) != 1 {
			t.Fatal("fixture HTTP timestamp is not unique")
		}
		header = bytes.Replace(header, original, binary.BigEndian.AppendUint64(nil, stamp+5000), 1)
	}
	binary.BigEndian.PutUint32(data[headerOffset-4:headerOffset], 2)
	secondHeader := uint32(len(data))
	data = append(data, header...)
	secondBody := uint32(len(data))
	data = append(data, body...)
	binary.BigEndian.PutUint32(index[:4], 2)
	index = binary.BigEndian.AppendUint64(index, 9002)
	index = binary.BigEndian.AppendUint32(index, secondHeader)
	index = binary.BigEndian.AppendUint32(index, secondBody)
	if err := os.WriteFile(binPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, index, 0600); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, nil)
	if err := svc.initializeHistoryIndexMap(); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]uint64{{9002}, {9002, 9001}} {
		target := filepath.Join(t.TempDir(), "selection.har")
		if _, err := svc.ExportHAR(HARExportRequest{Key: key, Path: target, TrafficIDs: ids}); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Log struct {
				Entries []struct {
					Timings struct {
						DNS     float64 `json:"dns"`
						Connect float64 `json:"connect"`
					} `json:"timings"`
				} `json:"entries"`
			} `json:"log"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Log.Entries) != len(ids) {
			t.Fatal("selection changed")
		}
		for i, entry := range doc.Log.Entries {
			if ids[i] == 9002 && (entry.Timings.DNS != -1 || entry.Timings.Connect != -1) {
				t.Fatal("reused history connection charged again")
			}
			if ids[i] == 9001 && (entry.Timings.DNS != .122 || entry.Timings.Connect != .333) {
				t.Fatal("first history connection timings missing")
			}
		}
	}
}

func TestExportHARStreamsV2ConnectionTimings(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryWithVersion(t, historyDir, "export-v2", 2)
	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "v2.har")
	result, err := service.ExportHAR(HARExportRequest{Key: "export-v2", Path: target, TrafficIDs: []uint64{9001}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Exported != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Log struct {
			Entries []struct {
				ConnectionTimings *proxyservice.HTTPConnectionTimings `json:"_connectionTimings"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Log.Entries) != 1 {
		t.Fatalf("entries = %d", len(document.Log.Entries))
	}
	got := document.Log.Entries[0].ConnectionTimings
	if got == nil || got.DNSStartedAtMicros != 1000001 || got.DNSEndedAtMicros != 1000123 || got.TLSEndedAtMicros != -1 {
		t.Fatalf("exported connection timings = %+v", got)
	}
}

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
