package proxyservice

import (
	"bytes"
	"testing"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
)

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
