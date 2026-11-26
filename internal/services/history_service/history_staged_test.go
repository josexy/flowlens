package historyservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/josexy/flowlens/internal/pkg/fs"
)

func TestStagedHistoryReadsPreserveVersionsAndSurviveMissingBody(t *testing.T) {
	directory := setupHistoryTestStorage(t)
	for _, version := range []uint16{1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			key := fmt.Sprintf("staged-%d", version)
			writeHistoryWithVersion(t, directory, key, version)
			s := New(nil, nil)
			if err := s.refreshHistories(); err != nil {
				t.Fatal(err)
			}
			full, err := s.GetHistoryDetail(context.Background(), key, 9001)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := s.GetHistoryEntry(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(entry, full.Entry) {
				t.Fatalf("metadata differs: %v", err)
			}
			body, err := s.GetHistoryBody(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(body, full.Body) {
				t.Fatalf("body differs: %v", err)
			}
			index, err := s.historyIndex(key)
			if err != nil {
				t.Fatal(err)
			}
			position, _ := index.entries.Get(9001)
			if err := os.Truncate(filepath.Join(directory, fs.GetHBinFileName(key)), int64(position.bodyIndex)); err != nil {
				t.Fatal(err)
			}
			entry, err = s.GetHistoryEntry(context.Background(), key, 9001)
			if err != nil || !reflect.DeepEqual(entry, full.Entry) {
				t.Fatalf("metadata unnecessarily depends on the body: %v", err)
			}
			if _, err := s.GetHistoryBody(context.Background(), key, 9001); err == nil {
				t.Fatal("missing body accepted")
			}
			partial, err := s.GetHistoryDetail(context.Background(), key, 9001)
			if err == nil || !reflect.DeepEqual(partial.Entry, full.Entry) {
				t.Fatal("complete action read changed its partial-error behavior")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := s.GetHistoryEntry(ctx, key, 9001); !errors.Is(err, context.Canceled) {
				t.Fatalf("metadata cancellation: %v", err)
			}
			if _, err := s.GetHistoryBody(ctx, key, 9001); !errors.Is(err, context.Canceled) {
				t.Fatalf("body cancellation: %v", err)
			}
			if _, err := s.GetHistoryEntry(context.Background(), key, 999); err == nil {
				t.Fatal("missing metadata accepted")
			}
		})
	}
}
