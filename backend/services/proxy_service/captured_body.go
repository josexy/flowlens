package proxyservice

import (
	"bytes"
	"io"
	"os"
	"unicode/utf8"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
	"github.com/josexy/flowlens/backend/pkg/logger"
)

// capturedBody owns the storage for one request or response body.
//
// The normal lifecycle is:
//  1. start with memory buffer;
//  2. once the next chunk would exceed threshold, create a body-cache writer;
//  3. write the already buffered bytes to disk first, then the new chunk;
//  4. release the memory buffer and stream later chunks directly to disk;
//  5. Close commits the tmp cache file so readers can open it.
//
// If cache is unavailable or a disk write fails, capturedBody falls back to
// memory so callers can still view/replay as much body data as possible.
type capturedBody struct {
	flowID    uint64
	kind      string
	threshold int64
	cache     *bodycache.BodyCache
	// memory is non-nil while the body is still below the threshold, or when
	// disk spooling has failed and we deliberately fall back to memory.
	memory *bytes.Buffer
	// writer is non-nil after the body crosses the threshold and subsequent
	// chunks are being streamed directly to body cache.
	writer        *bodycache.BodyCacheWriter
	cacheDisabled bool
	closed        bool
	utf8Valid     bool
	utf8Pending   [utf8.UTFMax]byte
	utf8PendingN  int
}

func newCapturedBody(flowID uint64, kind string, threshold int64, cache *bodycache.BodyCache) *capturedBody {
	return &capturedBody{
		flowID:    flowID,
		kind:      kind,
		threshold: threshold,
		cache:     cache,
		memory:    new(bytes.Buffer),
		utf8Valid: true,
	}
}

// Write appends a decoded body chunk to the active storage.
//
// It is called under TrafficBodies' request/response lock, so capturedBody does
// not carry its own mutex. Keeping locking outside avoids mixing request and
// response lock order with body-cache cleanup locks.
func (b *capturedBody) Write(p []byte) {
	if b == nil || b.closed || len(p) == 0 {
		return
	}
	b.observeUTF8(p)

	// Spool mode: the body already crossed the threshold, so do not grow the
	// in-memory buffer. Only the unexpected write-failure path falls back.
	if b.writer != nil {
		if n, err := b.writer.Write(p); err != nil {
			logger.G().Warnf("[%d] body cache stream write failed: %v", b.flowID, err)
			b.fallbackToMemory(p[n:])
		}
		return
	}
	if b.memory == nil {
		return
	}

	// Memory mode: stay in memory until this chunk would exceed the threshold
	// or cache is unavailable/disabled.
	if b.cache == nil || b.cacheDisabled || b.threshold <= 0 || int64(b.memory.Len()+len(p)) <= b.threshold {
		b.memory.Write(p)
		return
	}

	// Crossing the threshold: seed the cache writer with bytes already captured
	// in memory, append the current chunk, then release the buffer.
	writer, err := b.cache.Writer(b.flowID, b.kind)
	if err != nil {
		logger.G().Warnf("[%d] body cache stream open failed: %v", b.flowID, err)
		b.cacheDisabled = true
		b.memory.Write(p)
		return
	}
	if _, err := writer.Write(b.memory.Bytes()); err != nil {
		logger.G().Warnf("[%d] body cache stream seed write failed: %v", b.flowID, err)
		writer.Abort()
		b.cacheDisabled = true
		b.memory.Write(p)
		return
	}
	if _, err := writer.Write(p); err != nil {
		logger.G().Warnf("[%d] body cache stream first chunk write failed: %v", b.flowID, err)
		writer.Abort()
		b.cacheDisabled = true
		b.memory.Write(p)
		return
	}

	b.writer = writer
	b.memory = nil
}

func (b *capturedBody) fallbackToMemory(unwritten []byte) {
	// This is an error recovery path. Preserve the tmp cache file first so the
	// bytes already written to disk remain readable, then rebuild a memory copy
	// and continue there. The normal large-body path does not keep this copy.
	if b.writer != nil {
		if err := b.writer.Preserve(); err != nil {
			logger.G().Warnf("[%d] body cache stream preserve failed: %v", b.flowID, err)
		}
		b.writer = nil
	}

	fallback := new(bytes.Buffer)
	if b.cache != nil {
		data, err := b.cache.Read(b.flowID, b.kind)
		if err != nil && !os.IsNotExist(err) {
			logger.G().Warnf("[%d] body cache stream fallback read failed: %v", b.flowID, err)
		}
		if len(data) > 0 {
			fallback.Write(data)
		}
		b.cache.DeleteKind(b.flowID, b.kind)
	}
	fallback.Write(unwritten)
	b.memory = fallback
	b.cacheDisabled = true
}

func (b *capturedBody) Memory() *bytes.Buffer {
	if b == nil {
		return nil
	}
	return b.memory
}

func (b *capturedBody) UTF8Valid() bool {
	return b == nil || b.utf8Valid && b.utf8PendingN == 0
}

func (b *capturedBody) observeUTF8(data []byte) {
	if !b.utf8Valid {
		return
	}
	if b.utf8PendingN == 0 && utf8.Valid(data) {
		return
	}
	if b.utf8PendingN > 0 {
		for len(data) > 0 && !utf8.FullRune(b.utf8Pending[:b.utf8PendingN]) {
			b.utf8Pending[b.utf8PendingN] = data[0]
			b.utf8PendingN++
			data = data[1:]
		}
		if !utf8.FullRune(b.utf8Pending[:b.utf8PendingN]) {
			return
		}
		if runeValue, size := utf8.DecodeRune(b.utf8Pending[:b.utf8PendingN]); runeValue == utf8.RuneError && size == 1 {
			b.utf8Valid = false
			return
		}
		b.utf8PendingN = 0
	}
	for len(data) > 0 {
		if data[0] < utf8.RuneSelf {
			data = data[1:]
			continue
		}
		if !utf8.FullRune(data) {
			b.utf8PendingN = copy(b.utf8Pending[:], data)
			return
		}
		runeValue, size := utf8.DecodeRune(data)
		if runeValue == utf8.RuneError && size == 1 {
			b.utf8Valid = false
			return
		}
		data = data[size:]
	}
}

// Reader opens a read handle for the captured body.
//
// The bool result tells callers whether the reader is backed by body cache. A
// cache-backed reader must keep ProxyService.bodyCacheMu held until Close, so
// cache cleanup cannot remove the file while it is being read.
func (b *capturedBody) Reader() (io.ReadCloser, int64, bool, error) {
	if b == nil {
		return nil, 0, false, nil
	}
	if b.memory != nil {
		data := append([]byte(nil), b.memory.Bytes()...)
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), false, nil
	}
	if b.writer != nil {
		data, err := b.writer.Snapshot()
		if err != nil {
			return nil, 0, false, err
		}
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), false, nil
	}
	if b.cache == nil || !b.cache.Has(b.flowID, b.kind) {
		return nil, 0, false, nil
	}
	reader, size, err := b.cache.Reader(b.flowID, b.kind)
	return reader, size, true, err
}

// Close finalizes capture after EOF or reader close.
func (b *capturedBody) Close() {
	if b == nil || b.closed {
		return
	}
	b.closed = true
	if b.utf8PendingN != 0 {
		b.utf8Valid = false
	}
	if b.writer == nil {
		return
	}
	// Close commits the tmp body file. If commit fails, BodyCacheWriter keeps a
	// preserved tmp entry readable so callers do not silently lose the body.
	if err := b.writer.Close(); err != nil {
		logger.G().Warnf("[%d] body cache stream close failed: %v", b.flowID, err)
	}
	b.writer = nil
}

// Abort discards any partial capture for a deleted/cleared traffic entry.
func (b *capturedBody) Abort() {
	if b == nil || b.closed {
		return
	}
	b.closed = true
	// Abort is used when a traffic entry is deleted/cleared before normal EOF.
	// In that case partial cached bytes should not remain visible.
	if b.writer != nil {
		b.writer.Abort()
		b.writer = nil
	}
	b.memory = nil
}
