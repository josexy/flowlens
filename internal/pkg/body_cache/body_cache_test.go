package bodycache_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
)

func TestNewWithDirTightensExistingStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "body-cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod dir setup: %v", err)
	}
	existing := filepath.Join(dir, "1_req.body")
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(existing, 0o644); err != nil {
		t.Fatalf("Chmod file setup: %v", err)
	}

	cache, err := bodycache.NewWithDir(dir)
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	if info, statErr := os.Stat(cache.SessionDir()); statErr != nil {
		t.Fatalf("Stat dir: %v", statErr)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("body cache dir mode = %04o, want 0700", got)
	}
	if info, statErr := os.Stat(existing); statErr != nil {
		t.Fatalf("Stat existing file: %v", statErr)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("existing body mode = %04o, want 0600", got)
	}

	if err := cache.Write(2, bodycache.KindResponse, []byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	created := filepath.Join(dir, "2_resp.body")
	if info, statErr := os.Stat(created); statErr != nil {
		t.Fatalf("Stat created file: %v", statErr)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("created body mode = %04o, want 0600", got)
	}
}

func newTestCache(t *testing.T) *bodycache.BodyCache {
	t.Helper()
	dir := t.TempDir()
	c, err := bodycache.NewWithDir(dir)
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	return c
}

func TestWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	data := []byte("hello body cache")
	if err := c.Write(42, bodycache.KindRequest, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read(42, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestHasReturnsFalseBeforeWrite(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	if c.Has(1, bodycache.KindResponse) {
		t.Error("Has should be false before write")
	}
}

func TestHasReturnsTrueAfterWrite(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	if err := c.Write(1, bodycache.KindResponse, []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !c.Has(1, bodycache.KindResponse) {
		t.Error("Has should be true after write")
	}
}

func TestDeleteClearsHas(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	if err := c.Write(5, bodycache.KindRequest, []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	c.Delete(5)
	if c.Has(5, bodycache.KindRequest) {
		t.Error("Has should be false after Delete")
	}
	if _, err := c.Read(5, bodycache.KindRequest); !os.IsNotExist(err) {
		t.Errorf("Read after Delete should return IsNotExist, got %v", err)
	}
}

func TestKindIsolation(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	if err := c.Write(10, bodycache.KindRequest, []byte("req")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if c.Has(10, bodycache.KindResponse) {
		t.Error("writing KindRequest should not affect KindResponse")
	}
}

func TestWriteIsAtomic(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i)
	}
	if err := c.Write(99, bodycache.KindResponse, data); err != nil {
		t.Fatalf("Write large: %v", err)
	}
	got, err := c.Read(99, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Read large: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("length mismatch: got %d, want %d", len(got), len(data))
	}
}

func TestWriterCloseCommitsAndMarksHas(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(77, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := w.Write([]byte("streamed")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	w.Abort()

	if !c.Has(77, bodycache.KindRequest) {
		t.Fatal("Has should be true after writer Close")
	}
	got, err := c.Read(77, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "streamed" {
		t.Fatalf("Read = %q, want streamed", got)
	}
	if _, err := os.Stat(filepath.Join(c.SessionDir(), "77_req.body.tmp")); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be removed after Close, got %v", err)
	}
}

func TestWriterAbortRemovesTmpAndIsRepeatable(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(78, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := w.Write([]byte("discarded")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.Abort()
	w.Abort()

	if c.Has(78, bodycache.KindResponse) {
		t.Fatal("Has should remain false after Abort")
	}
	if _, err := os.Stat(filepath.Join(c.SessionDir(), "78_resp.body.tmp")); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be removed after Abort, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.SessionDir(), "78_resp.body")); !os.IsNotExist(err) {
		t.Fatalf("body file should not exist after Abort, got %v", err)
	}
}

func TestWriterPreserveKeepsTmpReadableUntilDelete(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(79, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := w.Write([]byte("preserved tmp")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Preserve(); err != nil {
		t.Fatalf("Preserve: %v", err)
	}

	if !c.Has(79, bodycache.KindRequest) {
		t.Fatal("Has should be true after Preserve")
	}
	got, err := c.Read(79, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Read preserved tmp: %v", err)
	}
	if string(got) != "preserved tmp" {
		t.Fatalf("Read = %q, want preserved tmp", got)
	}
	reader, size, err := c.Reader(79, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Reader preserved tmp: %v", err)
	}
	readerBytes, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		t.Fatalf("ReadAll preserved tmp: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("Close reader: %v", closeErr)
	}
	if size != int64(len("preserved tmp")) || string(readerBytes) != "preserved tmp" {
		t.Fatalf("Reader = size %d body %q, want preserved tmp", size, readerBytes)
	}

	c.Delete(79)
	if c.Has(79, bodycache.KindRequest) {
		t.Fatal("Has should be false after Delete")
	}
	if _, err := os.Stat(filepath.Join(c.SessionDir(), "79_req.body.tmp")); !os.IsNotExist(err) {
		t.Fatalf("tmp file should be removed after Delete, got %v", err)
	}
}

func readSnapshot(t *testing.T, w *bodycache.BodyCacheWriter) ([]byte, int64) {
	t.Helper()
	reader, size, err := w.SnapshotReader()
	if err != nil {
		t.Fatalf("SnapshotReader: %v", err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("Close snapshot reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("ReadAll snapshot: %v", err)
	}
	if int64(len(data)) != size {
		t.Fatalf("snapshot read %d bytes, size %d", len(data), size)
	}
	return data, size
}

func TestWriterSnapshotReaderIgnoresLaterWrites(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(81, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	t.Cleanup(w.Abort)
	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatalf("Write first: %v", err)
	}

	reader, size, err := w.SnapshotReader()
	if err != nil {
		t.Fatalf("SnapshotReader: %v", err)
	}
	defer reader.Close()
	if size != int64(len("first")) {
		t.Fatalf("size = %d, want %d", size, len("first"))
	}
	// Write after the snapshot was taken and before it is read: the snapshot
	// must still return exactly the bytes written when it was opened.
	if _, err := w.Write([]byte(" second")); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("snapshot = %q, want first", got)
	}

	if data, _ := readSnapshot(t, w); string(data) != "first second" {
		t.Fatalf("second snapshot = %q, want first second", data)
	}
}

func TestWriterSnapshotReaderAfterCloseAndPreserve(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	committed, err := c.Writer(82, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := committed.Write([]byte("committed")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	open, _, err := committed.SnapshotReader()
	if err != nil {
		t.Fatalf("SnapshotReader before Close: %v", err)
	}
	// An open snapshot reader must not prevent the commit.
	if err := committed.Close(); err != nil {
		t.Fatalf("Close with open snapshot reader: %v", err)
	}
	if got, err := io.ReadAll(open); err != nil || string(got) != "committed" {
		t.Fatalf("snapshot opened before Close = %q, %v", got, err)
	}
	_ = open.Close()
	if _, err := os.Stat(filepath.Join(c.SessionDir(), "82_req.body")); err != nil {
		t.Fatalf("committed file: %v", err)
	}
	if data, _ := readSnapshot(t, committed); string(data) != "committed" {
		t.Fatalf("snapshot after Close = %q", data)
	}

	preserved, err := c.Writer(83, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	if _, err := preserved.Write([]byte("preserved")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := preserved.Preserve(); err != nil {
		t.Fatalf("Preserve: %v", err)
	}
	if data, _ := readSnapshot(t, preserved); string(data) != "preserved" {
		t.Fatalf("snapshot after Preserve = %q", data)
	}

	preserved.Abort()
	if _, _, err := preserved.SnapshotReader(); err == nil {
		t.Fatal("SnapshotReader after Abort should fail")
	}
}

func TestWriterSnapshotReaderConcurrentWithWrites(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(84, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	t.Cleanup(w.Abort)

	const chunks = 200
	var expected []byte
	for i := range chunks {
		expected = append(expected, bytes.Repeat([]byte{byte('a' + i%26)}, 97)...)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range chunks {
			if _, err := w.Write(expected[i*97 : (i+1)*97]); err != nil {
				t.Errorf("Write %d: %v", i, err)
				return
			}
		}
	}()

	var last int64
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		data, size := readSnapshot(t, w)
		if size < last {
			t.Fatalf("snapshot size went back from %d to %d", last, size)
		}
		if !bytes.Equal(data, expected[:size]) {
			t.Fatalf("snapshot of %d bytes is not a prefix of the written data", size)
		}
		last = size
	}
	if data, _ := readSnapshot(t, w); !bytes.Equal(data, expected) {
		t.Fatalf("final snapshot has %d bytes, want %d", len(data), len(expected))
	}
}

func TestAppendFileConcurrentAppendAndRead(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	f, err := c.AppendFile(90, bodycache.KindWebSocket)
	if err != nil {
		t.Fatalf("AppendFile: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	type record struct {
		offset int64
		data   []byte
	}
	records := make(chan record, 256)
	go func() {
		defer close(records)
		for i := range 200 {
			data := bytes.Repeat([]byte{byte(i)}, 1+i*37%4096)
			offset, err := f.Append(data)
			if err != nil {
				t.Errorf("Append %d: %v", i, err)
				return
			}
			records <- record{offset: offset, data: data}
		}
	}()
	var expectedOffset int64
	for r := range records {
		if r.offset != expectedOffset {
			t.Fatalf("offset = %d, want %d", r.offset, expectedOffset)
		}
		expectedOffset += int64(len(r.data))
		got := make([]byte, len(r.data))
		if _, err := f.ReadAt(got, r.offset); err != nil {
			t.Fatalf("ReadAt %d: %v", r.offset, err)
		}
		if !bytes.Equal(got, r.data) {
			t.Fatalf("payload at %d changed", r.offset)
		}
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := f.Append([]byte("late")); err == nil {
		t.Fatal("Append after Close should fail")
	}
	first := make([]byte, 1)
	if _, err := f.ReadAt(first, 0); err != nil || first[0] != 0 {
		t.Fatalf("ReadAt after Close = %v, %v", first, err)
	}
}

func TestDeleteRemovesAppendFile(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	if err := c.Write(91, bodycache.KindResponse, []byte("response")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f, err := c.AppendFile(91, bodycache.KindWebSocket)
	if err != nil {
		t.Fatalf("AppendFile: %v", err)
	}
	if _, err := f.Append([]byte("payload")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	path := filepath.Join(c.SessionDir(), "91_ws.body")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("append file: %v", err)
	}

	c.Delete(91)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("append file should be removed with the entry, got %v", err)
	}
	if c.Has(91, bodycache.KindResponse) {
		t.Fatal("response body should be removed with the entry")
	}
	if _, err := f.ReadAt(make([]byte, 1), 0); err == nil {
		t.Fatal("ReadAt of a deleted append file should fail")
	}
}
