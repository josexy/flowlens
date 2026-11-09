package proxyservice

import (
	"bytes"
	"io"
	"testing"
)

func TestEncodeDecodedBodyForContentEncodingRoundTripsSupportedEncodings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
	}{
		{name: "gzip", encoding: "gzip"},
		{name: "brotli", encoding: "br"},
		{name: "snappy", encoding: "snappy"},
		{name: "deflate", encoding: "deflate"},
		{name: "zstd", encoding: "zstd"},
		{name: "chained", encoding: "gzip, br"},
	}

	payload := []byte("decoded request body for resend")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := encodeDecodedBodyForContentEncoding(payload, tt.encoding)
			if err != nil {
				t.Fatalf("encodeDecodedBodyForContentEncoding: %v", err)
			}
			if bytes.Equal(encoded, payload) {
				t.Fatal("encoded body should differ from decoded payload")
			}

			reader, err := getDecodedReader(bytes.NewReader(encoded), tt.encoding)
			if err != nil {
				t.Fatalf("getDecodedReader: %v", err)
			}
			decoded, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read decoded body: %v", err)
			}
			if err := reader.Close(); err != nil {
				t.Fatalf("close decoded reader: %v", err)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatalf("decoded body = %q, want %q", string(decoded), string(payload))
			}
		})
	}
}

func TestEncodeDecodedBodyForContentEncodingKeepsUnsupportedEncodingBody(t *testing.T) {
	t.Parallel()

	payload := []byte("already encoded or unsupported body")
	got, err := encodeDecodedBodyForContentEncoding(payload, "gzip, compress")
	if err != nil {
		t.Fatalf("encodeDecodedBodyForContentEncoding: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body changed for unsupported encoding: got %q want %q", string(got), string(payload))
	}
}
