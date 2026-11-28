package historyservice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/pkg/fs"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestHistorySummaryCancelsWhileSkippingCookie(t *testing.T) {
	var encoded bytes.Buffer
	writeHistoryValue(t, &encoded, uint64(1))
	writeHistoryString(t, &encoded, "https")
	writeHistoryValue(t, &encoded, int64(0)) // StartedAt
	for _, value := range []string{"GET", "https://example.test/", "example.test", "/"} {
		writeHistoryString(t, &encoded, value)
	}
	writeHistoryValue(t, &encoded, int32(200))
	writeHistoryString(t, &encoded, "OK")
	writeHistoryValue(t, &encoded, uint8(0)) // Metadata
	writeHistoryValue(t, &encoded, uint8(1)) // Request
	writeHistoryString(t, &encoded, "HTTP/1.1")
	writeHistoryHeaderFields(t, &encoded, []proxyservice.HTTPHeaderField{{Name: "Cookie", Value: strings.Repeat("x", 1<<20)}})
	writeHistoryHeaderFields(t, &encoded, nil)
	for range 4 { // Request metrics, response, error, connection timings.
		writeHistoryValue(t, &encoded, uint8(0))
	}
	if _, err := proxyservice.DecodeTrafficEntrySummaryWithVersion(bytes.NewReader(encoded.Bytes()), 3); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancelHistoryReader{reader: bytes.NewReader(encoded.Bytes()), cancel: cancel}
	reader := bufio.NewReader(historyContextReader{ctx: ctx, reader: source})
	entry, err := proxyservice.DecodeTrafficEntrySummaryWithVersion(reader, 3)
	if entry != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("entry=%v, error=%v", entry, err)
	}
	if source.bytesRead > 8192 {
		t.Fatalf("read %d bytes after cancellation during the skipped field", source.bytesRead)
	}
}

type cancelHistoryReader struct {
	reader    io.Reader
	cancel    context.CancelFunc
	bytesRead int
}

func (r *cancelHistoryReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += n
	r.cancel()
	return n, err
}

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
