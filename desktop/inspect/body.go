package inspect

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// BodyContent is the body a viewer shows.
type BodyContent struct {
	Identity    uint64
	Body        string
	Encoding    string
	ContentType string
	SourcePath  string
	// Unavailable tells that the stored payload is missing.
	Unavailable bool
}

// BodyViewer shows one body in its applicable modes.
type BodyViewer struct {
	deps      Dependencies
	enableSSE bool

	content  BodyContent
	shown    bool
	tabs     []logic.BodyTab
	active   logic.BodyTab
	category logic.BodyCategory
	// tabChoice is the tab the tab bar edits.
	tabChoice string

	formatted *widgets.CodeView
	raw       *widgets.CodeView
	bodyText  string

	hex   *HexView
	sse   *SSEView
	image imagePreview
	// imageMenu is the image's context menu, kept apart from image, which
	// is reset as a whole.
	imageMenu widgets.PointMenu

	exporting bool
	clipboard *string
}

// NewBodyViewer returns an empty viewer.
func NewBodyViewer(deps Dependencies, enableSSE bool) *BodyViewer {
	return &BodyViewer{
		deps: deps, enableSSE: enableSSE, active: logic.BodyTabRaw,
		formatted: widgets.NewCodeView("plaintext", true), raw: widgets.NewCodeView("plaintext", true),
		hex: NewHexView(deps), sse: NewSSEView(deps),
	}
}

// Active returns the viewing mode on show.
func (b *BodyViewer) Active() logic.BodyTab { return b.active }

func (b *BodyViewer) set(content BodyContent) {
	changed := !b.shown || content.Identity != b.content.Identity || content.Body != b.bodyText ||
		content.Encoding != b.content.Encoding || content.ContentType != b.content.ContentType
	tabs := logic.BodyTabs(content.ContentType, content.Encoding, content.Body, b.enableSSE)
	if !b.shown || !sameTabs(tabs, b.tabs) {
		b.active = logic.NextBodyTab(b.active, tabs, b.tabs)
		b.tabs = tabs
	}
	identityChanged := !b.shown || content.Identity != b.content.Identity
	b.content, b.shown = content, true
	b.category = logic.ClassifyBody(content.ContentType, content.Encoding)
	if !changed {
		return
	}
	b.bodyText = content.Body
	if identityChanged {
		b.image.reset()
	}
	b.formatted.SetLanguage(logic.EditorLanguage(b.category))
	if content.Encoding != "base64" {
		b.formatted.SetText(logic.FormatBody(content.Body, b.category))
		b.raw.SetText(content.Body)
	} else {
		b.formatted.SetText("")
		b.raw.SetText(locales.DetailBinaryDataHint())
	}
}

func sameTabs(left, right []logic.BodyTab) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func tabLabel(tab logic.BodyTab) string {
	switch tab {
	case logic.BodyTabFormatted:
		return locales.DetailFormatted()
	case logic.BodyTabImage:
		return locales.DetailPreview()
	case logic.BodyTabSSE:
		return locales.DetailSseTab()
	case logic.BodyTabHex:
		return locales.DetailHex()
	default:
		return locales.DetailBodyRaw()
	}
}

func (p *Panel) bodyPane(c *ui.Context, theme nui.Theme, viewer *BodyViewer, entry *logic.TrafficEntry, response bool) {
	body := p.loadedBody(entry)
	if body == nil {
		if p.data.BodyLoading() {
			ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(24, 24).TextColor(theme.Muted) })
			return
		}
		emptyState(c, theme, lucide.FileX(), locales.DetailBodyEmpty())
		return
	}
	content := BodyContent{Identity: entry.ID, SourcePath: entry.Path}
	message := entry.Request
	if response {
		message = entry.Response
		content.Body, content.Encoding, content.Unavailable = body.ResponseBody, body.ResponseBodyEncoding, body.ResponseBodyUnavailable
	} else {
		content.Body, content.Encoding, content.Unavailable = body.RequestBody, body.RequestBodyEncoding, body.RequestBodyUnavailable
	}
	if message != nil {
		content.ContentType, _ = logic.FirstHeaderValue(message.HeaderFields, "Content-Type")
	}
	viewer.Build(c, theme, content)
}

// Build shows content.
func (b *BodyViewer) Build(c *ui.Context, theme nui.Theme, content BodyContent) {
	if b.clipboard != nil {
		c.WriteClipboard(*b.clipboard)
		b.clipboard = nil
	}
	if content.Unavailable {
		emptyState(c, theme, lucide.TriangleAlert(), locales.DetailBodyUnavailable())
		return
	}
	if b.tabChoice != "" {
		b.active = logic.BodyTab(b.tabChoice)
	}
	b.set(content)
	b.tabChoice = string(b.active)
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		b.toolbar(c, theme)
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			if content.Body == "" && b.active != logic.BodyTabSSE {
				emptyState(c, theme, lucide.File(), locales.DetailBodyEmpty())
				return
			}
			font := b.deps.Settings.CodeFont()
			switch b.active {
			case logic.BodyTabFormatted:
				b.formatted.Build(c, font).Fill()
			case logic.BodyTabImage:
				b.image.build(c, theme, b)
			case logic.BodyTabSSE:
				b.sse.Build(c, theme, content)
			case logic.BodyTabHex:
				b.hex.Build(c, theme, content.Identity, content.Body, content.Encoding)
			default:
				b.raw.Build(c, font).Fill()
			}
		})
	})
}

func (b *BodyViewer) toolbar(c *ui.Context, theme nui.Theme) {
	ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).PaddingX(10).PaddingY(4).Children(func() {
		items := make([]widgets.PillTab, len(b.tabs))
		for index, tab := range b.tabs {
			items[index] = widgets.PillTab{ID: string(tab), Label: tabLabel(tab)}
		}
		strip := ui.ScrollHorizontal(c.Key("body-tabs-scroll")).Grow(1).Basis(0).MinWidth(0).Scrollbars(false)
		widgets.WheelScroll(c, strip).Children(func() {
			widgets.PillTabs(c.Key("body-tabs"), theme, &b.tabChoice, items, locales.DetailBodyRaw())
		})
		b.active = logic.BodyTab(b.tabChoice)
		ui.Row(c).Shrink(0).Gap(4).Children(func() { b.actions(c, theme) })
	})
}

func (b *BodyViewer) wrapTarget() *widgets.CodeView {
	switch b.active {
	case logic.BodyTabRaw:
		return b.raw
	case logic.BodyTabFormatted:
		return b.formatted
	default:
		return nil
	}
}

func (b *BodyViewer) copyable() string {
	if b.content.Body == "" {
		return ""
	}
	switch {
	case b.active == logic.BodyTabFormatted:
		return b.formatted.Text()
	case (b.active == logic.BodyTabRaw || b.active == logic.BodyTabSSE) && b.content.Encoding != "base64":
		return b.content.Body
	default:
		return ""
	}
}

func (b *BodyViewer) saveTarget() (body, encoding string, ok bool) {
	if b.content.Body == "" {
		return "", "", false
	}
	if b.active == logic.BodyTabFormatted {
		return b.formatted.Text(), "", true
	}
	return b.content.Body, b.content.Encoding, true
}

func (b *BodyViewer) actions(c *ui.Context, theme nui.Theme) {
	if b.content.Body == "" {
		return
	}
	if target := b.wrapTarget(); target != nil {
		color := nui.Neutral
		if target.WordWrap() {
			color = nui.Primary
		}
		button := nui.Button(c.Key("wrap"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: color, Size: nui.SM, Square: true, Leading: lucide.TextWrap(), AccessibleLabel: locales.DetailWrapBody()})
		button.OnClick(func() { target.SetWordWrap(!target.WordWrap()) })
		nui.Tooltip(c, theme, button, locales.DetailWrapBody(), nui.TooltipOptions{})
	}
	if text := b.copyable(); text != "" {
		iconButton(c, theme, "copy", lucide.Copy(), locales.DetailCopyBody(), false, func() {
			b.clipboard = &text
			b.deps.Notifier.Success(locales.DetailBodyCopied())
		})
	}
	if _, _, ok := b.saveTarget(); ok {
		iconButton(c, theme, "save", lucide.Download(), locales.DetailSaveBody(), b.exporting, b.saveCurrent)
	}
}

func (b *BodyViewer) suggestedName() string {
	fallback := "response-body"
	if b.active == logic.BodyTabImage {
		fallback = "response-image"
	}
	return logic.InferBodyFilename(b.content.SourcePath, b.content.ContentType, b.category, fallback)
}

func (b *BodyViewer) saveCurrent() {
	body, encoding, ok := b.saveTarget()
	if !ok || b.exporting {
		return
	}
	saveToFile(b.deps, &b.exporting, b.suggestedName(), proxyservice.SaveBodyToFileRequest{Body: body, BodyEncoding: encoding, ContentType: b.content.ContentType})
}

// saveToFile asks for a path and writes request there, off the main thread. A
// canceled dialog is not an error.
func saveToFile(deps Dependencies, busy *bool, filename string, request proxyservice.SaveBodyToFileRequest) {
	if deps.PickSavePath == nil || deps.SaveBody == nil {
		return
	}
	*busy = true
	runtime.Run(deps.Runtime.Jobs, fmt.Sprintf("inspect:save:%p", busy), func(context.Context) (struct{}, error) {
		path, err := deps.PickSavePath(filename)
		if err != nil || strings.TrimSpace(path) == "" {
			return struct{}{}, err
		}
		request.Path = path
		return struct{}{}, deps.SaveBody(request)
	}, func(_ struct{}, err error) {
		*busy = false
		if err != nil {
			deps.Notifier.Error(locales.DetailBodySaveFailed(err.Error()))
		}
	})
}
