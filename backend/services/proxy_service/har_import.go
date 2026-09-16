package proxyservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
)

const HARImportMaxFileSize int64 = 512 << 20

type harImportLimits struct {
	fileBytes, valueBytes int64
	entries               int
}

var defaultHARImportLimits = harImportLimits{HARImportMaxFileSize, 32 << 20, 100_000}
var errEmptyHARImport = errors.New("har: no importable entries")

type HARImportDiagnostic struct {
	Entry int    `json:"entry"`
	Code  string `json:"code"`
}

type HARImportResult struct {
	Metadata      *HistoryMetadata      `json:"metadata,omitempty"`
	Imported      int                   `json:"imported"`
	Skipped       int                   `json:"skipped"`
	MissingBodies int                   `json:"missingBodies"`
	Diagnostics   []HARImportDiagnostic `json:"diagnostics"`
}

func (r *HARImportResult) diagnose(entry int, code string) {
	if len(r.Diagnostics) < 20 {
		r.Diagnostics = append(r.Diagnostics, HARImportDiagnostic{Entry: entry, Code: code})
	}
}

// ImportHARHistory writes an independent HBIN v2 history. The caller owns the
// history storage lock, including exclusion from transaction recovery.
//
//wails:ignore
func ImportHARHistory(ctx context.Context, source io.Reader, directory string, metadata HistoryMetadata) (HARImportResult, error) {
	return importHARHistory(ctx, source, directory, metadata, defaultHARImportLimits, os.Rename)
}

func importHARHistory(ctx context.Context, source io.Reader, directory string, metadata HistoryMetadata, limits harImportLimits, rename func(string, string) error) (HARImportResult, error) {
	result := HARImportResult{Diagnostics: []HARImportDiagnostic{}}
	if metadata.Key == "" || filepath.Base(metadata.Key) != metadata.Key || bytes.ContainsAny([]byte(metadata.Key), `/\`) {
		return result, errors.New("har: invalid history key")
	}
	if err := fs.EnsurePrivateDir(directory); err != nil {
		return result, err
	}
	targets := historyFilePairPaths{
		data:  filepath.Join(directory, fs.GetHBinFileName(metadata.Key)),
		index: filepath.Join(directory, fs.GetHIdxFileName(metadata.Key)),
	}
	// An import must never replace an existing capture, even on a key collision.
	for _, path := range append(targets.values(), targets.backups().values()...) {
		if _, err := os.Lstat(path); err == nil {
			return result, errors.New("har: history key already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
	}
	err := writeAndCommitHistoryFilePair(targets, func(data, index *os.File) error {
		if err := encodeHistoryMetadata(data, metadata); err != nil {
			return err
		}
		countOffset, err := data.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if err := binary.Write(data, binary.BigEndian, uint32(0)); err != nil {
			return err
		}
		if err := binary.Write(index, binary.BigEndian, uint32(0)); err != nil {
			return err
		}
		err = readHARImport(ctx, source, limits, func(raw []byte, ordinal int) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry, body, diagnostics, err := convertHARImportEntry(raw, uint64(ordinal))
			if err != nil {
				result.Skipped++
				result.diagnose(ordinal, "invalid_entry")
				return nil
			}
			defer body.closeReqBodyReaderSafely()
			defer body.closeRspBodyReaderSafely()
			for _, code := range diagnostics {
				result.diagnose(ordinal, code)
			}
			if body.RequestBodyUnavailable {
				result.MissingBodies++
			}
			if body.ResponseBodyUnavailable {
				result.MissingBodies++
			}
			if err := encodeTrafficEntry(index, data, entry, &body); err != nil {
				return err
			}
			end, err := data.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			if end > int64(^uint32(0)) {
				return errors.New("hbin: imported history exceeds 32-bit offsets")
			}
			result.Imported++
			return nil
		})
		if err != nil {
			return err
		}
		if result.Imported == 0 {
			return errEmptyHARImport
		}
		if _, err := data.Seek(countOffset, io.SeekStart); err != nil {
			return err
		}
		if err := binary.Write(data, binary.BigEndian, uint32(result.Imported)); err != nil {
			return err
		}
		if _, err := index.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return binary.Write(index, binary.BigEndian, uint32(result.Imported))
	}, rename, func(string) error { return ctx.Err() })
	if errors.Is(err, errEmptyHARImport) {
		return result, nil
	}
	if err != nil {
		return HARImportResult{}, err
	}
	metadata.Total, metadata.FormatVersion = result.Imported, hbinVersionCurrent
	result.Metadata = &metadata
	return result, nil
}

// The movable read boundary bounds allocations BEFORE ReadValue buffers a
// potentially large JSON string. The boundary uses consumed InputOffset while
// read includes lookahead, charging buffered bytes to the next value as well.
type harImportReader struct {
	ctx                  context.Context
	source               io.Reader
	read, end, fileLimit int64
}

func (r *harImportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	remaining := min(r.end, r.fileLimit) - r.read
	if remaining <= 0 {
		return 0, errors.New("har: JSON input exceeds import size limit")
	}
	n, err := r.source.Read(p[:min(int64(len(p)), remaining, 32<<10)])
	r.read += int64(n)
	return n, err
}

type harImportStream struct {
	decoder *jsontext.Decoder
	reader  *harImportReader
	limits  harImportLimits
}

func (s *harImportStream) limit()                         { s.reader.end = s.decoder.InputOffset() + s.limits.valueBytes }
func (s *harImportStream) token() (jsontext.Token, error) { s.limit(); return s.decoder.ReadToken() }
func (s *harImportStream) value() ([]byte, error)         { s.limit(); return s.decoder.ReadValue() }
func (s *harImportStream) skip() error                    { s.limit(); return s.decoder.SkipValue() }
func (s *harImportStream) expect(kind jsontext.Kind) error {
	t, err := s.token()
	if err != nil {
		return err
	}
	if t.Kind() != kind {
		return fmt.Errorf("har: expected %c", kind)
	}
	return nil
}

func (s *harImportStream) object(field func(string) error) error {
	if err := s.expect('{'); err != nil {
		return err
	}
	for {
		t, err := s.token()
		if err != nil {
			return err
		}
		if t.Kind() == '}' {
			return nil
		}
		if t.Kind() != '"' {
			return errors.New("har: expected object key")
		}
		if err := field(t.String()); err != nil {
			return err
		}
	}
}

func readHARImport(ctx context.Context, source io.Reader, limits harImportLimits, visit func([]byte, int) error) error {
	buffer := bufio.NewReader(source)
	if bom, _ := buffer.Peek(3); bytes.Equal(bom, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = buffer.Discard(3)
	}
	r := &harImportReader{ctx: ctx, source: buffer, fileLimit: limits.fileBytes + 1}
	s := harImportStream{reader: r, limits: limits}
	s.decoder = jsontext.NewDecoder(r)
	seenLog, seenEntries := false, false
	err := s.object(func(key string) error {
		if key != "log" {
			return s.skip()
		}
		seenLog = true
		return s.object(func(key string) error {
			switch key {
			case "version":
				raw, err := s.value()
				if err != nil {
					return err
				}
				var version string
				if err := json.Unmarshal(raw, &version); err != nil {
					return err
				}
				if version != "" && version != "1.1" && version != "1.2" {
					return fmt.Errorf("har: unsupported version %q", version)
				}
				return nil
			case "entries":
				seenEntries = true
				if err := s.expect('['); err != nil {
					return err
				}
				for ordinal := 1; ; ordinal++ {
					s.limit()
					if s.decoder.PeekKind() == ']' {
						return s.expect(']')
					}
					if ordinal > limits.entries {
						return errors.New("har: too many entries")
					}
					raw, err := s.value()
					if err != nil {
						return err
					}
					if int64(len(raw)) > limits.valueBytes {
						return errors.New("har: entry exceeds size limit")
					}
					if err := visit(raw, ordinal); err != nil {
						return err
					}
				}
			default:
				return s.skip()
			}
		})
	})
	if err != nil {
		return fmt.Errorf("har: parse archive: %w", err)
	}
	if !seenLog || !seenEntries {
		return errors.New("har: log.entries is required")
	}
	if _, err := s.token(); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("har: unexpected data after archive")
	}
	if r.read > limits.fileBytes {
		return errors.New("har: archive exceeds file size limit")
	}
	return ctx.Err()
}
