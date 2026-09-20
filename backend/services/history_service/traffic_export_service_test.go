package historyservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func TestHistoryTrafficExportSupportedVersions(t *testing.T) {
	for _, version := range []uint16{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			directory := setupHistoryTestStorage(t)
			writeHistoryWithVersion(t, directory, "export", version)
			service := New(nil, nil)
			if err := service.initializeHistoryIndexMap(); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []proxyservice.TrafficExportKind{proxyservice.TrafficExportRequestMessage, proxyservice.TrafficExportRequestHeaders, proxyservice.TrafficExportRequestBody, proxyservice.TrafficExportResponseBody, proxyservice.TrafficExportCSV} {
				target := filepath.Join(t.TempDir(), "export")
				result, err := service.ExportTraffic(context.Background(), TrafficExportRequest{Key: "export", Kind: kind, Path: target, TargetType: "file", TrafficIDs: []uint64{9001, 9001}})
				if err != nil {
					t.Fatal(err)
				}
				if kind == proxyservice.TrafficExportResponseBody {
					if result.Skipped != 1 || result.Exported != 0 {
						t.Fatalf("result = %+v", result)
					}
					if _, err := os.Stat(target); !os.IsNotExist(err) {
						t.Fatal("absent response created a file")
					}
					continue
				}
				data, err := os.ReadFile(target)
				if err != nil || result.Exported != 1 {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
				if kind == proxyservice.TrafficExportRequestBody && len(data) != 0 {
					t.Fatalf("empty body = %q", data)
				}
				if kind == proxyservice.TrafficExportRequestMessage && string(data) != "GET /process HTTP/2.0\r\nhost: current.example\r\n\r\n" {
					t.Fatalf("message = %q", data)
				}
				if kind == proxyservice.TrafficExportCSV && !strings.Contains(string(data), "FlowLens Fixture") {
					t.Fatalf("CSV = %q", data)
				}
			}
			result, err := service.ExportTraffic(context.Background(), TrafficExportRequest{Key: "export", Kind: proxyservice.TrafficExportRequestHeaders, Path: t.TempDir(), TargetType: "directory"})
			if err != nil || result.Exported != 1 {
				t.Fatalf("all: %+v, %v", result, err)
			}
			if _, err := service.ExportTraffic(context.Background(), TrafficExportRequest{Key: "export", Kind: proxyservice.TrafficExportCSV, Path: filepath.Join(t.TempDir(), "bad.csv"), TargetType: "file", TrafficIDs: []uint64{2}}); err == nil {
				t.Fatal("unknown ID accepted")
			}
		})
	}
}
