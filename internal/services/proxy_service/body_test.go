package proxyservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
	"github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

func TestShouldShowBodyAsBinary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		contentType     string
		contentEncoding string
		bodySize        int64
		want            bool
	}{
		{
			name:        "zero body size",
			contentType: "application/octet-stream",
			bodySize:    0,
			want:        false,
		},
		{
			name:        "multipart body",
			contentType: "multipart/form-data; boundary=abc123",
			bodySize:    128,
			want:        true,
		},
		{
			name:        "binary octet stream",
			contentType: "application/octet-stream",
			bodySize:    3,
			want:        true,
		},
		{
			name:            "gzip encoded json body",
			contentType:     "application/json",
			contentEncoding: "gzip",
			bodySize:        128,
			want:            false,
		},
		{
			name:            "multiple supported encoded json body",
			contentType:     "application/json",
			contentEncoding: "gzip, br",
			bodySize:        128,
			want:            false,
		},
		{
			name:            "unknown encoded text body",
			contentType:     "text/plain; charset=utf-8",
			contentEncoding: "compress",
			bodySize:        12,
			want:            true,
		},
		{
			name:            "multiple encoded text body with unknown coding",
			contentType:     "text/plain; charset=utf-8",
			contentEncoding: "gzip, compress",
			bodySize:        12,
			want:            true,
		},
		{
			name:            "identity encoded text body",
			contentType:     "text/plain; charset=utf-8",
			contentEncoding: "identity",
			bodySize:        12,
			want:            false,
		},
		{
			name:        "plain text body",
			contentType: "text/plain; charset=utf-8",
			bodySize:    12,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldShowBodyAsBinary(tt.contentType, tt.contentEncoding, tt.bodySize)
			if got != tt.want {
				t.Fatalf(
					"shouldShowBodyAsBinary(%q, %q, %d) = %t, want %t",
					tt.contentType,
					tt.contentEncoding,
					tt.bodySize,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestIsBinaryContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{
			name:        "application octet stream is binary",
			contentType: "application/octet-stream",
			want:        true,
		},
		{
			name:        "plain text is not binary",
			contentType: "text/plain; charset=utf-8",
			want:        false,
		},
		{
			name:        "svg is not binary",
			contentType: "image/svg+xml",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isBinaryContentType(tt.contentType)
			if got != tt.want {
				t.Fatalf("isBinaryContentType(%q) = %t, want %t", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestGetDecodedReaderCanCloseAndDecodeRepeatedly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		compress func([]byte) ([]byte, error)
		payloads [][]byte
	}{
		{
			name:     "gzip",
			encoding: "gzip",
			compress: compressWithGzip,
			payloads: [][]byte{
				[]byte("first gzip payload"),
				bytes.Repeat([]byte("g"), 128),
			},
		},
		{
			name:     "brotli",
			encoding: "br",
			compress: compressWithBrotli,
			payloads: [][]byte{
				[]byte("first brotli payload"),
				bytes.Repeat([]byte("b"), 128),
			},
		},
		{
			name:     "snappy",
			encoding: "snappy",
			compress: compressWithSnappy,
			payloads: [][]byte{
				[]byte("first snappy payload"),
				bytes.Repeat([]byte("s"), 128),
			},
		},
		{
			name:     "deflate zlib",
			encoding: "deflate",
			compress: compressWithZlib,
			payloads: [][]byte{
				[]byte("first zlib payload"),
				bytes.Repeat([]byte("z"), 128),
			},
		},
		{
			name:     "deflate raw",
			encoding: "deflate",
			compress: compressWithFlate,
			payloads: [][]byte{
				[]byte("first raw flate payload"),
				bytes.Repeat([]byte("f"), 128),
			},
		},
		{
			name:     "zstd",
			encoding: "zstd",
			compress: compressWithZstd,
			payloads: [][]byte{
				[]byte("first zstd payload"),
				bytes.Repeat([]byte("d"), 128),
			},
		},
		{
			name:     "gzip then brotli",
			encoding: "gzip, br",
			compress: func(payload []byte) ([]byte, error) {
				return compressWithChain(payload, compressWithGzip, compressWithBrotli)
			},
			payloads: [][]byte{
				[]byte("first chained payload"),
				bytes.Repeat([]byte("c"), 128),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, want := range tt.payloads {
				compressed, err := tt.compress(want)
				if err != nil {
					t.Fatalf("compress: %v", err)
				}
				reader, err := getDecodedReader(bytes.NewReader(compressed), tt.encoding)
				if err != nil {
					t.Fatalf("getDecodedReader: %v", err)
				}
				got, err := io.ReadAll(reader)
				if err != nil {
					t.Fatalf("ReadAll: %v", err)
				}
				if err := reader.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				if err := reader.Close(); err != nil {
					t.Fatalf("second Close: %v", err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("decoded payload = %q, want %q", got, want)
				}
			}
		})
	}
}

func compressWithGzip(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressWithChain(payload []byte, compressors ...func([]byte) ([]byte, error)) ([]byte, error) {
	data := payload
	for _, compress := range compressors {
		compressed, err := compress(data)
		if err != nil {
			return nil, err
		}
		data = compressed
	}
	return data, nil
}

func compressWithBrotli(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	bw := brotli.NewWriter(&buf)
	if _, err := bw.Write(payload); err != nil {
		return nil, err
	}
	if err := bw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressWithSnappy(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	sw := snappy.NewBufferedWriter(&buf)
	if _, err := sw.Write(payload); err != nil {
		return nil, err
	}
	if err := sw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressWithZlib(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressWithFlate(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(payload); err != nil {
		return nil, err
	}
	if err := fw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressWithZstd(payload []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	return encoder.EncodeAll(payload, nil), nil
}

func TestCapturedBodySeedsCacheWithBufferedBytesWhenThresholdCrosses(t *testing.T) {
	t.Parallel()

	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	body := newCapturedBody(9001, bodycache.KindRequest, 8, cache)

	body.Write([]byte("1234"))
	if cache.Has(9001, bodycache.KindRequest) {
		t.Fatal("body should stay in memory before crossing threshold")
	}
	if got := body.Memory().String(); got != "1234" {
		t.Fatalf("memory body = %q, want buffered prefix", got)
	}

	body.Write([]byte("56789"))
	if body.Memory() != nil {
		t.Fatal("memory buffer should be released after crossing threshold")
	}
	if body.writer == nil {
		t.Fatal("body should switch to cache writer after crossing threshold")
	}
	body.Close()

	got, err := cache.Read(9001, bodycache.KindRequest)
	if err != nil {
		t.Fatalf("cache.Read: %v", err)
	}
	if want := []byte("123456789"); !bytes.Equal(got, want) {
		t.Fatalf("cached body = %q, want %q", got, want)
	}
}

func TestCapturedBodyTracksUTF8AcrossChunkBoundaries(t *testing.T) {
	valid := newCapturedBody(1, bodycache.KindResponse, 0, nil)
	encoded := []byte("前")
	valid.Write(encoded[:1])
	if valid.UTF8Valid() {
		t.Fatal("incomplete UTF-8 sequence should not be exposed as text")
	}
	valid.Write(encoded[1:])
	valid.Close()
	if !valid.UTF8Valid() {
		t.Fatal("valid UTF-8 split across chunks was rejected")
	}

	invalid := newCapturedBody(2, bodycache.KindResponse, 0, nil)
	invalid.Write([]byte{0xff, 0x00})
	invalid.Close()
	if invalid.UTF8Valid() {
		t.Fatal("invalid UTF-8 body was accepted as text")
	}
}

func TestExportHARKeepsReadableBodyWhenOtherCacheFileIsMissing(t *testing.T) {
	for _, missingKind := range []string{bodycache.KindRequest, bodycache.KindResponse} {
		t.Run(missingKind, func(t *testing.T) {
			service, entry, cache, requestData, responseData := newPartialBodyFailureFixture(t)
			removeCachedBodyFileWithoutUpdatingIndex(t, cache, entry.ID, missingKind)

			target := filepath.Join(t.TempDir(), "partial.har")
			result, err := service.ExportHAR(HARExportRequest{Path: target})
			if err != nil {
				t.Fatalf("ExportHAR: %v", err)
			}
			if result != (HARWriteResult{Exported: 1, MissingBodies: 1}) {
				t.Fatalf("result = %+v", result)
			}

			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("ReadFile HAR: %v", err)
			}
			var document struct {
				Log struct {
					Entries []struct {
						Request struct {
							PostData *struct {
								Text string `json:"text"`
							} `json:"postData"`
						} `json:"request"`
						Response struct {
							Content struct {
								Size int64   `json:"size"`
								Text *string `json:"text"`
							} `json:"content"`
						} `json:"response"`
					} `json:"entries"`
				} `json:"log"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("Unmarshal HAR: %v", err)
			}
			if len(document.Log.Entries) != 1 {
				t.Fatalf("entries = %d", len(document.Log.Entries))
			}
			got := document.Log.Entries[0]
			if missingKind == bodycache.KindRequest {
				if got.Request.PostData != nil {
					t.Fatalf("missing request postData = %+v, want omitted", got.Request.PostData)
				}
				if got.Response.Content.Text == nil || *got.Response.Content.Text != string(responseData) || got.Response.Content.Size != int64(len(responseData)) {
					t.Fatalf("readable response content = %+v", got.Response.Content)
				}
			} else {
				if got.Request.PostData == nil || got.Request.PostData.Text != string(requestData) {
					t.Fatalf("readable request postData = %+v", got.Request.PostData)
				}
				if got.Response.Content.Text != nil || got.Response.Content.Size != -1 {
					t.Fatalf("missing response content = %+v", got.Response.Content)
				}
			}
		})
	}
}

func TestHistoryFlushKeepsReadableBodyWhenOtherCacheFileIsMissing(t *testing.T) {
	for _, missingKind := range []string{bodycache.KindRequest, bodycache.KindResponse} {
		t.Run(missingKind, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("APPDATA", configDir)
			t.Setenv("XDG_CONFIG_HOME", configDir)
			service, entry, cache, requestData, responseData := newPartialBodyFailureFixture(t)
			removeCachedBodyFileWithoutUpdatingIndex(t, cache, entry.ID, missingKind)

			if err := service.flushHistoryToDisk(true); err != nil {
				t.Fatalf("flushHistoryToDisk: %v", err)
			}
			historyDir, err := getHistoryStoragePath()
			if err != nil {
				t.Fatalf("getHistoryStoragePath: %v", err)
			}
			hbinPath := filepath.Join(historyDir, fs.GetHBinFileName(service.currentHistoryMetadata.Key))
			file, err := os.Open(hbinPath)
			if err != nil {
				t.Fatalf("Open history: %v", err)
			}
			defer file.Close()

			metadata, err := DecodeHistoryMetadata(file)
			if err != nil {
				t.Fatalf("DecodeHistoryMetadata: %v", err)
			}
			if metadata.FormatVersion != hbinVersionCurrent || metadata.Total != 1 {
				t.Fatalf("metadata = %+v", metadata)
			}
			persistedEntry, err := DecodeTrafficEntryWithVersion(file, metadata.FormatVersion)
			if err != nil {
				t.Fatalf("DecodeTrafficEntryWithVersion: %v", err)
			}
			if persistedEntry.ID != entry.ID {
				t.Fatalf("persisted entry ID = %d, want %d", persistedEntry.ID, entry.ID)
			}
			requestBody, responseBody, err := DecodeTrafficHARBodies(file, metadata.FormatVersion)
			if err != nil {
				t.Fatalf("DecodeTrafficHARBodies: %v", err)
			}
			if missingKind == bodycache.KindRequest {
				if requestBody.Available || responseBody.Available != true || string(responseBody.Data) != string(responseData) {
					t.Fatalf("persisted bodies = request %+v, response %+v", requestBody, responseBody)
				}
			} else {
				if !requestBody.Available || string(requestBody.Data) != string(requestData) || responseBody.Available {
					t.Fatalf("persisted bodies = request %+v, response %+v", requestBody, responseBody)
				}
			}
		})
	}
}

func newPartialBodyFailureFixture(t *testing.T) (*ProxyService, *TrafficEntry, *bodycache.BodyCache, []byte, []byte) {
	t.Helper()
	service := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("bodycache.NewWithDir: %v", err)
	}
	service.bodyCache = cache

	requestData := []byte("request payload")
	responseData := []byte("response payload")
	started := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	entry := service.newTrafficEntry(TrafficEntry{
		Type:       "https",
		StartedAt:  started,
		Method:     "POST",
		URL:        "https://example.test/data",
		Host:       "example.test",
		Path:       "/data",
		StatusCode: 200,
		Status:     "200 OK",
		Request: &HTTPMessage{
			Proto:        "HTTP/1.1",
			HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "text/plain; charset=utf-8"}},
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: started.UnixMicro(),
				EndedAtMicros:   started.Add(time.Millisecond).UnixMicro(),
				HeaderSize:      41,
				BodySize:        int64(len(requestData)),
				State:           HTTPMessageStateCompleted,
			},
		},
		Response: &HTTPMessage{
			Proto:        "HTTP/1.1",
			HeaderFields: []HTTPHeaderField{{Name: "Content-Type", Value: "text/plain; charset=utf-8"}},
			Metrics: &HTTPMessageMetrics{
				StartedAtMicros: started.Add(2 * time.Millisecond).UnixMicro(),
				EndedAtMicros:   started.Add(3 * time.Millisecond).UnixMicro(),
				HeaderSize:      41,
				BodySize:        int64(len(responseData)),
				State:           HTTPMessageStateCompleted,
			},
		},
	})
	service.storeTrafficEntry(entry)
	service.trafficBodies.Store(entry.ID, &TrafficBodies{})
	if err := cache.Write(entry.ID, bodycache.KindRequest, requestData); err != nil {
		t.Fatalf("cache.Write request: %v", err)
	}
	if err := cache.Write(entry.ID, bodycache.KindResponse, responseData); err != nil {
		t.Fatalf("cache.Write response: %v", err)
	}
	return service, entry, cache, requestData, responseData
}

func removeCachedBodyFileWithoutUpdatingIndex(t *testing.T, cache *bodycache.BodyCache, id uint64, kind string) {
	t.Helper()
	path := filepath.Join(cache.SessionDir(), fmt.Sprintf("%d_%s.body", id, kind))
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove cache file: %v", err)
	}
	if !cache.Has(id, kind) {
		t.Fatalf("cache index no longer records %s after external file removal", kind)
	}
}
