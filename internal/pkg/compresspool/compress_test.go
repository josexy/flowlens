package compresspool

import (
	"bufio"
	"bytes"
	"compress/flate"
	stdgzip "compress/gzip"
	stdzlib "compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"
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

// zstdTestBody returns compressible JSON lines of at least size bytes.
func zstdTestBody(size int) []byte {
	var b bytes.Buffer
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "{\"id\":%d,\"name\":\"user-%d\",\"score\":%d},\n", i, i*7919%100003, i*31%9973)
	}
	return b.Bytes()
}

func TestPooledZstdEncoderUsesBoundedWindow(t *testing.T) {
	t.Parallel()

	body := zstdTestBody(3 << 20)
	// The second encoder can come back from the pool.
	for round := range 2 {
		encoded := mustEncodeForTest(t, encodeZstdWithPool, body)
		var header zstd.Header
		if err := header.Decode(encoded); err != nil {
			t.Fatalf("round %d: decode frame header: %v", round, err)
		}
		if header.WindowSize == 0 || header.WindowSize > zstdEncoderWindow {
			t.Fatalf("round %d: frame window %d, want at most %d", round, header.WindowSize, zstdEncoderWindow)
		}
		if got := readDecodedPayload(t, NewPooledZstdReadCloser, encoded); !bytes.Equal(got, body) {
			t.Fatalf("round %d: decoded %d bytes, want the %d-byte body", round, len(got), len(body))
		}
	}

	// Earlier builds wrote frames with the library's 8 MiB default window.
	var legacy bytes.Buffer
	enc, err := zstd.NewWriter(&legacy, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	var header zstd.Header
	if err := header.Decode(legacy.Bytes()); err != nil || header.WindowSize != 8<<20 {
		t.Fatalf("legacy frame window %d (%v), want 8 MiB", header.WindowSize, err)
	}
	if got := readDecodedPayload(t, NewPooledZstdReadCloser, legacy.Bytes()); !bytes.Equal(got, body) {
		t.Fatalf("legacy frame decoded %d bytes, want the %d-byte body", len(got), len(body))
	}
}

// A frame header sets the window a decoder allocates before decoding anything,
// and a pooled decoder keeps that allocation. Windows over the cap must fail
// without being allocated.
func TestPooledZstdDecoderRejectsWindowsOverCap(t *testing.T) {
	rawBlock := []byte{0x09, 0x00, 0x00, 'A'} // last block, raw, 1 byte
	windowDescriptor := func(windowLog byte) []byte {
		return append([]byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, (windowLog - 10) << 3}, rawBlock...)
	}
	// A single-segment frame's window is its content size.
	singleSegment := binary.LittleEndian.AppendUint64([]byte{0x28, 0xb5, 0x2f, 0xfd, 0xe0}, 256<<20)
	singleSegment = append(singleSegment, rawBlock...)

	for _, tc := range []struct {
		name  string
		frame []byte
	}{
		{name: "512 MiB window descriptor", frame: windowDescriptor(29)},
		{name: "256 MiB single segment", frame: singleSegment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			reader, err := NewPooledZstdReadCloser(bytes.NewReader(tc.frame))
			if err != nil {
				t.Fatalf("open pooled reader: %v", err)
			}
			_, err = io.ReadAll(reader)
			_ = reader.Close()
			runtime.ReadMemStats(&after)
			if !errors.Is(err, zstd.ErrWindowSizeExceeded) && !errors.Is(err, zstd.ErrDecoderSizeExceeded) {
				t.Fatalf("decode error = %v, want a window size error", err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
				t.Fatalf("a %d-byte frame allocated %.1f MiB", len(tc.frame), float64(allocated)/(1<<20))
			}
		})
	}

	if got := readDecodedPayload(t, NewPooledZstdReadCloser, windowDescriptor(23)); string(got) != "A" {
		t.Fatalf("frame with an 8 MiB window decoded %q, want %q", got, "A")
	}
}

// An idle pooled encoder stays in memory until the second collection after
// its release, so what it keeps after a large body matters.
func TestZstdEncoderRetainsBoundedMemoryAfterLargeBody(t *testing.T) {
	body := zstdTestBody(2 << 20)
	// Two collections empty the pool, so the encoder below is new.
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	enc, err := AcquireZstdEncoder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(body); offset += 32 << 10 {
		if _, err := enc.Write(body[offset:min(offset+32<<10, len(body))]); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(enc)
	ReleaseZstdEncoder(enc)
	// About 5 MiB with the 1 MiB window; the 8 MiB default kept about 19 MiB.
	if retained := int64(after.HeapAlloc) - int64(before.HeapAlloc); retained > 8<<20 {
		t.Fatalf("encoder retained %.1f MiB after a %d-byte body, want at most 8 MiB", float64(retained)/(1<<20), len(body))
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
