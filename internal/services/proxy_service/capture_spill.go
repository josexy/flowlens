package proxyservice

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	appfs "github.com/josexy/flowlens/internal/pkg/fs"
)

// captureSpillSegmentLimit is the size after which the spill store starts a
// new segment. It keeps every HBIN offset inside a segment far below the
// uint32 limit.
const captureSpillSegmentLimit = 1 << 30

// errSpillEntryNotFound reports an ID the spill store does not hold.
var errSpillEntryNotFound = errors.New("spilled traffic entry not found")

type spillKind uint8

const (
	spillKindOther spillKind = iota
	spillKindHTTP
	spillKindWebSocket
	spillKindTCP
)

func spillKindOf(entryType string) spillKind {
	switch entryType {
	case "http", "https":
		return spillKindHTTP
	case "ws", "wss":
		return spillKindWebSocket
	case "tcp":
		return spillKindTCP
	default:
		return spillKindOther
	}
}

// spillRecord locates one spilled entry: its HBIN v3 header at header, its
// body record at body, and the end of both at end, in segment.
type spillRecord struct {
	id      uint64
	header  uint32
	body    uint32
	end     uint32
	segment uint16
	kind    spillKind
}

type spillSegment struct {
	path string
	file *os.File
	size int64
}

// captureSpillStore holds finished capture entries that left the in-memory
// window. Each entry is written with hbinWriteEntry into a segment file of the
// session's body-cache directory, and an ID-sorted index of about 24 bytes per
// entry locates it.
//
// Append, Delete, Reset, and Close are serialized by writeMu. Reads may run at
// any time: they see only entries whose bytes are completely written.
type captureSpillStore struct {
	dir          string
	segmentLimit int64

	writeMu sync.Mutex
	// mu guards records and segments. Writers hold it only to publish a change.
	mu       sync.RWMutex
	records  []spillRecord
	segments []*spillSegment
}

func newCaptureSpillStore(dir string) *captureSpillStore {
	return &captureSpillStore{dir: dir, segmentLimit: captureSpillSegmentLimit}
}

// Len returns how many entries the store holds.
func (s *captureSpillStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// IDs returns the held entry IDs in ascending order.
func (s *captureSpillStore) IDs() []uint64 {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]uint64, len(s.records))
	for i, record := range s.records {
		ids[i] = record.id
	}
	return ids
}

// Has reports whether the store holds id.
func (s *captureSpillStore) Has(id uint64) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, found := s.findLocked(id)
	return found
}

func (s *captureSpillStore) findLocked(id uint64) (int, bool) {
	return slices.BinarySearchFunc(s.records, id, func(record spillRecord, id uint64) int {
		switch {
		case record.id < id:
			return -1
		case record.id > id:
			return 1
		default:
			return 0
		}
	})
}

func (s *captureSpillStore) lookup(id uint64) (spillRecord, *spillSegment, error) {
	if s == nil {
		return spillRecord{}, nil, errSpillEntryNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, found := s.findLocked(id)
	if !found {
		return spillRecord{}, nil, errSpillEntryNotFound
	}
	record := s.records[index]
	return record, s.segments[record.segment], nil
}

// Append writes entry and its bodies to the active segment and indexes it.
// Readers of bv are consumed but not closed.
func (s *captureSpillStore) Append(entry *TrafficEntry, bv *trafficBodyViewInner) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	segment, segmentIndex, err := s.activeSegmentWriteLocked()
	if err != nil {
		return err
	}
	writer := newSegmentWriter(segment.file, segment.size)
	var index bytes.Buffer
	writeErr := hbinWriteEntry(&index, writer, entry, bv)
	if writeErr == nil {
		writeErr = writer.Flush()
	}
	end := writer.pos
	if writeErr == nil && end > int64(^uint32(0)) {
		writeErr = fmt.Errorf("spill: entry %d ends past the segment offset limit", entry.ID)
	}
	if writeErr != nil {
		// Leave no partial entry: the next append overwrites these bytes.
		_ = segment.file.Truncate(segment.size)
		return writeErr
	}
	record := spillRecord{
		id:      entry.ID,
		header:  binary.BigEndian.Uint32(index.Bytes()[8:12]),
		body:    binary.BigEndian.Uint32(index.Bytes()[12:16]),
		end:     uint32(end),
		segment: segmentIndex,
		kind:    spillKindOf(entry.Type),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	segment.size = end
	position, found := s.findLocked(record.id)
	if found {
		s.records[position] = record
		return nil
	}
	// IDs arrive almost in order, so this is an append or a short shift.
	s.records = slices.Insert(s.records, position, record)
	return nil
}

func (s *captureSpillStore) activeSegmentWriteLocked() (*spillSegment, uint16, error) {
	s.mu.RLock()
	count := len(s.segments)
	var active *spillSegment
	if count > 0 {
		active = s.segments[count-1]
	}
	s.mu.RUnlock()
	if active != nil && active.size < s.segmentLimit {
		return active, uint16(count - 1), nil
	}
	if count >= int(^uint16(0)) {
		return nil, 0, errors.New("spill: too many segments")
	}
	path := filepath.Join(s.dir, fmt.Sprintf("spill-%04d.hbin", count))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, appfs.PrivateFileMode)
	if err != nil {
		return nil, 0, fmt.Errorf("spill: create segment: %w", err)
	}
	if err := appfs.EnsurePrivateFile(path); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, 0, fmt.Errorf("spill: secure segment: %w", err)
	}
	segment := &spillSegment{path: path, file: file}
	s.mu.Lock()
	s.segments = append(s.segments, segment)
	s.mu.Unlock()
	return segment, uint16(count), nil
}

// Entry decodes the header of a spilled entry.
func (s *captureSpillStore) Entry(id uint64) (*TrafficEntry, error) {
	record, segment, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(io.NewSectionReader(segment.file, int64(record.header), int64(record.body-record.header)))
	return DecodeTrafficEntryWithVersion(reader, hbinVersionCurrent)
}

// BodyReader returns the HBIN body record of a spilled entry, for decoders
// that stream it.
func (s *captureSpillStore) BodyReader(id uint64) (io.Reader, error) {
	record, segment, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return bufio.NewReader(io.NewSectionReader(segment.file, int64(record.body), int64(record.end-record.body))), nil
}

// Body decodes the bodies and WebSocket messages of a spilled entry.
func (s *captureSpillStore) Body(id uint64) (*TrafficBodyView, error) {
	reader, err := s.BodyReader(id)
	if err != nil {
		return nil, err
	}
	return DecodeTrafficBodyWithVersion(reader, hbinVersionCurrent)
}

// CopyRaw copies the encoded header and body of a spilled entry to dst without
// decoding them. bodyRel is where the body record starts within the copied
// bytes, and n how many bytes were copied.
func (s *captureSpillStore) CopyRaw(dst io.Writer, id uint64) (bodyRel int64, n int64, err error) {
	record, segment, err := s.lookup(id)
	if err != nil {
		return 0, 0, err
	}
	size := int64(record.end - record.header)
	n, err = io.Copy(dst, io.NewSectionReader(segment.file, int64(record.header), size))
	if err == nil && n != size {
		err = io.ErrUnexpectedEOF
	}
	return int64(record.body - record.header), n, err
}

// Delete removes a spilled entry and overwrites its bytes with zeros. It
// reports the kind of the removed entry.
func (s *captureSpillStore) Delete(id uint64) (spillKind, bool) {
	if s == nil {
		return spillKindOther, false
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	index, found := s.findLocked(id)
	if !found {
		return spillKindOther, false
	}
	record := s.records[index]
	s.records = slices.Delete(s.records, index, index+1)
	segment := s.segments[record.segment]
	zeros := make([]byte, min(int64(record.end-record.header), 64<<10))
	for offset := int64(record.header); offset < int64(record.end); {
		chunk := zeros[:min(int64(len(zeros)), int64(record.end)-offset)]
		if _, err := segment.file.WriteAt(chunk, offset); err != nil {
			break
		}
		offset += int64(len(chunk))
	}
	return record.kind, true
}

// Reset removes every spilled entry and its segment files.
func (s *captureSpillStore) Reset() error {
	if s == nil {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.closeSegmentsLocked(true)
	s.records = nil
	return err
}

// Close releases the segment files and keeps them on disk.
func (s *captureSpillStore) Close() error {
	if s == nil {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.closeSegmentsLocked(false)
	s.records = nil
	return err
}

func (s *captureSpillStore) closeSegmentsLocked(remove bool) error {
	var errs []error
	for _, segment := range s.segments {
		if err := segment.file.Close(); err != nil {
			errs = append(errs, err)
		}
		if remove {
			if err := os.Remove(segment.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	s.segments = nil
	return errors.Join(errs...)
}

// segmentWriter is the io.WriteSeeker hbinWriteEntry writes through. It
// buffers the many small header writes and flushes before any real seek.
type segmentWriter struct {
	file *os.File
	buf  *bufio.Writer
	pos  int64
	err  error
}

func newSegmentWriter(file *os.File, pos int64) *segmentWriter {
	w := &segmentWriter{file: file, buf: bufio.NewWriterSize(file, 64<<10), pos: pos}
	if _, err := file.Seek(pos, io.SeekStart); err != nil {
		w.err = err
	}
	return w
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.buf.Write(p)
	w.pos += int64(n)
	return n, err
}

func (w *segmentWriter) Seek(offset int64, whence int) (int64, error) {
	if w.err != nil {
		return 0, w.err
	}
	if whence == io.SeekCurrent && offset == 0 {
		return w.pos, nil
	}
	if err := w.buf.Flush(); err != nil {
		return 0, err
	}
	pos, err := w.file.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	w.pos = pos
	return pos, nil
}

func (w *segmentWriter) Flush() error {
	if w.err != nil {
		return w.err
	}
	return w.buf.Flush()
}
