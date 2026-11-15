package historyservice

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The files in testdata/old_build were written by the Wails-based 1.0.5 build.
func copyOldBuildHistories(t *testing.T, target string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "old_build", "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	for _, file := range files {
		source, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		destination, err := os.Create(filepath.Join(target, filepath.Base(file)))
		if err != nil {
			t.Fatal(err)
		}
		defer destination.Close()
		if _, err := io.Copy(destination, source); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoriesFromTheOldBuildLoadUnchanged(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	copyOldBuildHistories(t, historyDir)
	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatal(err)
	}
	list, err := service.ListHistoryKeys()
	if err != nil || len(list) != 3 {
		t.Fatalf("list %+v, %v", list, err)
	}
	for _, key := range []string{"old-v1", "old-v2", "old-raw-tcp"} {
		entries, err := service.GetHistory(key)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s: %+v %v", key, entries, err)
		}
		if _, err := service.GetHistoryTrafficBodyView(key, entries[0].ID); err != nil {
			t.Fatalf("%s body: %v", key, err)
		}
	}
	entries, _ := service.GetHistory("old-v2")
	if entries[0].Metadata.ConnectionTimings == nil {
		t.Fatal("v2 timings lost")
	}
	raw, _ := service.GetHistory("old-raw-tcp")
	if raw[0].RawTCP == nil || raw[0].RawTCP.HostPort != "db.internal.example:5432" {
		t.Fatalf("%+v", raw[0].RawTCP)
	}
}
