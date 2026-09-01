package bodycache_test

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
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

func TestWriterSnapshotReadsActiveBytesAndAllowsFurtherWrites(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	w, err := c.Writer(80, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	t.Cleanup(w.Abort)
	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatalf("Write first: %v", err)
	}

	snapshot, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if string(snapshot) != "first" {
		t.Fatalf("Snapshot = %q, want first", snapshot)
	}

	if _, err := w.Write([]byte(" second")); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := c.Read(80, bodycache.KindResponse)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "first second" {
		t.Fatalf("Read = %q, want first second", got)
	}
}
