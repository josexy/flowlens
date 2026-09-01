package proxyservice

import (
	"bytes"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

func TestShouldEncodeRequestBodyForTrafficView(t *testing.T) {
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

			got := shouldEncodeRequestBodyForTrafficView(tt.contentType, tt.contentEncoding, tt.bodySize)
			if got != tt.want {
				t.Fatalf(
					"shouldEncodeRequestBodyForTrafficView(%q, %q, %d) = %t, want %t",
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
