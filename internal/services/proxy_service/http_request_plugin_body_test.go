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
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "text", value: "hello €"},
		{name: "binary", value: "\x00\xff\x80hello\xc3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, size, err := materializeHTTPRequestPluginResponseReader(
				context.Background(), strings.NewReader(test.value),
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.value || size != int64(len(test.value)) {
				t.Fatalf("result = %q size=%d, want %q size=%d", got, size, test.value, len(test.value))
			}
		})
	}
}

func TestMaterializeHTTPRequestPluginResponseReaderPropagatesFailure(t *testing.T) {
	reader := &failingHTTPResponseReader{remaining: []byte("partial")}
	_, _, err := materializeHTTPRequestPluginResponseReader(context.Background(), reader)
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

func TestValidateHTTPRequestPluginBodyFileBySemanticKind(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		content []byte
		wantErr bool
	}{
		{name: "text", kind: "text", content: []byte("hello 世界")},
		{name: "xml", kind: "xml", content: []byte("<root>世界</root>")},
		{name: "split UTF-8", kind: "text", content: append(make([]byte, 64*1024-1), []byte("界")...)},
		{name: "invalid UTF-8 text", kind: "text", content: []byte{0xff}, wantErr: true},
		{name: "JSON", kind: "json", content: []byte(`{"nested":[1,{"ok":true}]}`)},
		{name: "JSON trailing whitespace", kind: "json", content: []byte("[1,2]\n\t")},
		{name: "JSON multiple values", kind: "json", content: []byte(`{} {}`), wantErr: true},
		{name: "JSON trailing garbage", kind: "json", content: []byte(`{} trailing`), wantErr: true},
		{name: "binary accepts arbitrary bytes", kind: "binary", content: []byte{0xff, 0x00}},
		{name: "file accepts arbitrary bytes", kind: "file", content: []byte{0xff, 0x00}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "body.bin")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			file := &HTTPRequestPluginBodyFile{
				Path: path, Name: filepath.Base(path), Size: int64(len(test.content)), ReadOnly: true,
			}
			err := validateHTTPRequestPluginBodyFile(context.Background(), file, test.kind)
			if test.wantErr && err == nil {
				t.Fatal("validation succeeded, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestValidateHTTPRequestPluginBodyFileHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte(`{"value":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := validateHTTPRequestPluginBodyFile(ctx, &HTTPRequestPluginBodyFile{Path: path, Size: 11}, "json")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("validation error = %v, want context.Canceled", err)
	}
}

func TestValidateHTTPRequestPluginRequestRejectsConflictingBodyRepresentations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := HTTPRequestPluginRequest{
		Method: "POST", URL: "https://example.com/",
		Body: SendRequestBody{BodyType: SendRequestBodyTypeText, Text: "inline"},
		BodyFile: &HTTPRequestPluginBodyFile{
			Path: path, Name: "body.txt", Size: 4, ReadOnly: true,
		},
	}
	if _, err := ValidateHTTPRequestPluginRequest(request); err == nil {
		t.Fatal("ValidateHTTPRequestPluginRequest() accepted inline and file Body together")
	}

	request.Body.Text = ""
	validated, err := ValidateHTTPRequestPluginRequest(request)
	if err != nil {
		t.Fatalf("ValidateHTTPRequestPluginRequest() error = %v", err)
	}
	if validated.BodyFile == nil || validated.BodyFile.Size != 4 {
		t.Fatalf("validated BodyFile = %+v", validated.BodyFile)
	}
}
