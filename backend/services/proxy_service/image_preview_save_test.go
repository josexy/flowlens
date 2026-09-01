package proxyservice

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeBodyToFileBytesDecodesBase64Binary(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte{0x00, 0x41, 0xff})
	got, err := decodeBodyToFileBytes(SaveBodyToFileRequest{
		Body:         encoded,
		BodyEncoding: "base64",
		ContentType:  "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("decodeBodyToFileBytes returned error: %v", err)
	}
	if string(got) != string([]byte{0x00, 0x41, 0xff}) {
		t.Fatalf("decoded bytes = %v, want original binary bytes", got)
	}
}

func TestDecodeBodyToFileBytesUsesPlainTextBytes(t *testing.T) {
	t.Parallel()

	body := "plain text body"
	got, err := decodeBodyToFileBytes(SaveBodyToFileRequest{
		Body:        body,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("decodeBodyToFileBytes returned error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("decoded bytes = %q, want %q", string(got), body)
	}
}

func TestDecodeBodyToFileBytesPreservesWhitespaceOnlyText(t *testing.T) {
	t.Parallel()

	body := "   \n\t  "
	got, err := decodeBodyToFileBytes(SaveBodyToFileRequest{
		Body:        body,
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("decodeBodyToFileBytes returned error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("decoded bytes = %q, want %q", string(got), body)
	}
}

func TestDecodeBodyToFileBytesRejectsInvalidBase64(t *testing.T) {
	t.Parallel()

	_, err := decodeBodyToFileBytes(SaveBodyToFileRequest{
		Body:         "%not-base64%",
		BodyEncoding: "base64",
		ContentType:  "application/octet-stream",
	})
	if err == nil {
		t.Fatal("decodeBodyToFileBytes error = nil, want invalid base64 error")
	}
}

func TestWriteBodyToFileWritesBytesToDisk(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "body.bin")
	data := []byte{0x01, 0x02, 0x03}

	if err := writeBodyToFile(target, data); err != nil {
		t.Fatalf("writeBodyToFile returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("written bytes = %v, want %v", got, data)
	}
}

func TestSaveBodyToFileWritesDecodedBodyToExplicitPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "payload")
	encoded := base64.StdEncoding.EncodeToString([]byte{0x00, 0x41, 0xff})

	if err := (&ProxyService{}).SaveBodyToFile(SaveBodyToFileRequest{
		Path:         target,
		Body:         encoded,
		BodyEncoding: "base64",
		ContentType:  "application/octet-stream",
	}); err != nil {
		t.Fatalf("SaveBodyToFile returned error: %v", err)
	}

	got, err := os.ReadFile(target + ".bin")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != string([]byte{0x00, 0x41, 0xff}) {
		t.Fatalf("written bytes = %v, want original binary bytes", got)
	}
}

func TestNormalizeBodyToFileSavePathAppendsDefaultExtensionWhenMissing(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	tests := []struct {
		name        string
		contentType string
		path        string
		want        string
	}{
		{
			name:        "append json extension",
			contentType: "application/json",
			path:        filepath.Join(tempDir, "payload-json"),
			want:        filepath.Join(tempDir, "payload-json.json"),
		},
		{
			name:        "append xml extension",
			contentType: "application/xml",
			path:        filepath.Join(tempDir, "payload-xml"),
			want:        filepath.Join(tempDir, "payload-xml.xml"),
		},
		{
			name:        "append html extension",
			contentType: "text/html",
			path:        filepath.Join(tempDir, "payload-html"),
			want:        filepath.Join(tempDir, "payload-html.html"),
		},
		{
			name:        "append text fallback",
			contentType: "text/plain",
			path:        filepath.Join(tempDir, "payload-text"),
			want:        filepath.Join(tempDir, "payload-text.txt"),
		},
		{
			name:        "append png extension",
			contentType: "image/png",
			path:        filepath.Join(tempDir, "preview"),
			want:        filepath.Join(tempDir, "preview.png"),
		},
		{
			name:        "keep existing extension",
			contentType: "image/png",
			path:        filepath.Join(tempDir, "preview.jpeg"),
			want:        filepath.Join(tempDir, "preview.jpeg"),
		},
		{
			name:        "append binary fallback extension",
			contentType: "application/octet-stream",
			path:        filepath.Join(tempDir, "payload"),
			want:        filepath.Join(tempDir, "payload.bin"),
		},
		{
			name:        "append pdf binary fallback extension",
			contentType: "application/pdf",
			path:        filepath.Join(tempDir, "document"),
			want:        filepath.Join(tempDir, "document.bin"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBodyToFileSavePath(tt.contentType, tt.path)
			if got != tt.want {
				t.Fatalf("normalizeBodyToFileSavePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
