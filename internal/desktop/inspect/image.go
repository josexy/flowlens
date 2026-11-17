package inspect

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	_ "github.com/gen2brain/gav1d/avif" // Register AVIF for ui.DecodeBitmap without cgo or external codecs.
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type decodedImage struct {
	bitmap *ui.Bitmap
	svg    *ui.SVG
}

// imagePreview decodes a picture off the main thread and shows it zoomable.
type imagePreview struct {
	identity    uint64
	body        string
	started     bool
	loading     bool
	failed      bool
	unsupported bool
	image       decodedImage
	width       int
	height      int
	// scale is the zoom over the fitted size: 1 shows the whole image in
	// the pane without upscaling it, as 1.0.5's object-contain.
	scale float64
	// offsetX and offsetY pan the image while it is zoomed past 1.
	offsetX, offsetY float32
	// pane is the image area's last observed bounds. hasPane distinguishes
	// the first read from an area laid out with no space available.
	pane     ui.Rect
	hasPane  bool
	sequence uint64
}

func (i *imagePreview) reset() {
	*i = imagePreview{scale: 1, sequence: i.sequence}
}

// imagePanePadding is the image area's padding, 1.0.5's p-2.5.
const imagePanePadding float32 = 10

// decodeImage decodes body's raw bytes. It runs in a background job, so the
// conversion to a byte slice never copies a large body on the main thread.
func decodeImage(body string, svg bool) (decodedImage, error) {
	data := []byte(body)
	if svg {
		parsed, err := ui.ParseSVG(data)
		if err != nil {
			return decodedImage{}, err
		}
		return decodedImage{svg: parsed}, nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return decodedImage{}, err
	}
	return decodedImage{bitmap: bitmap}, nil
}

func (i *imagePreview) ensure(b *BodyViewer) {
	if i.started && i.body == b.content.Body && i.identity == b.content.Identity {
		return
	}
	body := b.content.Body
	svg := b.category == logic.BodySVG
	i.reset()
	i.identity, i.body, i.started, i.loading = b.content.Identity, body, true, true
	i.sequence++
	key := fmt.Sprintf("inspect:image:%p", i)
	runtime.Run(b.deps.Runtime.Jobs, key, func(context.Context) (decodedImage, error) {
		return decodeImage(body, svg)
	}, func(result decodedImage, err error) {
		i.loading = false
		if err != nil {
			i.failed = true
			i.unsupported = errors.Is(err, image.ErrFormat)
			return
		}
		i.image = result
		if result.bitmap != nil {
			i.width, i.height = result.bitmap.Size()
		}
	})
}

func (i *imagePreview) zoom(delta float64) { i.setScale(i.scale + delta) }

// setScale zooms to scale within the allowed range; at 1 or below the image
// is centered again.
func (i *imagePreview) setScale(scale float64) {
	i.scale = logic.ClampImageScale(scale)
	if i.scale <= 1 {
		i.offsetX, i.offsetY = 0, 0
	}
}

func (i *imagePreview) resetView() { i.scale, i.offsetX, i.offsetY = 1, 0, 0 }

func (i *imagePreview) canReset() bool { return i.scale != 1 || i.offsetX != 0 || i.offsetY != 0 }

// naturalSize is the image's own size: an SVG without one shows at 320.
func (i *imagePreview) naturalSize() (float32, float32) {
	if i.image.bitmap != nil {
		return float32(i.width), float32(i.height)
	}
	return 320, 320
}

// fittedSize is the image's size at 100%: its own size, scaled down to fit
// in a pane w by h, never up.
func fittedSize(naturalW, naturalH, w, h float32) (float32, float32) {
	if naturalW <= 0 || naturalH <= 0 {
		return 0, 0
	}
	fit := float32(1)
	if w > 0 && naturalW > w {
		fit = w / naturalW
	}
	if h > 0 && naturalH*fit > h {
		fit = h / naturalH
	}
	return naturalW * fit, naturalH * fit
}

// displaySize is the image's size at the current scale.
func (i *imagePreview) displaySize() (float32, float32) {
	naturalW, naturalH := i.naturalSize()
	w, h := fittedSize(naturalW, naturalH, i.pane.W-2*imagePanePadding, i.pane.H-2*imagePanePadding)
	return w * float32(i.scale), h * float32(i.scale)
}

func (i *imagePreview) build(c *ui.Context, theme nui.Theme, b *BodyViewer) {
	i.ensure(b)
	if i.scale == 0 {
		i.scale = 1
	}
	if i.loading {
		ui.Column(c).Fill().Center().Children(func() { widgets.Spinner(c, theme).Size(24, 24).TextColor(theme.Muted) })
		return
	}
	if i.failed {
		message := locales.DetailImagePreviewFailed()
		if i.unsupported {
			message = locales.DetailImageFormatUnsupported()
		}
		emptyState(c, theme, lucide.ImageOff(), message)
		return
	}
	ui.Column(c).Fill().MinHeight(0).Border(1, theme.Border).Background(theme.Background).Children(func() {
		i.infoBar(c, theme, b)
		area := ui.Box(c.Key("image-area")).FillWidth().Grow(1).Basis(0).MinHeight(0).Clip().Label("image-area")
		bounds := area.Bounds()
		if !i.hasPane || bounds.W != i.pane.W || bounds.H != i.pane.H {
			// Bounds describe the previous layout. After loading (or reopening
			// the pane), request the pass that can use its measured size.
			c.Invalidate()
		}
		i.pane, i.hasPane = bounds, true
		held := widgets.HeldModifiers(c)
		area.HandleInput(func(ev ui.InputEvent) bool {
			switch {
			case b.imageMenu.Take(ev):
				return true
			case ev.Kind == ui.InputScroll && (ev.Mods|held)&ui.Ctrl != 0 && ev.DY != 0:
				if ev.DY < 0 {
					i.zoom(logic.ImageScaleStep)
				} else {
					i.zoom(-logic.ImageScaleStep)
				}
				return true
			case ev.Kind == ui.InputZoom && ev.Scale != 1:
				if ev.Scale > 1 {
					i.zoom(logic.ImageScaleStep)
				} else {
					i.zoom(-logic.ImageScaleStep)
				}
				return true
			}
			return false
		})
		if dx, dy, held := area.Dragged(); held && i.scale > 1 {
			i.offsetX += dx
			i.offsetY += dy
		}
		switch {
		case i.scale > 1 && area.Pressed():
			area.Cursor(ui.CursorGrabbing)
		case i.scale > 1:
			area.Cursor(ui.CursorGrab)
		}
		area.Children(func() {
			i.picture(c, theme)
			b.imageMenu.Build(c, theme, func() []nui.MenuItem { return i.menuItems(b) }, nui.MenuOptions{})
		})
	})
}

// menuItems are the actions on the image, as 1.0.5's.
func (i *imagePreview) menuItems(b *BodyViewer) []nui.MenuItem {
	return []nui.MenuItem{
		{ID: "zoom-in", Label: locales.DetailImageZoomIn(), Disabled: i.scale >= logic.ImageScaleMax, OnSelect: func() { i.zoom(logic.ImageScaleStep) }},
		{ID: "zoom-out", Label: locales.DetailImageZoomOut(), Disabled: i.scale <= logic.ImageScaleMin, OnSelect: func() { i.zoom(-logic.ImageScaleStep) }},
		{ID: "reset", Label: locales.DetailImageReset(), Disabled: !i.canReset(), OnSelect: i.resetView},
		widgets.MenuSeparator("save-separator"),
		{ID: "save", Label: locales.DetailImageSave(), OnSelect: b.saveCurrent},
	}
}

func (i *imagePreview) picture(c *ui.Context, theme nui.Theme) {
	// Until the area has been laid out, centering against zero dimensions
	// would put half the image outside its top-left corner.
	if i.pane.W <= 0 || i.pane.H <= 0 {
		return
	}
	w, h := i.displaySize()
	if w <= 0 || h <= 0 {
		return
	}
	left := (i.pane.W-w)/2 + i.offsetX
	top := (i.pane.H-h)/2 + i.offsetY
	if i.image.svg != nil {
		ui.Icon(c.Key("picture"), i.image.svg).Absolute().Left(left).Top(top).Size(w, h).TextColor(theme.Text).IgnoreInput(true).Label("image-picture")
		return
	}
	if i.image.bitmap != nil {
		ui.Image(c.Key("picture"), i.image.bitmap).Absolute().Left(left).Top(top).Size(w, h).Fit(ui.Contain).IgnoreInput(true).Label("image-picture")
	}
}

func (i *imagePreview) info(b *BodyViewer) string {
	parts := []string{}
	if format := logic.ImageFormatLabel(b.content.ContentType); format != "" {
		parts = append(parts, format)
	}
	if i.width > 0 && i.height > 0 {
		parts = append(parts, strconv.Itoa(i.width)+" x "+strconv.Itoa(i.height))
	}
	parts = append(parts, logic.FormatFileSize(int64(len(b.content.Body))))
	return strings.Join(parts, " | ")
}

// infoBar shows the scale, the image's format, dimensions, and size, and
// how to zoom, as 1.0.5 did above the image.
func (i *imagePreview) infoBar(c *ui.Context, theme nui.Theme, b *BodyViewer) {
	bar := ui.Row(c.Key("image-info")).FillWidth().Shrink(0).MinHeight(28).AlignItems(ui.Center).Gap(10).PaddingX(10).Background(theme.Elevated)
	bar.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c, strconv.Itoa(int(i.scale*100+0.5))+"%").Shrink(0).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).FontFeatures("tnum").TextColor(theme.Toned)
		ui.Text(c, i.info(b)).Shrink(1).MinWidth(0).SingleLine().FontSize(apptheme.TextSize(c, 14)).FontFeatures("tnum").TextColor(theme.Toned)
		ui.Box(c).Grow(1)
		ui.Text(c, locales.DetailImageZoomHint()).Shrink(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
	})
}
