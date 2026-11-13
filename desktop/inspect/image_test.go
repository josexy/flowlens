package inspect

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

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
			decoded, err := decodeImage(format.body, "", format.svg)
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
	f := bodyFixture(t, "image/png", base64.StdEncoding.EncodeToString(data.Bytes()), "base64")
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
	f := bodyFixture(t, "image/png", imageFixture(t, func(w *bytes.Buffer, i image.Image) error { return png.Encode(w, i) }), "base64")
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
