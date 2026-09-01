package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPRequestPluginResponseBodyKindFromFile(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
		want        string
	}{
		{name: "empty", contentType: "text/plain", want: "none"},
		{
			name:        "utf8 split across validator buffer",
			contentType: "text/plain; charset=utf-8",
			body:        append(bytes.Repeat([]byte("x"), 64*1024-1), []byte("€")...),
			want:        "text",
		},
		{
			name:        "nested json",
			contentType: "application/problem+json",
			body:        []byte(`{"outer":[1,{"inner":true}],"tail":null}`),
			want:        "json",
		},
		{
			name:        "application xml",
			contentType: "application/xml; charset=utf-8",
			body:        []byte(`<root>世界</root>`),
			want:        "xml",
		},
		{
			name:        "xml suffix",
			contentType: "application/soap+xml",
			body:        []byte(`<Envelope/>`),
			want:        "xml",
		},
		{
			name:        "json trailing value falls back to text",
			contentType: "application/json",
			body:        []byte(`{"first":true} {"second":true}`),
			want:        "text",
		},
		{
			name:        "json trailing garbage falls back to text",
			contentType: "application/json",
			body:        []byte(`{"first":true} trailing`),
			want:        "text",
		},
		{
			name:        "invalid utf8",
			contentType: "text/plain",
			body:        []byte{0xff, 0xfe, 0xfd},
			want:        "binary",
		},
		{
			name:        "binary content type",
			contentType: "application/octet-stream",
			body:        []byte("printable but semantically binary"),
			want:        "binary",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "body")
			if err := os.WriteFile(path, test.body, 0o600); err != nil {
				t.Fatal(err)
			}
			kind, err := httpRequestPluginResponseBodyKindFromFile(context.Background(), test.contentType, &HTTPRequestPluginBodyFile{
				Path: path, Name: "body", Size: int64(len(test.body)), ReadOnly: true,
			})
			if err != nil {
				t.Fatalf("httpRequestPluginResponseBodyKindFromFile: %v", err)
			}
			if kind != test.want {
				t.Fatalf("kind = %q, want %q", kind, test.want)
			}
		})
	}
}

func TestHTTPRequestPluginResponseBodyKindFromFileHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 128*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := httpRequestPluginResponseBodyKindFromFile(ctx, "text/plain", &HTTPRequestPluginBodyFile{
		Path: path, Size: 128 * 1024, ReadOnly: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestMaterializeHTTPRequestPluginResponseReader(t *testing.T) {
	value := "hello €"
	for _, test := range []struct {
		name       string
		base64Body bool
		want       string
	}{
		{name: "text", want: value},
		{name: "base64", base64Body: true, want: "aGVsbG8g4oKs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, size, err := materializeHTTPRequestPluginResponseReader(
				context.Background(), strings.NewReader(value), test.base64Body,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || size != int64(len(value)) {
				t.Fatalf("result = %q size=%d, want %q size=%d", got, size, test.want, len(value))
			}
		})
	}
}

func TestMaterializeHTTPRequestPluginResponseReaderPropagatesFailure(t *testing.T) {
	reader := &failingHTTPResponseReader{remaining: []byte("partial")}
	_, _, err := materializeHTTPRequestPluginResponseReader(context.Background(), reader, false)
	if err == nil || !strings.Contains(err.Error(), "materialize response body") {
		t.Fatalf("error = %v", err)
	}
}

type failingHTTPResponseReader struct {
	remaining []byte
}

func (r *failingHTTPResponseReader) Read(value []byte) (int, error) {
	if len(r.remaining) > 0 {
		count := copy(value, r.remaining)
		r.remaining = r.remaining[count:]
		return count, nil
	}
	return 0, errors.New("fixture read failure")
}
