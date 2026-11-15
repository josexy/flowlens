package bodyspool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreReadKeepsInlinePayloadAtLimit(t *testing.T) {
	store := newTestStore(t)
	payloadBytes := patternedBytes(int(DefaultInlineLimit))

	payload, err := store.Read(context.Background(), bytes.NewReader(payloadBytes), DefaultInlineLimit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if payload.File != nil {
		t.Fatalf("Read() file = %+v, want inline payload", payload.File)
	}
	if !bytes.Equal(payload.Inline, payloadBytes) {
		t.Fatal("Read() inline bytes differ from input")
	}
	if got := payload.Size(); got != DefaultInlineLimit {
		t.Fatalf("Size() = %d, want %d", got, DefaultInlineLimit)
	}
	assertPayloadOpenBytes(t, payload, payloadBytes)
}

func TestStoreReadSpillsPayloadAboveLimit(t *testing.T) {
	store := newTestStore(t)
	payloadBytes := patternedBytes(int(DefaultInlineLimit + 1))

	payload, err := store.Read(context.Background(), bytes.NewReader(payloadBytes), DefaultInlineLimit)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if payload.Inline != nil {
		t.Fatalf("Read() inline length = %d, want file-backed payload", len(payload.Inline))
	}
	if payload.File == nil {
		t.Fatal("Read() file = nil, want file-backed payload")
	}
	if got := payload.Size(); got != int64(len(payloadBytes)) {
		t.Fatalf("Size() = %d, want %d", got, len(payloadBytes))
	}
	assertPayloadOpenBytes(t, payload, payloadBytes)
	assertPrivateModes(t, filepath.Dir(payload.File.Path), payload.File.Path)
}

func TestStoreCopyFileOwnsIndependentCopy(t *testing.T) {
	store := newTestStore(t)
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "source.bin")
	want := patternedBytes(2*1024*1024 + 17)
	if err := os.WriteFile(sourcePath, want, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := store.CopyFile(context.Background(), sourcePath)
	if err != nil {
		t.Fatalf("CopyFile() error = %v", err)
	}
	if payload.File == nil {
		t.Fatal("CopyFile() returned inline payload")
	}
	if samePath(payload.File.Path, sourcePath) {
		t.Fatalf("CopyFile() retained caller path %q", sourcePath)
	}
	if err := os.WriteFile(sourcePath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertPayloadOpenBytes(t, payload, want)
}

func TestStoreHandoffAdoptsOnlyOwnedRegularFiles(t *testing.T) {
	store := newTestStore(t)
	handoff, err := store.NewHandoffDir()
	if err != nil {
		t.Fatalf("NewHandoffDir() error = %v", err)
	}
	otherHandoff, err := store.NewHandoffDir()
	if err != nil {
		t.Fatalf("second NewHandoffDir() error = %v", err)
	}
	want := []byte("staged body")
	stagedPath := filepath.Join(handoff, "body.bin")
	if err := os.WriteFile(stagedPath, want, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := store.AdoptFile(context.Background(), handoff, stagedPath)
	if err != nil {
		t.Fatalf("AdoptFile() error = %v", err)
	}
	if payload.File == nil || !samePath(payload.File.Path, stagedPath) {
		t.Fatalf("AdoptFile() file = %+v, want %q", payload.File, stagedPath)
	}
	assertPayloadOpenBytes(t, payload, want)

	externalPath := filepath.Join(t.TempDir(), "external.bin")
	if err := os.WriteFile(externalPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	siblingPath := filepath.Join(otherHandoff, "sibling.bin")
	if err := os.WriteFile(siblingPath, want, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"external":  externalPath,
		"sibling":   siblingPath,
		"directory": handoff,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.AdoptFile(context.Background(), handoff, path); err == nil {
				t.Fatalf("AdoptFile(%q) succeeded", path)
			}
		})
	}

	symlinkPath := filepath.Join(handoff, "link.bin")
	if err := os.Symlink(externalPath, symlinkPath); err == nil {
		if _, err := store.AdoptFile(context.Background(), handoff, symlinkPath); err == nil {
			t.Fatalf("AdoptFile(%q) accepted a symlink", symlinkPath)
		}
	}
	assertPrivateModes(t, handoff, stagedPath)
}

func TestStoreReadCancellationRemovesPartialFile(t *testing.T) {
	store := newTestStore(t)
	handoff, err := store.NewHandoffDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(handoff)
	ctx, cancel := context.WithCancel(context.Background())
	prefixReads := int((DefaultInlineLimit + 1 + 64*1024 - 1) / (64 * 1024))
	reader := &cancelingReader{
		remaining:        int(DefaultInlineLimit + 2*1024*1024),
		cancel:           cancel,
		cancelAfterReads: prefixReads + 1,
	}

	_, err = store.Read(ctx, reader, DefaultInlineLimit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
	assertNoRegularFilesOutside(t, root, handoff)
}

func TestStoreHonorsCanceledCopyAndAdopt(t *testing.T) {
	store := newTestStore(t)
	sourcePath := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(sourcePath, patternedBytes(1024), 0o600); err != nil {
		t.Fatal(err)
	}
	handoff, err := store.NewHandoffDir()
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(handoff, "staged.bin")
	if err := os.WriteFile(stagedPath, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.CopyFile(ctx, sourcePath); !errors.Is(err, context.Canceled) {
		t.Fatalf("CopyFile() error = %v, want context.Canceled", err)
	}
	if _, err := store.AdoptFile(ctx, handoff, stagedPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("AdoptFile() error = %v, want context.Canceled", err)
	}
}

func TestPayloadOpenRejectsConflictingRepresentations(t *testing.T) {
	payload := Payload{
		Inline: []byte("inline"),
		File:   &File{Path: "ignored", Size: 7},
	}
	if _, err := payload.Open(); err == nil {
		t.Fatal("Open() succeeded for conflicting payload representations")
	}
}

func TestStoreCloseIsIdempotentAndRejectsFutureWrites(t *testing.T) {
	store, err := New("flowlens-python-body-test-")
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := store.NewHandoffDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(handoff)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(%q) error = %v, want os.ErrNotExist", root, err)
	}

	if _, err := store.Read(context.Background(), bytes.NewReader(nil), DefaultInlineLimit); err == nil {
		t.Fatal("Read() succeeded after Close()")
	}
	if _, err := store.CopyFile(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("CopyFile() succeeded after Close()")
	}
	if _, err := store.NewHandoffDir(); err == nil {
		t.Fatal("NewHandoffDir() succeeded after Close()")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New("flowlens-python-body-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func patternedBytes(size int) []byte {
	pattern := []byte("FlowLens-body-spool-pattern\x00\xff")
	value := make([]byte, size)
	for offset := 0; offset < len(value); {
		offset += copy(value[offset:], pattern)
	}
	return value
}

func assertPayloadOpenBytes(t *testing.T, payload Payload, want []byte) {
	t.Helper()
	reader, err := payload.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("opened payload differs: got %d bytes, want %d", len(got), len(want))
	}
}

func assertPrivateModes(t *testing.T, directory, file string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}

func assertNoRegularFilesOutside(t *testing.T, root, allowedDirectory string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if samePath(path, allowedDirectory) {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			t.Fatalf("partial file remained after cancellation: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func samePath(left, right string) bool {
	left, _ = filepath.Abs(left)
	right, _ = filepath.Abs(right)
	if runtime.GOOS == "windows" {
		return filepath.Clean(left) == filepath.Clean(right) || equalFoldPath(left, right)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func equalFoldPath(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftByte := left[index]
		rightByte := right[index]
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}

type cancelingReader struct {
	remaining        int
	cancel           context.CancelFunc
	cancelAfterReads int
	reads            int
}

func (r *cancelingReader) Read(value []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	count := min(min(len(value), 64*1024), r.remaining)
	for index := range count {
		value[index] = byte((r.reads + index) % 251)
	}
	r.remaining -= count
	r.reads++
	if r.reads == r.cancelAfterReads {
		r.cancel()
	}
	return count, nil
}
