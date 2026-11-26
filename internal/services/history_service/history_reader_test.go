package historyservice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

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
