package processattribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	appfs "github.com/josexy/flowlens/backend/pkg/fs"
)

func TestIconStoreUsesPNGContentSHA256AsKey(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewIconStore(baseDir)
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	icon := solidNRGBA(64, 64, color.NRGBA{R: 30, G: 80, B: 180, A: 220})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	sum := sha256.Sum256(encoded.Bytes())
	wantKey := hex.EncodeToString(sum[:])

	gotKey, err := store.Put(icon)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if gotKey != wantKey {
		t.Fatalf("Put key = %q, want %q", gotKey, wantKey)
	}
	entries, err := os.ReadDir(filepath.Join(baseDir, "cache", "process-icons"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != wantKey+".png" {
		t.Fatalf("stored entries = %v, want %s.png", entryNames(entries), wantKey)
	}
}

func TestIconStorePutTightensExistingIconPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	baseDir := t.TempDir()
	store, err := NewIconStore(baseDir)
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	icon := solidNRGBA(32, 32, color.NRGBA{R: 25, G: 75, B: 125, A: 255})
	key, err := store.Put(icon)
	if err != nil {
		t.Fatalf("initial Put: %v", err)
	}
	path := filepath.Join(store.dir, key+".png")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("weaken cached icon permissions: %v", err)
	}

	if _, err := store.Put(icon); err != nil {
		t.Fatalf("deduplicated Put: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat cached icon: %v", err)
	}
	if got := info.Mode().Perm(); got != appfs.PrivateFileMode {
		t.Fatalf("cached icon mode = %04o, want %04o", got, appfs.PrivateFileMode)
	}
}

func TestIconStoreDeduplicatesConcurrentWrites(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewIconStore(baseDir)
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	icon := solidNRGBA(64, 64, color.NRGBA{R: 10, G: 160, B: 90, A: 255})

	const writers = 24
	keys := make(chan string, writers)
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			key, putErr := store.Put(icon)
			keys <- key
			errs <- putErr
		})
	}
	wg.Wait()
	close(keys)
	close(errs)

	var wantKey string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Put: %v", err)
		}
	}
	for key := range keys {
		if wantKey == "" {
			wantKey = key
		}
		if key != wantKey {
			t.Fatalf("concurrent Put key = %q, want %q", key, wantKey)
		}
	}
	entries, err := os.ReadDir(filepath.Join(baseDir, "cache", "process-icons"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != wantKey+".png" {
		t.Fatalf("stored entries = %v, want only %s.png", entryNames(entries), wantKey)
	}
}

func TestIconStoreNormalizesConcurrentPuts(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	first := newBlockingIcon(color.NRGBA{R: 220, A: 255})
	second := newBlockingIcon(color.NRGBA{B: 220, A: 255})
	type result struct {
		key string
		err error
	}
	results := make(chan result, 2)
	go func() {
		key, putErr := store.Put(first)
		results <- result{key: key, err: putErr}
	}()
	select {
	case <-first.started:
	case <-time.After(2 * time.Second):
		close(first.release)
		t.Fatal("first Put did not start normalization")
	}

	go func() {
		key, putErr := store.Put(second)
		results <- result{key: key, err: putErr}
	}()
	select {
	case <-second.started:
	case <-time.After(250 * time.Millisecond):
		close(first.release)
		close(second.release)
		<-results
		<-results
		t.Fatal("second Put waited for the first Put to finish normalization")
	}

	close(first.release)
	close(second.release)
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("Put: %v", got.err)
		}
		if got.key == "" {
			t.Fatal("Put returned an empty key")
		}
	}
}

func TestIconStoreResetWaitsForActivePut(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	icon := newBlockingIcon(color.NRGBA{G: 220, A: 255})
	putDone := make(chan error, 1)
	go func() {
		_, putErr := store.Put(icon)
		putDone <- putErr
	}()
	select {
	case <-icon.started:
	case <-time.After(2 * time.Second):
		close(icon.release)
		t.Fatal("Put did not start normalization")
	}

	resetDone := make(chan error, 1)
	go func() { resetDone <- store.Reset() }()
	select {
	case resetErr := <-resetDone:
		close(icon.release)
		<-putDone
		t.Fatalf("Reset returned while Put was active: %v", resetErr)
	case <-time.After(30 * time.Millisecond):
	}

	close(icon.release)
	if putErr := <-putDone; putErr != nil {
		t.Fatalf("Put: %v", putErr)
	}
	select {
	case resetErr := <-resetDone:
		if resetErr != nil {
			t.Fatalf("Reset: %v", resetErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reset did not finish after Put completed")
	}
}

func TestIconStoreRejectsInvalidKey(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	for _, key := range []string{
		"",
		"../secret",
		strings.Repeat("A", 64),
		strings.Repeat("0", 63),
		"g" + strings.Repeat("0", 63),
		"a" + string(filepath.Separator) + strings.Repeat("0", 62),
	} {
		t.Run(hex.EncodeToString([]byte(key)), func(t *testing.T) {
			if _, _, err := store.Get(key); err == nil {
				t.Fatalf("Get(%q) succeeded, want invalid-key error", key)
			}
		})
	}
}

func TestIconStoreReturnsMissingForClearedKey(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	key, err := store.Put(solidNRGBA(8, 8, color.NRGBA{R: 255, A: 255}))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	data, found, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get after Reset: %v", err)
	}
	if found || data != nil {
		t.Fatalf("Get after Reset = (%d bytes, %v), want (nil, false)", len(data), found)
	}
}

func TestIconStoreResetWaitsForActiveReader(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	if _, err := store.Put(solidNRGBA(8, 8, color.NRGBA{G: 255, A: 255})); err != nil {
		t.Fatalf("Put: %v", err)
	}

	store.mu.RLock()
	resetDone := make(chan error, 1)
	go func() { resetDone <- store.Reset() }()
	select {
	case err := <-resetDone:
		store.mu.RUnlock()
		t.Fatalf("Reset returned while reader was active: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	store.mu.RUnlock()
	select {
	case err := <-resetDone:
		if err != nil {
			t.Fatalf("Reset: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reset did not finish after reader released")
	}
	if _, err := os.Stat(store.dir); !os.IsNotExist(err) {
		t.Fatalf("icon directory still exists after Reset: %v", err)
	}
}

func TestIconStoreNormalizesTo64Pixels(t *testing.T) {
	store, err := NewIconStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewIconStore: %v", err)
	}
	key, err := store.Put(solidNRGBA(2, 4, color.NRGBA{R: 255, G: 120, A: 255}))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, found, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("normalized icon was not found")
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if got := decoded.Bounds().Size(); got.X != 64 || got.Y != 64 {
		t.Fatalf("normalized size = %v, want 64x64", got)
	}
	if _, _, _, alpha := decoded.At(0, 32).RGBA(); alpha != 0 {
		t.Fatalf("letterbox pixel alpha = %d, want transparent", alpha)
	}
	if _, _, _, alpha := decoded.At(32, 32).RGBA(); alpha == 0 {
		t.Fatal("scaled image center is transparent")
	}
}

func solidNRGBA(width, height int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, fill)
		}
	}
	return img
}

type blockingIcon struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	fill    color.NRGBA
}

func newBlockingIcon(fill color.NRGBA) *blockingIcon {
	return &blockingIcon{
		started: make(chan struct{}),
		release: make(chan struct{}),
		fill:    fill,
	}
}

func (i *blockingIcon) ColorModel() color.Model {
	return color.NRGBAModel
}

func (i *blockingIcon) Bounds() image.Rectangle {
	return image.Rect(0, 0, 64, 64)
}

func (i *blockingIcon) At(_, _ int) color.Color {
	i.once.Do(func() { close(i.started) })
	<-i.release
	return i.fill
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
