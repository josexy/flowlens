package proxyservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
