package bodyspool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const DefaultInlineLimit int64 = 4 * 1024 * 1024

var errStoreClosed = errors.New("body spool store is closed")

type File struct {
	Path string
	Size int64
}

type Payload struct {
	Inline []byte
	File   *File
}

func (p Payload) Size() int64 {
	if p.File != nil {
		return p.File.Size
	}
	return int64(len(p.Inline))
}

func (p Payload) Open() (io.ReadCloser, error) {
	if p.File != nil && p.Inline != nil {
		return nil, errors.New("body spool payload has conflicting representations")
	}
	if p.File != nil {
		if strings.TrimSpace(p.File.Path) == "" {
			return nil, errors.New("body spool payload file path is empty")
		}
		return os.Open(p.File.Path)
	}
	return io.NopCloser(bytes.NewReader(p.Inline)), nil
}

type Store struct {
	mu       sync.Mutex
	root     string
	closed   bool
	handoffs map[string]struct{}
}

func New(prefix string) (*Store, error) {
	if strings.TrimSpace(prefix) == "" {
		prefix = "flowlens-python-body-"
	}
	root, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, fmt.Errorf("create body spool directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("secure body spool directory: %w", err)
	}
	return &Store{root: root, handoffs: make(map[string]struct{})}, nil
}

func (s *Store) Read(
	ctx context.Context,
	r io.Reader,
	inlineLimit int64,
) (Payload, error) {
	if s == nil {
		return Payload{}, errStoreClosed
	}
	if r == nil {
		return Payload{}, errors.New("body spool reader is nil")
	}
	if inlineLimit < 0 {
		return Payload{}, errors.New("body spool inline limit cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return Payload{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Payload{}, errStoreClosed
	}

	prefix, reachedEOF, err := readPrefix(ctx, r, inlineLimit+1)
	if err != nil {
		return Payload{}, err
	}
	if reachedEOF && int64(len(prefix)) <= inlineLimit {
		return Payload{Inline: prefix}, nil
	}

	file, err := s.createFileLocked("body-")
	if err != nil {
		return Payload{}, err
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := writeAll(file, prefix); err != nil {
		return Payload{}, fmt.Errorf("write body spool prefix: %w", err)
	}
	written, err := copyWithContext(ctx, file, r)
	if err != nil {
		return Payload{}, fmt.Errorf("write body spool remainder: %w", err)
	}
	if err := file.Close(); err != nil {
		return Payload{}, fmt.Errorf("close body spool file: %w", err)
	}
	keep = true
	return Payload{File: &File{Path: path, Size: int64(len(prefix)) + written}}, nil
}

func (s *Store) CopyFile(ctx context.Context, path string) (Payload, error) {
	if s == nil {
		return Payload{}, errStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return Payload{}, err
	}
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return Payload{}, errors.New("body spool source must be an absolute regular-file path")
	}
	lstat, err := os.Lstat(path)
	if err != nil {
		return Payload{}, fmt.Errorf("inspect body spool source: %w", err)
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return Payload{}, errors.New("body spool source must be a non-symlink regular file")
	}
	source, err := os.Open(path)
	if err != nil {
		return Payload{}, fmt.Errorf("open body spool source: %w", err)
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil {
		return Payload{}, fmt.Errorf("inspect opened body spool source: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(lstat, openedInfo) {
		return Payload{}, errors.New("body spool source changed while opening")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Payload{}, errStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return Payload{}, err
	}
	destination, err := s.createFileLocked("copy-")
	if err != nil {
		return Payload{}, err
	}
	destinationPath := destination.Name()
	keep := false
	defer func() {
		if !keep {
			_ = destination.Close()
			_ = os.Remove(destinationPath)
		}
	}()
	written, err := copyWithContext(ctx, destination, source)
	if err != nil {
		return Payload{}, fmt.Errorf("copy body spool source: %w", err)
	}
	if err := destination.Close(); err != nil {
		return Payload{}, fmt.Errorf("close copied body spool file: %w", err)
	}
	keep = true
	return Payload{File: &File{Path: destinationPath, Size: written}}, nil
}

func (s *Store) NewHandoffDir() (string, error) {
	if s == nil {
		return "", errStoreClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errStoreClosed
	}
	directory, err := os.MkdirTemp(s.root, "handoff-")
	if err != nil {
		return "", fmt.Errorf("create body spool handoff directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("secure body spool handoff directory: %w", err)
	}
	cleaned := filepath.Clean(directory)
	s.handoffs[cleaned] = struct{}{}
	return cleaned, nil
}

func (s *Store) AdoptFile(
	ctx context.Context,
	handoffDir string,
	path string,
) (Payload, error) {
	if s == nil {
		return Payload{}, errStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return Payload{}, err
	}
	if !filepath.IsAbs(handoffDir) || !filepath.IsAbs(path) {
		return Payload{}, errors.New("body spool handoff paths must be absolute")
	}
	handoffDir = filepath.Clean(handoffDir)
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Payload{}, errStoreClosed
	}
	if _, ok := s.handoffs[handoffDir]; !ok {
		return Payload{}, errors.New("body spool handoff directory is not owned by this store")
	}
	contained, err := pathWithin(handoffDir, path)
	if err != nil {
		return Payload{}, fmt.Errorf("validate body spool handoff path: %w", err)
	}
	if !contained || sameCleanPath(handoffDir, path) {
		return Payload{}, errors.New("body spool handoff file is outside its assigned directory")
	}
	lstat, err := os.Lstat(path)
	if err != nil {
		return Payload{}, fmt.Errorf("inspect body spool handoff file: %w", err)
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return Payload{}, errors.New("body spool handoff must be a non-symlink regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Payload{}, fmt.Errorf("open body spool handoff file: %w", err)
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return Payload{}, fmt.Errorf("inspect opened body spool handoff file: %w", statErr)
	}
	if closeErr != nil {
		return Payload{}, fmt.Errorf("close body spool handoff file: %w", closeErr)
	}
	if !info.Mode().IsRegular() || !os.SameFile(lstat, info) {
		return Payload{}, errors.New("body spool handoff file changed while opening")
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(path)
		return Payload{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return Payload{}, fmt.Errorf("secure body spool handoff file: %w", err)
	}
	return Payload{File: &File{Path: path, Size: info.Size()}}, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	root := s.root
	s.root = ""
	clear(s.handoffs)
	if root == "" {
		return nil
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove body spool directory: %w", err)
	}
	return nil
}

func (s *Store) createFileLocked(pattern string) (*os.File, error) {
	file, err := os.CreateTemp(s.root, pattern)
	if err != nil {
		return nil, fmt.Errorf("create body spool file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		path := file.Name()
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure body spool file: %w", err)
	}
	return file, nil
}

func readPrefix(ctx context.Context, reader io.Reader, limit int64) ([]byte, bool, error) {
	if limit < 0 {
		return nil, false, errors.New("body spool prefix limit overflow")
	}
	var result bytes.Buffer
	chunk := make([]byte, 64*1024)
	for int64(result.Len()) < limit {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := limit - int64(result.Len())
		readBuffer := chunk
		if remaining < int64(len(readBuffer)) {
			readBuffer = readBuffer[:remaining]
		}
		count, err := reader.Read(readBuffer)
		if count > 0 {
			_, _ = result.Write(readBuffer[:count])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return result.Bytes(), true, nil
			}
			return nil, false, err
		}
		if count == 0 {
			return nil, false, io.ErrNoProgress
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return result.Bytes(), false, nil
}

func copyWithContext(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if err := writeAll(writer, buffer[:count]); err != nil {
				return written, err
			}
			written += int64(count)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
		if count == 0 {
			return written, io.ErrNoProgress
		}
	}
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if err != nil {
			return err
		}
		if count <= 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}

func pathWithin(root, path string) (bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
}

func sameCleanPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}
