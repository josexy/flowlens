package logic

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"math"
	"strings"
)

const (
	hexRangeChunk = 0x8000

	// HexHorizontalPadding is the padding around the hex view's rows.
	HexHorizontalPadding = 16
	// HexSectionPadding separates the offset, hex, and text columns.
	HexSectionPadding = 8
	// HexMaxBytesPerRow bounds a row.
	HexMaxBytesPerRow = 64
)

// HexSource is the bytes a hex view reads.
type HexSource interface {
	Len() int64
	Get(index int64) (byte, bool)
	Range(start, end int64) iter.Seq[[]byte]
}

// HexString reads a body's raw bytes without copying it.
type HexString string

// Len implements HexSource.
func (s HexString) Len() int64 { return int64(len(s)) }

// Get implements HexSource.
func (s HexString) Get(index int64) (byte, bool) {
	if index < 0 || index >= int64(len(s)) {
		return 0, false
	}
	return s[index], true
}

// Range implements HexSource.
func (s HexString) Range(start, end int64) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		end = min(int64(len(s)), end)
		for offset := start; offset < end; offset += hexRangeChunk {
			if !yield([]byte(s[offset:min(end, offset+hexRangeChunk)])) {
				return
			}
		}
	}
}

// HexLayout is how a width of the hex view divides into columns.
type HexLayout struct {
	BytesPerRow  int
	OffsetDigits int
	OffsetWidth  float32
	HexStart     float32
	TextStart    float32
}

// NewHexLayout returns the columns that fit width.
func NewHexLayout(width, charWidth float32, byteLength int64, gutter float32) HexLayout {
	digits := max(5, len(fmt.Sprintf("%x", max(0, byteLength-1))))
	offsetWidth := float32(digits+1)*charWidth + 1
	available := width - gutter - HexHorizontalPadding - offsetWidth - HexSectionPadding*3 - 1
	perRow := max(1, min(HexMaxBytesPerRow, int(math.Floor(float64(available/(4*charWidth))))))
	hexStart := offsetWidth + HexSectionPadding
	return HexLayout{
		BytesPerRow: perRow, OffsetDigits: digits, OffsetWidth: offsetWidth, HexStart: hexStart,
		TextStart: hexStart + float32(perRow)*3*charWidth + HexSectionPadding*2 + 1,
	}
}

// HexColumn names a column of bytes.
type HexColumn int

// The byte columns.
const (
	HexColumnHex HexColumn = iota
	HexColumnText
)

// HexRowCount returns how many rows hold byteLength bytes.
func HexRowCount(byteLength int64, bytesPerRow int) int64 {
	if byteLength <= 0 {
		return 0
	}
	return (byteLength-1)/int64(bytesPerRow) + 1
}

// HexByteAt returns the byte under x in a row, including gaps and positions
// beyond the row's ends. It returns -1 for an empty body.
func HexByteAt(x float32, row int64, column HexColumn, layout HexLayout, charWidth float32, byteLength int64) int64 {
	if byteLength <= 0 {
		return -1
	}
	start, cellWidth := layout.HexStart, charWidth*3
	if column == HexColumnText {
		start, cellWidth = layout.TextStart, charWidth
	}
	cell := max(0, min(layout.BytesPerRow-1, int(math.Floor(float64((x-start)/cellWidth)))))
	return min(byteLength-1, row*int64(layout.BytesPerRow)+int64(cell))
}

// HexCopyFormat is how a selection is written out.
type HexCopyFormat int

// Selection formats.
const (
	HexCopyText HexCopyFormat = iota
	HexCopyHex
	HexCopyBase64
)

// ErrHexCopyCanceled reports an abandoned copy.
var ErrHexCopyCanceled = errors.New("hex selection copy canceled")

func checkHexContext(ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrHexCopyCanceled
	}
	return nil
}

// SerializeHexSelection writes the selected bytes in format, without
// flattening the whole store. UTF-8 text keeps every character; other text
// maps each byte to the character of that value.
func SerializeHexSelection(ctx context.Context, bytes HexSource, start, end int64, format HexCopyFormat, utf8Text bool) (string, error) {
	var out strings.Builder
	var encoded []byte
	first := true
	for chunk := range bytes.Range(start, end) {
		if err := checkHexContext(ctx); err != nil {
			return "", err
		}
		switch format {
		case HexCopyBase64:
			encoded = append(encoded, chunk...)
		case HexCopyHex:
			for _, value := range chunk {
				if !first {
					out.WriteByte(' ')
				}
				first = false
				out.WriteString(strings.ToUpper(hex.EncodeToString([]byte{value})))
			}
		default:
			if utf8Text {
				out.Write(chunk)
				continue
			}
			for _, value := range chunk {
				out.WriteRune(rune(value))
			}
		}
	}
	if err := checkHexContext(ctx); err != nil {
		return "", err
	}
	switch {
	case format == HexCopyBase64:
		return base64.StdEncoding.EncodeToString(encoded), nil
	case format == HexCopyText && utf8Text:
		return strings.ToValidUTF8(out.String(), "�"), nil
	default:
		return out.String(), nil
	}
}

// EscapeHexClipboardNull writes NUL as \x00, because the Windows text
// clipboard rejects it. Other characters are unchanged.
func EscapeHexClipboardNull(ctx context.Context, text string) (string, error) {
	if err := checkHexContext(ctx); err != nil {
		return "", err
	}
	if !strings.Contains(text, "\x00") {
		return text, nil
	}
	var out strings.Builder
	for offset := 0; offset < len(text); offset += hexRangeChunk {
		if err := checkHexContext(ctx); err != nil {
			return "", err
		}
		out.WriteString(strings.ReplaceAll(text[offset:min(len(text), offset+hexRangeChunk)], "\x00", `\x00`))
	}
	return out.String(), nil
}
