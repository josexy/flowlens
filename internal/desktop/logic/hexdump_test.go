package logic

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestEstimateDecodedByteLength(t *testing.T) {
	for _, input := range []string{"", "ASCII", "中文", "😀", "a😀中"} {
		if got := EstimateDecodedByteLength(input, ""); got != int64(len(input)) {
			t.Fatalf("%q: %d", input, got)
		}
	}
	for length := range 20 {
		encoded := base64.StdEncoding.EncodeToString(make([]byte, length))
		if got := EstimateDecodedByteLength(encoded, "base64"); got != int64(length) {
			t.Fatalf("%d: %d", length, got)
		}
	}
}

func TestByteStorageAcrossBoundaries(t *testing.T) {
	var store HexBytes
	prefix := make([]byte, 65536)
	for i := range prefix {
		prefix[i] = byte(i)
	}
	store.Append(prefix)
	for i := range 70000 {
		store.Append([]byte{byte(i)})
	}
	if store.Len() != 135536 {
		t.Fatalf("length %d", store.Len())
	}
	for _, index := range []int64{0, 255, 65535, 65536, 65537, 131071, 135535} {
		if got, ok := store.Get(index); !ok || got != byte(index) {
			t.Fatalf("%d: %d %v", index, got, ok)
		}
	}
	if _, ok := store.Get(-1); ok {
		t.Fatal("-1")
	}
	if _, ok := store.Get(store.Len()); ok {
		t.Fatal("end")
	}
}

func TestByteRowsSurviveWidthChanges(t *testing.T) {
	const length = 100 * 1024 * 1024
	for _, offset := range []int64{0, 10000, 31_234_567, length - 1} {
		for _, columns := range []int{1, 16, 21, 64} {
			row := HexRowOf(offset, length, columns)
			if row*int64(columns) > offset || (row+1)*int64(columns) <= offset {
				t.Fatalf("offset %d columns %d row %d", offset, columns, row)
			}
		}
	}
	if HexRowCount(1024*1024*1024*1024+7, 16) != 68719476737 {
		t.Fatalf("rows %d", HexRowCount(1024*1024*1024*1024+7, 16))
	}
	if HexRowCount(0, 16) != 0 {
		t.Fatal("empty")
	}
}

func TestHexLayout(t *testing.T) {
	for _, width := range []float32{160, 320, 480, 720, 1100, 4000} {
		for _, length := range []int64{3, 0x100000, 100 * 1024 * 1024} {
			layout := NewHexLayout(width, 8.4, length, 14)
			if layout.BytesPerRow < 1 || layout.BytesPerRow > 64 {
				t.Fatalf("bytes per row %d", layout.BytesPerRow)
			}
			if layout.OffsetDigits < 5 {
				t.Fatalf("digits %d", layout.OffsetDigits)
			}
			if layout.BytesPerRow > 1 && layout.TextStart+float32(layout.BytesPerRow)*8.4 > width-14-16 {
				t.Fatalf("width %v length %d overflows", width, length)
			}
			got := HexByteAt(layout.HexStart+4*3*8.4+1, 1, HexColumnHex, layout, 8.4, length)
			want := min(length-1, int64(layout.BytesPerRow)+int64(min(4, layout.BytesPerRow-1)))
			if got != want {
				t.Fatalf("got %d want %d", got, want)
			}
		}
	}
	if NewHexLayout(80, 8.4, 100*1024*1024, 14).BytesPerRow != 1 {
		t.Fatal("narrow")
	}
	if HexByteAt(0, 0, HexColumnHex, NewHexLayout(400, 8, 1, 14), 8, 0) != -1 {
		t.Fatal("empty body")
	}
}

func TestDecodeHexInput(t *testing.T) {
	binary := make([]byte, 257)
	for i := range binary {
		binary[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(binary)
	for _, input := range []string{encoded, strings.ReplaceAll(encoded, "=", ""), encoded[:10] + "\n" + encoded[10:]} {
		got, err := DecodeHexInput(input, "base64")
		if err != nil || string(got) != string(binary) {
			t.Fatalf("%v", err)
		}
	}
	for _, invalid := range []string{"A", "YQ==Yg==", "!!!!"} {
		if _, err := DecodeHexInput(invalid, "base64"); err == nil {
			t.Fatalf("%q accepted", invalid)
		}
	}
	if got, _ := DecodeHexInput("a😀", ""); string(got) != "a😀" {
		t.Fatal(string(got))
	}
}

func sampleBytes() (*HexBytes, []byte) {
	data := make([]byte, 100003)
	for i := range data {
		data[i] = byte(i % 256)
	}
	store := &HexBytes{}
	store.Append(data[:13])
	store.Append(data[13:65537])
	store.Append(data[65537:])
	return store, data
}

func TestSerializeSelection(t *testing.T) {
	store, data := sampleBytes()
	selected := data[11:100001]
	ctx := context.Background()
	got, err := SerializeHexSelection(ctx, store, 11, 100001, HexCopyBase64, false)
	if err != nil || got != base64.StdEncoding.EncodeToString(selected) {
		t.Fatal("base64", err)
	}
	got, err = SerializeHexSelection(ctx, store, 11, 100001, HexCopyHex, false)
	want := strings.ToUpper(strings.Join(splitPairs(hex.EncodeToString(selected)), " "))
	if err != nil || got != want {
		t.Fatal("hex", err)
	}
	sample := &HexBytes{}
	sample.Append([]byte{0, 10, 31, 32, 65, 126, 127, 255})
	got, _ = SerializeHexSelection(ctx, sample, 0, 8, HexCopyText, false)
	if got != "\x00\n\x1f A~\x7fÿ" {
		t.Fatalf("%q", got)
	}
	for piece := range store.Range(11, 100001) {
		if len(piece) > 0x8000 {
			t.Fatal("piece too large")
		}
	}
}

func splitPairs(value string) []string {
	pairs := make([]string, 0, len(value)/2)
	for i := 0; i < len(value); i += 2 {
		pairs = append(pairs, value[i:i+2])
	}
	return pairs
}

func TestUTF8SelectionKeepsCharacters(t *testing.T) {
	text := "\xef\xbb\xbf" + strings.Repeat("a", 32764) + "中🙂\r\n\t尾\x00"
	store := &HexBytes{}
	store.Append([]byte(text))
	ctx := context.Background()
	got, err := SerializeHexSelection(ctx, store, 0, store.Len(), HexCopyText, true)
	if err != nil || got != text {
		t.Fatalf("mismatch: %v", err)
	}
	got, _ = SerializeHexSelection(ctx, store, 0, store.Len(), HexCopyBase64, false)
	if got != base64.StdEncoding.EncodeToString([]byte(text)) {
		t.Fatal("base64")
	}
}

func TestSelectionCancels(t *testing.T) {
	store := &HexBytes{}
	store.Append(make([]byte, 2*1024*1024))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SerializeHexSelection(ctx, store, 0, store.Len(), HexCopyHex, false); !errors.Is(err, ErrHexCopyCanceled) {
		t.Fatal(err)
	}
}

func TestClipboardNullEscaping(t *testing.T) {
	ctx := context.Background()
	unchanged := "你好\xef\xbb\xbf🙂\\x00\r\n\t\x1f\x7f\u0080ÿ"
	if got, _ := EscapeHexClipboardNull(ctx, unchanged); got != unchanged {
		t.Fatal("changed")
	}
	if got, _ := EscapeHexClipboardNull(ctx, ""); got != "" {
		t.Fatal("empty")
	}
	input := strings.Repeat("a", 65535) + "\x00\x00" + unchanged + strings.Repeat("b", 32768) + "\x00"
	if got, _ := EscapeHexClipboardNull(ctx, input); got != strings.ReplaceAll(input, "\x00", `\x00`) {
		t.Fatal("escape")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := EscapeHexClipboardNull(canceled, "\x00"); !errors.Is(err, ErrHexCopyCanceled) {
		t.Fatal(err)
	}
}
