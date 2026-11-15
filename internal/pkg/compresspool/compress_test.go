package compresspool

import (
	"bufio"
	"bytes"
	"compress/flate"
	stdgzip "compress/gzip"
	stdzlib "compress/zlib"
	"encoding/hex"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zstd"
)

func TestEmptyGzipZlibStream(t *testing.T) {
	t.Logf("empty gzip stream:\n%s\n", hex.Dump(emptyGzipStream))
	t.Logf("empty zlib stream:\n%s\n", hex.Dump(emptyZlibStream))
	if looksLikeZlibStream(bufio.NewReader(newEmptyZlibReader())) {
		t.Log("empty zlib stream looks like a zlib stream")
	} else {
		t.Fatal("empty zlib stream does not look like a zlib stream")
	}
}

func TestPooledReadersDecodePayloads(t *testing.T) {
	t.Parallel()

	payload := []byte(strings.Repeat("flowlens-compresspool-", 64))

	testCases := []struct {
		name   string
		encode func([]byte) ([]byte, error)
		open   func(io.Reader) (io.ReadCloser, error)
	}{
		{
			name:   "gzip",
			encode: encodeGzip,
			open:   NewPooledGzipReadCloser,
		},
		{
			name:   "brotli",
			encode: encodeBrotli,
			open:   NewPooledBrotliReadCloser,
		},
		{
			name:   "snappy",
			encode: encodeSnappy,
			open: func(r io.Reader) (io.ReadCloser, error) {
				return NewPooledSnappyReadCloser(r), nil
			},
		},
		{
			name:   "deflate-raw",
			encode: encodeFlate,
			open:   NewPooledDeflateReadCloser,
		},
		{
			name:   "deflate-zlib",
			encode: encodeZlib,
			open:   NewPooledDeflateReadCloser,
		},
		{
			name:   "zstd",
			encode: encodeZstdWithPool,
			open:   NewPooledZstdReadCloser,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := tc.encode(payload)
			if err != nil {
				t.Fatalf("encode payload: %v", err)
			}

			reader, err := tc.open(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("open pooled reader: %v", err)
			}
			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read decoded payload: %v", err)
			}

			if !bytes.Equal(got, payload) {
				t.Fatalf("decoded payload mismatch: got %d bytes, want %d bytes", len(got), len(payload))
			}
		})
	}
}

func TestNewPooledReadCloserCloseOnlyReleasesOnce(t *testing.T) {
	t.Parallel()

	var releaseCount atomic.Int32
	reader := NewPooledReadCloser(io.NopCloser(strings.NewReader("payload")), func() {
		releaseCount.Add(1)
	})

	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if got := releaseCount.Load(); got != 1 {
		t.Fatalf("release count = %d, want 1", got)
	}
}

func TestPooledReadCloserWriteToUsesUnderlyingWriterTo(t *testing.T) {
	t.Parallel()

	source := &writerToReadCloser{data: []byte("writer-to")}
	reader := NewPooledReadCloser(source, nil)

	var dst bytes.Buffer
	written, err := reader.(*pooledReadCloser).WriteTo(&dst)
	if err != nil {
		t.Fatalf("write to: %v", err)
	}

	if written != int64(len(source.data)) {
		t.Fatalf("written = %d, want %d", written, len(source.data))
	}
	if source.writeToCalls != 1 {
		t.Fatalf("writeToCalls = %d, want 1", source.writeToCalls)
	}
	if dst.String() != string(source.data) {
		t.Fatalf("written payload = %q, want %q", dst.String(), string(source.data))
	}
}

func TestLooksLikeZlibStream(t *testing.T) {
	t.Parallel()

	valid, err := encodeZlib([]byte("proxy"))
	if err != nil {
		t.Fatalf("encode zlib payload: %v", err)
	}

	testCases := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "valid-zlib-header", data: valid, want: true},
		{name: "raw-deflate-header", data: mustEncodeForTest(t, encodeFlate, []byte("proxy")), want: false},
		{name: "short-input", data: []byte{0x78}, want: false},
		{name: "invalid-method", data: []byte{0x70, 0x9c}, want: false},
		{name: "invalid-window-size", data: []byte{0x88, 0x1c}, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := looksLikeZlibStream(bufio.NewReader(bytes.NewReader(tc.data)))
			if got != tc.want {
				t.Fatalf("looksLikeZlibStream() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPooledReadersDoNotLeakPreviousState(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		open  func(io.Reader) (io.ReadCloser, error)
		first func([]byte) ([]byte, error)
	}{
		{
			name:  "gzip",
			open:  NewPooledGzipReadCloser,
			first: encodeGzip,
		},
		{
			name:  "brotli",
			open:  NewPooledBrotliReadCloser,
			first: encodeBrotli,
		},
		{
			name: "snappy",
			open: func(r io.Reader) (io.ReadCloser, error) {
				return NewPooledSnappyReadCloser(r), nil
			},
			first: encodeSnappy,
		},
		{
			name:  "deflate-raw",
			open:  NewPooledDeflateReadCloser,
			first: encodeFlate,
		},
		{
			name:  "deflate-zlib",
			open:  NewPooledDeflateReadCloser,
			first: encodeZlib,
		},
		{
			name:  "zstd",
			open:  NewPooledZstdReadCloser,
			first: encodeZstdWithPool,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			firstPayload := []byte("first-payload-" + tc.name)
			secondPayload := []byte("second-payload-" + tc.name)

			readDecodedPayload(t, tc.open, mustEncodeForTest(t, tc.first, firstPayload))
			got := readDecodedPayload(t, tc.open, mustEncodeForTest(t, tc.first, secondPayload))

			if !bytes.Equal(got, secondPayload) {
				t.Fatalf("decoded payload mismatch after reuse: got %q, want %q", string(got), string(secondPayload))
			}
		})
	}
}

func TestAcquireZstdEncoderCanBeReused(t *testing.T) {
	t.Parallel()

	first := []byte(strings.Repeat("first-zstd-", 16))
	second := []byte(strings.Repeat("second-zstd-", 16))

	firstEncoded := mustEncodeForTest(t, encodeZstdWithPool, first)
	secondEncoded := mustEncodeForTest(t, encodeZstdWithPool, second)

	gotFirst := decodeZstdPayload(t, firstEncoded)
	gotSecond := decodeZstdPayload(t, secondEncoded)

	if !bytes.Equal(gotFirst, first) {
		t.Fatalf("first decoded payload mismatch: got %q, want %q", string(gotFirst), string(first))
	}
	if !bytes.Equal(gotSecond, second) {
		t.Fatalf("second decoded payload mismatch: got %q, want %q", string(gotSecond), string(second))
	}
}

func BenchmarkPooledReaders(b *testing.B) {
	payload := bytes.Repeat([]byte("flowlens-compresspool-benchmark-"), 128)

	benchmarks := []struct {
		name   string
		encode func([]byte) ([]byte, error)
		open   func(io.Reader) (io.ReadCloser, error)
	}{
		{name: "gzip", encode: encodeGzip, open: NewPooledGzipReadCloser},
		{name: "brotli", encode: encodeBrotli, open: NewPooledBrotliReadCloser},
		{
			name:   "snappy",
			encode: encodeSnappy,
			open: func(r io.Reader) (io.ReadCloser, error) {
				return NewPooledSnappyReadCloser(r), nil
			},
		},
		{name: "deflate-raw", encode: encodeFlate, open: NewPooledDeflateReadCloser},
		{name: "deflate-zlib", encode: encodeZlib, open: NewPooledDeflateReadCloser},
		{name: "zstd", encode: encodeZstdWithPool, open: NewPooledZstdReadCloser},
	}

	for _, bm := range benchmarks {
		encoded := mustEncodeForBenchmark(b, bm.encode, payload)

		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))

			for i := 0; i < b.N; i++ {
				reader, err := bm.open(bytes.NewReader(encoded))
				if err != nil {
					b.Fatalf("open pooled reader: %v", err)
				}

				if _, err := io.Copy(io.Discard, reader); err != nil {
					b.Fatalf("consume decoded payload: %v", err)
				}
				if err := reader.Close(); err != nil {
					b.Fatalf("close pooled reader: %v", err)
				}
			}
		})
	}
}

func BenchmarkAcquireZstdEncoder(b *testing.B) {
	payload := bytes.Repeat([]byte("flowlens-zstd-encoder-benchmark-"), 128)

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		enc, err := AcquireZstdEncoder(io.Discard)
		if err != nil {
			b.Fatalf("acquire zstd encoder: %v", err)
		}

		if _, err := enc.Write(payload); err != nil {
			b.Fatalf("write zstd payload: %v", err)
		}
		if err := enc.Close(); err != nil {
			b.Fatalf("close zstd encoder: %v", err)
		}

		ReleaseZstdEncoder(enc)
	}
}

func encodeGzip(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := stdgzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeBrotli(payload []byte) ([]byte, error) {
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

func encodeSnappy(payload []byte) ([]byte, error) {
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

func encodeFlate(payload []byte) ([]byte, error) {
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

func encodeZlib(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := stdzlib.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeZstdWithPool(payload []byte) ([]byte, error) {
	var buf bytes.Buffer

	enc, err := AcquireZstdEncoder(&buf)
	if err != nil {
		return nil, err
	}
	defer ReleaseZstdEncoder(enc)

	if _, err := enc.Write(payload); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func decodeZstdPayload(t *testing.T, encoded []byte) []byte {
	t.Helper()

	dec, err := zstd.NewReader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("create zstd decoder: %v", err)
	}
	defer dec.Close()

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("decode zstd payload: %v", err)
	}
	return got
}

func readDecodedPayload(t *testing.T, open func(io.Reader) (io.ReadCloser, error), encoded []byte) []byte {
	t.Helper()

	reader, err := open(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("open pooled reader: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read decoded payload: %v", err)
	}
	return got
}

func mustEncodeForTest(t *testing.T, encode func([]byte) ([]byte, error), payload []byte) []byte {
	t.Helper()

	encoded, err := encode(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return encoded
}

func mustEncodeForBenchmark(b *testing.B, encode func([]byte) ([]byte, error), payload []byte) []byte {
	b.Helper()

	encoded, err := encode(payload)
	if err != nil {
		b.Fatalf("encode payload: %v", err)
	}
	return encoded
}

type writerToReadCloser struct {
	data         []byte
	readOffset   int
	writeToCalls int
}

func (r *writerToReadCloser) Read(p []byte) (int, error) {
	if r.readOffset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.readOffset:])
	r.readOffset += n
	return n, nil
}

func (r *writerToReadCloser) Close() error {
	return nil
}

func (r *writerToReadCloser) WriteTo(w io.Writer) (int64, error) {
	r.writeToCalls++
	n, err := w.Write(r.data)
	return int64(n), err
}
