package bodycache

import (
	"errors"
	"fmt"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sync"

	appfs "github.com/josexy/flowlens/backend/pkg/fs"
)

const (
	KindRequest  = "req"
	KindResponse = "resp"
)

const (
	MaxBodyCacheThresholdBytes = 10 * 1024
)

// BodyCache stores body bytes on disk in a dedicated session directory.
// Write and BodyCacheWriter.Close commit via tmp-to-final rename. Preserved
// tmp files remain readable until Delete/DeleteKind removes them.
// Safe for concurrent use.
type BodyCache struct {
	mu         sync.RWMutex
	sessionDir string
	cached     map[uint64]uint8
}

func New(sessionDir string) (*BodyCache, error) {
	return NewWithDir(sessionDir)
}

func NewWithDir(dir string) (*BodyCache, error) {
	if err := appfs.EnsurePrivateDir(dir); err != nil {
		return nil, fmt.Errorf("body_cache: mkdir %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("body_cache: read dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := appfs.EnsurePrivateFile(path); err != nil {
			return nil, fmt.Errorf("body_cache: tighten %s: %w", path, err)
		}
	}
	return &BodyCache{
		sessionDir: dir,
		cached:     make(map[uint64]uint8),
	}, nil
}

func (c *BodyCache) filePath(id uint64, kind string) string {
	return filepath.Join(c.sessionDir, fmt.Sprintf("%d_%s.body", id, kind))
}

func (c *BodyCache) tmpFilePath(id uint64, kind string) string {
	return c.filePath(id, kind) + ".tmp"
}

func kindBit(kind string) uint8 {
	if kind == KindRequest {
		return 1
	}
	return 2
}

func (c *BodyCache) Write(id uint64, kind string, data []byte) error {
	path := c.filePath(id, kind)
	tmp := c.tmpFilePath(id, kind)
	if err := os.WriteFile(tmp, data, appfs.PrivateFileMode); err != nil {
		return fmt.Errorf("body_cache: write tmp %s: %w", tmp, err)
	}
	if err := appfs.EnsurePrivateFile(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("body_cache: tighten tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("body_cache: rename %s: %w", path, err)
	}
	c.mu.Lock()
	c.cached[id] |= kindBit(kind)
	c.mu.Unlock()
	return nil
}

type BodyCacheWriter struct {
	cache *BodyCache
	id    uint64
	kind  string
	path  string
	tmp   string
	file  *os.File
	mu    sync.Mutex
}

func (c *BodyCache) Writer(id uint64, kind string) (*BodyCacheWriter, error) {
	path := c.filePath(id, kind)
	tmp := c.tmpFilePath(id, kind)
	fp, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, appfs.PrivateFileMode)
	if err != nil {
		return nil, fmt.Errorf("body_cache: open tmp %s: %w", tmp, err)
	}
	if err := appfs.EnsurePrivateFile(tmp); err != nil {
		_ = fp.Close()
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("body_cache: tighten tmp %s: %w", tmp, err)
	}
	return &BodyCacheWriter{
		cache: c,
		id:    id,
		kind:  kind,
		path:  path,
		tmp:   tmp,
		file:  fp,
	}, nil
}

func (w *BodyCacheWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, fmt.Errorf("body_cache: writer closed")
	}
	return w.file.Write(p)
}

// Snapshot returns a stable copy of the bytes written so far without closing
// the streaming writer. Callers may continue writing after Snapshot returns.
func (w *BodyCacheWriter) Snapshot() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		data, err := w.cache.Read(w.id, w.kind)
		if err != nil {
			return nil, fmt.Errorf("body_cache: snapshot closed writer: %w", err)
		}
		return data, nil
	}
	if err := w.file.Sync(); err != nil {
		return nil, fmt.Errorf("body_cache: sync snapshot %s: %w", w.tmp, err)
	}
	data, err := os.ReadFile(w.tmp)
	if err != nil {
		return nil, fmt.Errorf("body_cache: read snapshot %s: %w", w.tmp, err)
	}
	return data, nil
}

func (w *BodyCacheWriter) markCachedLocked() {
	w.cache.mu.Lock()
	w.cache.cached[w.id] |= kindBit(w.kind)
	w.cache.mu.Unlock()
}

func (w *BodyCacheWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	fp := w.file
	w.file = nil
	if err := fp.Close(); err != nil {
		w.markCachedLocked()
		return fmt.Errorf("body_cache: close tmp %s: %w", w.tmp, err)
	}
	if err := os.Rename(w.tmp, w.path); err != nil {
		w.markCachedLocked()
		return fmt.Errorf("body_cache: rename %s: %w", w.path, err)
	}
	w.markCachedLocked()
	return nil
}

func (w *BodyCacheWriter) Preserve() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		w.markCachedLocked()
		return nil
	}
	fp := w.file
	w.file = nil
	if err := fp.Close(); err != nil {
		w.markCachedLocked()
		return fmt.Errorf("body_cache: close tmp %s: %w", w.tmp, err)
	}
	w.markCachedLocked()
	return nil
}

func (w *BodyCacheWriter) Abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	_ = os.Remove(w.tmp)
}

func (c *BodyCache) Read(id uint64, kind string) ([]byte, error) {
	data, err := os.ReadFile(c.filePath(id, kind))
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, stdfs.ErrNotExist) {
		return nil, err
	}
	return os.ReadFile(c.tmpFilePath(id, kind))
}

func (c *BodyCache) Reader(id uint64, kind string) (io.ReadCloser, int64, error) {
	fp, err := os.Open(c.filePath(id, kind))
	if errors.Is(err, stdfs.ErrNotExist) {
		fp, err = os.Open(c.tmpFilePath(id, kind))
	}
	if err != nil {
		return nil, 0, err
	}
	fi, err := fp.Stat()
	if err != nil {
		fp.Close()
		return nil, 0, err
	}
	return fp, fi.Size(), nil
}

func (c *BodyCache) Has(id uint64, kind string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cached[id]&kindBit(kind) != 0
}

func (c *BodyCache) Delete(id uint64) {
	c.DeleteKind(id, KindRequest)
	c.DeleteKind(id, KindResponse)
}

func (c *BodyCache) DeleteKind(id uint64, kind string) {
	c.mu.Lock()
	c.cached[id] &^= kindBit(kind)
	if c.cached[id] == 0 {
		delete(c.cached, id)
	}
	c.mu.Unlock()
	_ = os.Remove(c.tmpFilePath(id, kind))
	_ = os.Remove(c.filePath(id, kind))
}

func (c *BodyCache) SessionDir() string {
	return c.sessionDir
}
