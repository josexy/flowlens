package inspect

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

type decodedImage struct {
	bitmap *ui.Bitmap
	svg    *ui.SVG
}

// imagePreview decodes a picture off the main thread and shows it zoomable.
type imagePreview struct {
	identity uint64
	body     string
	started  bool
	loading  bool
	failed   bool
	image    decodedImage
	width    int
	height   int
	scale    float64
	sequence uint64
}

func (i *imagePreview) reset() {
	*i = imagePreview{scale: 1, sequence: i.sequence}
}

func decodeImage(body, encoding string, svg bool) (decodedImage, error) {
	data := []byte(body)
	if encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return decodedImage{}, err
		}
		data = decoded
	}
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
	body, encoding := b.content.Body, b.content.Encoding
	svg := b.category == logic.BodySVG
	i.reset()
	i.identity, i.body, i.started, i.loading = b.content.Identity, body, true, true
	i.sequence++
	key := fmt.Sprintf("inspect:image:%p", i)
	runtime.Run(b.deps.Runtime.Jobs, key, func(context.Context) (decodedImage, error) {
		return decodeImage(body, encoding, svg)
	}, func(result decodedImage, err error) {
		i.loading = false
		if err != nil {
			i.failed = true
			return
		}
		i.image = result
		if result.bitmap != nil {
			i.width, i.height = result.bitmap.Size()
		}
	})
}

func (i *imagePreview) zoom(delta float64) { i.scale = logic.ClampImageScale(i.scale + delta) }

func (i *imagePreview) build(c *ui.Context, theme nui.Theme, b *BodyViewer) {
	i.ensure(b)
	if i.scale == 0 {
		i.scale = 1
	}
	if i.loading {
		ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(24, 24).TextColor(theme.Muted) })
		return
	}
	if i.failed {
		emptyState(c, theme, lucide.ImageOff(), locales.DetailImagePreviewFailed())
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		i.controls(c, theme, b)
		area := ui.Scroll(c).FillWidth().Grow(1).Basis(0).MinHeight(0)
		area.ContextMenu(func(menu *ui.Menu) {
			if menu.Item(locales.DetailImageZoomIn()).Disabled(i.scale >= logic.ImageScaleMax).Chosen() {
				i.zoom(logic.ImageScaleStep)
			}
			if menu.Item(locales.DetailImageZoomOut()).Disabled(i.scale <= logic.ImageScaleMin).Chosen() {
				i.zoom(-logic.ImageScaleStep)
			}
			if menu.Item(locales.DetailImageReset()).Disabled(i.scale == 1).Chosen() {
				i.scale = 1
			}
			menu.Separator()
			if menu.Item(locales.DetailImageSave()).Chosen() {
				b.saveCurrent()
			}
		})
		area.Children(func() {
			ui.Box(c).FillWidth().Center().Padding(8).Children(func() { i.picture(c, theme) })
		})
	})
}

func (i *imagePreview) picture(c *ui.Context, theme nui.Theme) {
	scale := float32(i.scale)
	if i.image.svg != nil {
		ui.Icon(c, i.image.svg).Size(320*scale, 320*scale).TextColor(theme.Text)
		return
	}
	if i.image.bitmap != nil {
		ui.Image(c, i.image.bitmap).Size(float32(i.width)*scale, float32(i.height)*scale).Fit(ui.Contain)
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
	parts = append(parts, logic.FormatFileSize(logic.EstimateDecodedByteLength(b.content.Body, b.content.Encoding)))
	return strings.Join(parts, " | ")
}

func (i *imagePreview) controls(c *ui.Context, theme nui.Theme, b *BodyViewer) {
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(4).PaddingX(10).PaddingY(4).Children(func() {
		ui.Text(c, i.info(b)).Grow(1).Basis(0).MinWidth(0).FontSize(12).SingleLine().TextColor(theme.Muted)
		iconButton(c, theme, "zoom-out", lucide.ZoomOut(), locales.DetailImageZoomOut(), i.scale <= logic.ImageScaleMin, func() { i.zoom(-logic.ImageScaleStep) })
		ui.Text(c, strconv.Itoa(int(i.scale*100+0.5))+"%").FontSize(12).TextColor(theme.Toned)
		iconButton(c, theme, "zoom-in", lucide.ZoomIn(), locales.DetailImageZoomIn(), i.scale >= logic.ImageScaleMax, func() { i.zoom(logic.ImageScaleStep) })
		iconButton(c, theme, "reset", lucide.RotateCcw(), locales.DetailImageReset(), i.scale == 1, func() { i.scale = 1 })
	})
}
