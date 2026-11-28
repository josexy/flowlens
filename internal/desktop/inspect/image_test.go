package inspect

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func avifFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".avif")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAVIFPreview(t *testing.T) {
	for _, name := range []string{"opaque", "alpha"} {
		for _, binary := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/binary=%t", name, binary), func(t *testing.T) {
				data := avifFixture(t, name)
				body := string(data)
				f := bodyFixture(t, "image/avif", body, binary)
				preview := &f.panel.responseBody.image
				if preview.failed || preview.image.bitmap == nil {
					t.Fatal("AVIF preview failed")
				}
				if preview.width != 64 || preview.height != 48 {
					t.Fatalf("dimensions %d x %d, want 64 x 48", preview.width, preview.height)
				}
				for _, dark := range []bool{false, true} {
					f.setDark(dark)
					f.imageRects()
					f.has("AVIF | 64 x 48 | " + logic.FormatFileSize(int64(len(data))))
					f.lacks(locales.DetailImagePreviewFailed())
				}
				if binary {
					rendertest.ShootThemes(t, f.tester, "image-avif-"+name, f.setDark)
				}
				f.click(locales.DetailSaveBody())
				if len(f.saved) != 1 || f.saved[0].Body != body {
					t.Fatalf("saving changed the AVIF body: %+v", f.saved)
				}
				f.click(locales.DetailHex())
				var hexBody bytes.Buffer
				for chunk := range f.panel.responseBody.hex.source.Range(0, int64(len(data))) {
					hexBody.Write(chunk)
				}
				if !bytes.Equal(hexBody.Bytes(), data) {
					t.Fatal("hex does not contain the original AVIF bytes")
				}
				f.click(locales.DetailPreview())
				f.imageRects()
				// Rendering over black checks both color and alpha after MyGo's
				// conversion to premultiplied bitmap pixels.
				tester := ui.NewTester(func(c *ui.Context) {
					ui.Box(c).Size(64, 48).Background(ui.RGB(0, 0, 0)).Children(func() {
						ui.Image(c, preview.image.bitmap).Size(64, 48)
					})
				}, 64, 48)
				pixels := tester.Image()
				red := pixels.RGBAAt(16, 24)
				if red.R < 250 || red.G > 5 || red.B > 5 {
					t.Fatalf("opaque red pixel = %v", red)
				}
				blue := pixels.RGBAAt(48, 24)
				wantBlue := 255
				if name == "alpha" {
					wantBlue = 128
				}
				if blue.R > 5 || blue.G > 5 || math.Abs(float64(blue.B)-float64(wantBlue)) > 5 {
					t.Fatalf("blue pixel = %v, want blue %d over black", blue, wantBlue)
				}
			})
		}
	}
}

func TestImageCentersAsSoonAsLoadingFinishes(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewGray(image.Rect(0, 0, 88, 88))); err != nil {
		t.Fatal(err)
	}
	for _, format := range []struct {
		name, body string
		svg        bool
	}{
		{name: "png", body: data.String()},
		{name: "svg", body: `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="320"><rect width="320" height="320" fill="#ff6600"/></svg>`, svg: true},
	} {
		t.Run(format.name, func(t *testing.T) {
			decoded, err := decodeImage(format.body, format.svg)
			if err != nil {
				t.Fatal(err)
			}
			viewer := NewBodyViewer(Dependencies{}, false)
			viewer.content = BodyContent{Identity: 1, Body: format.body}
			preview := &viewer.image
			*preview = imagePreview{identity: 1, body: format.body, started: true, loading: true, scale: 1}
			shown, mode := true, apptheme.ModeLight
			tester := ui.NewTester(func(c *ui.Context) {
				palette := nui.App(c, apptheme.Resolve(apptheme.Appearance{Mode: mode}).Config)
				if shown {
					preview.build(c, palette, viewer)
				}
			}, 525, 300)
			if _, ok := tester.Find("image-picture"); ok {
				t.Fatal("picture shown before decoding finishes")
			}
			// Publish the decoded result, as the background job does. Allow
			// only the frames requested by the view, with no settle loop or input.
			preview.image, preview.loading = decoded, false
			if decoded.bitmap != nil {
				preview.width, preview.height = decoded.bitmap.Size()
			}
			tester.Frame()
			check := func() {
				t.Helper()
				area, ok := tester.Find("image-area")
				if !ok {
					t.Fatal("no image area")
				}
				picture, ok := tester.Find("image-picture")
				if !ok {
					t.Fatal("image did not appear without extra input")
				}
				if math.Abs(float64(picture.X+picture.W/2-area.X-area.W/2)) > 0.5 ||
					math.Abs(float64(picture.Y+picture.H/2-area.Y-area.H/2)) > 0.5 {
					t.Fatalf("picture %+v is not centered in %+v", picture, area)
				}
				if picture.W > area.W-2*imagePanePadding+0.5 || picture.H > area.H-2*imagePanePadding+0.5 {
					t.Fatalf("picture %+v does not fit %+v", picture, area)
				}
			}
			check()
			rendertest.Shoot(t, tester, "image-first-visible-"+format.name+"-light")
			// Reopening the tab after its container changes must measure again.
			shown = false
			tester.Frame()
			tester.SetSize(400, 220)
			shown, mode = true, apptheme.ModeDark
			tester.Frame()
			check()
			rendertest.Shoot(t, tester, "image-reopened-"+format.name+"-dark")
			// A collapsed pane must not keep requesting frames forever.
			tester.SetSize(400, 1)
			tester.Frame()
			if tester.FramePending() {
				t.Fatal("collapsed image pane keeps redrawing")
			}
		})
	}
}

func largeImageFixture(t *testing.T) *fixture {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewGray(image.Rect(0, 0, 4000, 3000))); err != nil {
		t.Fatal(err)
	}
	f := bodyFixture(t, "image/png", data.String(), true)
	f.tester.SetSize(1400, 1000)
	f.panel.responseBody.active = logic.BodyTabImage
	f.settle()
	return f
}

func (f *fixture) imageRects() (area, picture ui.Rect) {
	f.t.Helper()
	area, ok := f.tester.Find("image-area")
	if !ok {
		f.t.Fatalf("no image area in %v", f.tester.Texts())
	}
	picture, ok = f.tester.Find("image-picture")
	if !ok {
		f.t.Fatal("no picture")
	}
	return area, picture
}

func TestLargeImageFitsThePane(t *testing.T) {
	f := largeImageFixture(t)
	area, picture := f.imageRects()
	if picture.W > area.W || picture.H > area.H || picture.W < 100 {
		t.Fatalf("picture %v in area %v", picture, area)
	}
	if ratio := picture.W / picture.H; ratio < 1.32 || ratio > 1.35 {
		t.Fatalf("aspect %v", ratio)
	}
	f.has("100%")
	f.has(locales.DetailImageZoomHint())
	f.has("PNG | 4000 x 3000")
	f.lacks(locales.DetailImageZoomIn())
}

func TestSmallImageIsNotUpscaled(t *testing.T) {
	f := bodyFixture(t, "image/png", imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return png.Encode(w, i) }), true)
	f.panel.responseBody.active = logic.BodyTabImage
	f.settle()
	_, picture := f.imageRects()
	if picture.W != 4 || picture.H != 3 {
		t.Fatalf("picture %v, want its own 4 x 3", picture)
	}
}

func TestCtrlWheelZoomsOneStepAndDragPans(t *testing.T) {
	f := largeImageFixture(t)
	preview := &f.panel.responseBody.image
	area, before := f.imageRects()
	x, y := area.X+area.W/2, area.Y+area.H/2
	f.tester.Move(x, y)
	f.tester.Wheel(x, y, 0, -100)
	f.settle()
	if preview.scale != 1 {
		t.Fatalf("plain wheel zoomed to %v", preview.scale)
	}
	f.tester.HoldModifiers(ui.Ctrl)
	f.settle()
	f.tester.Wheel(x, y, 0, -100)
	f.tester.HoldModifiers(0)
	f.settle()
	if preview.scale != 1+logic.ImageScaleStep {
		t.Fatalf("scale %v after one Ctrl+wheel notch", preview.scale)
	}
	f.has("110%")
	_, zoomed := f.imageRects()
	if zoomed.W <= before.W {
		t.Fatalf("picture %v not larger than %v", zoomed, before)
	}
	f.tester.Move(x, y)
	f.settle()
	if f.tester.Cursor() != ui.CursorGrab {
		t.Fatalf("cursor %v while zoomed", f.tester.Cursor())
	}
	f.tester.Press(x, y)
	f.tester.Frame()
	for step := float32(1); step <= 5; step++ {
		f.tester.Move(x+8*step, y+4*step)
		f.tester.Frame()
	}
	if f.tester.Cursor() != ui.CursorGrabbing {
		t.Fatalf("cursor %v while dragging", f.tester.Cursor())
	}
	f.tester.Release(x+40, y+20)
	f.settle()
	if preview.offsetX < 30 || preview.offsetY < 15 {
		t.Fatalf("offset %v,%v after dragging 40,20", preview.offsetX, preview.offsetY)
	}
	f.tester.Pinch(x, y, 0.5)
	f.settle()
	if preview.scale != 1 || preview.offsetX != 0 {
		t.Fatalf("pinch out: scale %v offset %v", preview.scale, preview.offsetX)
	}
	f.tester.Move(x, y)
	f.settle()
	if f.tester.Cursor() == ui.CursorGrab {
		t.Fatal("grab cursor at 100%")
	}
}

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
