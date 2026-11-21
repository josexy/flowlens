package inspect

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

type icoTestFrame struct {
	side int
	bits uint16
	data []byte
}

// icoTestBody builds the container independently of the decoder/encoder library.
func icoTestBody(frames ...icoTestFrame) string {
	data := make([]byte, 6+16*len(frames))
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(frames)))
	for index, frame := range frames {
		entry := data[6+16*index : 6+16*(index+1)]
		entry[0], entry[1] = byte(frame.side), byte(frame.side)
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], frame.bits)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(frame.data)))
		binary.LittleEndian.PutUint32(entry[12:], uint32(len(data)))
		data = append(data, frame.data...)
	}
	return string(data)
}

// icoTestPixels has opaque red, transparent, and blue quadrants. Formats with
// an alpha channel use half-transparent blue to exercise bitmap conversion.
func icoTestPixels(side int, alpha bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			pixel := color.NRGBA{R: 255, A: 255}
			switch {
			case x >= side/2 && y < side/2:
				pixel.A = 0
			case x < side/2 && y >= side/2:
				pixel = color.NRGBA{B: 255, A: 255}
				if alpha {
					pixel.A = 128
				}
			}
			img.SetNRGBA(x, y, pixel)
		}
	}
	return img
}

func icoPNGFrame(t *testing.T, side int) icoTestFrame {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, icoTestPixels(side, true)); err != nil {
		t.Fatal(err)
	}
	return icoTestFrame{side: side, bits: 32, data: out.Bytes()}
}

func icoDIBFrame(side int, bits uint16) icoTestFrame {
	stride, maskStride := (side*int(bits)+31)/32*4, (side+31)/32*4
	paletteSize := 0
	if bits <= 8 {
		paletteSize = 8 // Two RGBQUAD palette entries.
	}
	pixelOffset := 40 + paletteSize
	maskOffset := pixelOffset + stride*side
	data := make([]byte, maskOffset+maskStride*side)
	binary.LittleEndian.PutUint32(data, 40)
	binary.LittleEndian.PutUint32(data[4:], uint32(side))
	binary.LittleEndian.PutUint32(data[8:], uint32(side*2))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], bits)
	binary.LittleEndian.PutUint32(data[20:], uint32(stride*side))
	if paletteSize != 0 {
		binary.LittleEndian.PutUint32(data[32:], 2)
		data[42], data[44] = 255, 255 // Red, blue in BGRA order.
	}
	img := icoTestPixels(side, bits == 32)
	for y := range side {
		row := side - 1 - y
		for x := range side {
			pixel := img.NRGBAAt(x, y)
			if bits <= 8 {
				if pixel.B != 0 {
					bit := x * int(bits)
					data[pixelOffset+row*stride+bit/8] |= 1 << (8 - int(bits) - bit%8)
				}
			} else {
				offset := pixelOffset + row*stride + x*int(bits)/8
				data[offset], data[offset+1], data[offset+2] = pixel.B, pixel.G, pixel.R
				if bits == 32 {
					data[offset+3] = pixel.A
				}
			}
			if pixel.A == 0 {
				data[maskOffset+row*maskStride+x/8] |= 1 << (7 - x%8)
			}
		}
	}
	return icoTestFrame{side: side, bits: bits, data: data}
}

func TestICOPreview(t *testing.T) {
	frames := map[string]icoTestFrame{"png": icoPNGFrame(t, 64)}
	for _, bits := range []uint16{1, 4, 8, 24, 32} {
		frames[fmt.Sprintf("dib%d", bits)] = icoDIBFrame(64, bits)
	}
	for name, frame := range frames {
		for _, contentType := range []string{"image/x-icon", "image/vnd.microsoft.icon"} {
			t.Run(name+"/"+contentType, func(t *testing.T) {
				body := icoTestBody(frame)
				f := bodyFixture(t, contentType, body, true)
				f.tester.SetSize(1200, 650)
				f.settle()
				preview := &f.panel.responseBody.image
				if preview.failed || preview.image.bitmap == nil || preview.width != 64 || preview.height != 64 {
					t.Fatalf("ICO preview failed: failed=%v size=%dx%d", preview.failed, preview.width, preview.height)
				}
				if f.panel.responseBody.Active() != logic.BodyTabImage {
					t.Fatal("ICO did not open Image")
				}
				f.has("ICO | 64 x 64 | " + logic.FormatFileSize(int64(len(body))))
				for _, dark := range []bool{false, true} {
					f.setDark(dark)
					f.imageRects()
				}
				if contentType == "image/x-icon" {
					rendertest.ShootThemes(t, f.tester, "image-ico-"+name, f.setDark)
				}
				f.click(locales.DetailSaveBody())
				if len(f.saved) != 1 || f.saved[0].Body != body {
					t.Fatal("saving changed the original ICO bytes")
				}
				f.click(locales.DetailHex())
				if f.panel.responseBody.hex.body != body {
					t.Fatal("Hex changed the original ICO bytes")
				}
				f.click(locales.DetailPreview())
				// Compare composited pixels, including premultiplied alpha.
				tester := ui.NewTester(func(c *ui.Context) {
					ui.Box(c).Size(64, 64).Background(ui.RGB(0, 0, 0)).Children(func() {
						ui.Image(c, preview.image.bitmap).Size(64, 64)
					})
				}, 64, 64)
				pixels := tester.Image()
				wantBlue := uint8(255)
				if frame.bits == 32 {
					wantBlue = 128
				}
				for point, want := range map[image.Point]color.RGBA{
					{X: 16, Y: 16}: {R: 255, A: 255},
					{X: 48, Y: 16}: {A: 255},
					{X: 16, Y: 48}: {B: wantBlue, A: 255},
				} {
					if got := pixels.RGBAAt(point.X, point.Y); got != want {
						t.Fatalf("pixel at %v = %v, want %v", point, got, want)
					}
				}
			})
		}
	}
}

func TestICOMultipleSizesAnd256Dimension(t *testing.T) {
	small, large := icoDIBFrame(16, 32), icoPNGFrame(t, 256)
	for _, frames := range [][]icoTestFrame{{small, large}, {large, small}} {
		decoded, err := decodeImage(icoTestBody(frames...), false)
		if err != nil {
			t.Fatal(err)
		}
		if w, h := decoded.bitmap.Size(); w != 256 || h != 256 {
			t.Fatalf("size=%dx%d, want 256x256", w, h)
		}
	}
}

func TestICORejectsInvalidBodies(t *testing.T) {
	valid := []byte(icoTestBody(icoDIBFrame(16, 32)))
	cases := map[string][]byte{
		"truncated header":    valid[:4],
		"empty directory":     {0, 0, 1, 0, 0, 0},
		"truncated directory": valid[:21],
		"truncated pixels":    valid[:len(valid)-1],
		"invalid payload":     []byte(icoTestBody(icoTestFrame{side: 16, bits: 32, data: []byte("invalid image")})),
		"oversized PNG":       []byte(icoTestBody(icoPNGFrame(t, 257))),
	}
	for _, test := range []struct {
		name   string
		offset int
		value  uint32
	}{
		{"huge payload", 14, ^uint32(0)},
		{"offset past body", 18, ^uint32(0)},
		{"offset inside directory", 18, 6},
		{"empty payload", 14, 0},
		{"oversized DIB", 26, 65536},
		{"zero width", 26, 0},
	} {
		data := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(data[test.offset:], test.value)
		cases[test.name] = data
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeImage(string(data), false); err == nil {
				t.Fatal("invalid ICO was accepted")
			}
		})
	}
	f := bodyFixture(t, "image/x-icon", string(cases["invalid payload"]), true)
	f.has(locales.DetailImagePreviewFailed())
	f.click(locales.DetailHex())
	f.click(locales.DetailSaveBody())
	if len(f.saved) != 1 || f.saved[0].Body != string(cases["invalid payload"]) {
		t.Fatal("failed ICO preview lost the body")
	}
}
