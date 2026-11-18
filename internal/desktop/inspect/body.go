package inspect

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// BodyContent is the body a viewer shows.
type BodyContent struct {
	Identity uint64
	// Body holds the body's raw bytes.
	Body string
	// Binary marks a body that cannot be shown as text.
	Binary      bool
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

func (b *BodyViewer) release() {
	if jobs := b.deps.Runtime.Jobs; jobs != nil {
		jobs.Cancel(fmt.Sprintf("inspect:image:%p", &b.image))
		jobs.Cancel(fmt.Sprintf("inspect:sse:%p", b.sse))
	}
}

func (b *BodyViewer) copyPreferences(previous *BodyViewer) {
	b.active, b.tabChoice, b.tabs = previous.active, previous.tabChoice, previous.tabs
	b.raw.SetWordWrap(previous.raw.WordWrap())
	b.formatted.SetWordWrap(previous.formatted.WordWrap())
	b.sse.table.List.Columns = previous.sse.table.List.Columns
}

// Active returns the viewing mode on show.
func (b *BodyViewer) Active() logic.BodyTab { return b.active }

func (b *BodyViewer) set(content BodyContent) {
	changed := !b.shown || content.Identity != b.content.Identity || content.Body != b.bodyText ||
		content.Binary != b.content.Binary || content.ContentType != b.content.ContentType
	streamingText := b.enableSSE && !content.Binary && logic.IsEventStream(content.ContentType)
	previousBody := b.bodyText
	appended := b.shown && content.Identity == b.content.Identity && content.Binary == b.content.Binary &&
		content.ContentType == b.content.ContentType && strings.HasPrefix(content.Body, previousBody)
	grew := streamingText && appended
	tabs := logic.BodyTabs(content.ContentType, content.Binary, content.Body, b.enableSSE)
	if !b.shown || !sameTabs(tabs, b.tabs) {
		b.active = logic.NextBodyTab(b.active, tabs, b.tabs)
		b.tabs = tabs
	}
	b.content, b.shown = content, true
	b.category = logic.ClassifyBody(content.ContentType, content.Binary)
	if !changed {
		return
	}
	b.bodyText = content.Body
	if !appended {
		// A hidden Hex, SSE, or image tab must not keep the previous body's
		// payload or accept its queued decode result after replacement.
		b.release()
		*b.hex = *NewHexView(b.deps)
		b.sse.reset()
		b.sse.identity, b.sse.body, b.sse.binary = 0, "", false
		b.image.reset()
	}
	b.formatted.SetLanguage(logic.EditorLanguage(b.category))
	if streamingText {
		if grew {
			chunk := content.Body[len(previousBody):]
			// The editor normalizes CR and CRLF to LF. When a chunk splits a
			// CRLF, its CR already supplied the visible newline.
			if strings.HasSuffix(previousBody, "\r") && strings.HasPrefix(chunk, "\n") {
				chunk = chunk[1:]
			}
			b.raw.AppendText(chunk)
		} else {
			b.raw.SetText(content.Body)
			// SSE has no Formatted tab. Retaining a second full copy here
			// would undo the store's incremental body updates.
			b.formatted.SetText("")
		}
	} else if !content.Binary {
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
			ui.Column(c).Fill().Center().Children(func() { widgets.Spinner(c, theme).Size(24, 24).TextColor(theme.Muted) })
			return
		}
		emptyState(c, theme, lucide.FileX(), locales.DetailBodyEmpty())
		return
	}
	content := BodyContent{Identity: entry.ID, SourcePath: entry.Path}
	message := entry.Request
	if response {
		message = entry.Response
		content.Body, content.Binary, content.Unavailable = body.ResponseBody, body.ResponseBodyBinary, body.ResponseBodyUnavailable
	} else {
		content.Body, content.Binary, content.Unavailable = body.RequestBody, body.RequestBodyBinary, body.RequestBodyUnavailable
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
		b.set(BodyContent{Identity: content.Identity, ContentType: content.ContentType})
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
		widgets.PaneContent(c, func() {
			if content.Body == "" && b.active != logic.BodyTabSSE {
				emptyState(c, theme, lucide.File(), locales.DetailBodyEmpty())
				return
			}
			switch b.active {
			case logic.BodyTabImage:
				b.image.build(c, theme, b)
			case logic.BodyTabSSE:
				b.sse.Build(c, theme, content)
			case logic.BodyTabHex:
				ui.Box(c).Fill().MinHeight(0).Border(1, theme.Border).Clip().Children(func() {
					b.hex.Build(c, theme, content.Identity, content.Body, content.Binary)
				})
			default:
				view := b.raw
				if b.active == logic.BodyTabFormatted {
					view = b.formatted
				}
				view.Build(c, theme, b.deps.Settings.CodeFont())
			}
		})
	})
}

func (b *BodyViewer) toolbar(c *ui.Context, theme nui.Theme) {
	widgets.PaneToolbar(c, func() {
		items := make([]widgets.PillTab, len(b.tabs))
		for index, tab := range b.tabs {
			items[index] = widgets.PillTab{ID: string(tab), Label: tabLabel(tab)}
		}
		strip := ui.ScrollHorizontal(c.Key("body-tabs-scroll")).Grow(1).Basis(0).MinWidth(0).Scrollbars(false)
		widgets.WheelScroll(c, strip).Children(func() {
			widgets.PillTabs(c.Key("body-tabs"), theme, &b.tabChoice, items, locales.DetailBodyRaw())
		})
		b.active = logic.BodyTab(b.tabChoice)
	}, func() { b.actions(c, theme) })
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
	case (b.active == logic.BodyTabRaw || b.active == logic.BodyTabSSE) && !b.content.Binary:
		return b.content.Body
	default:
		return ""
	}
}

// saveTarget returns the bytes Save writes: the formatted text on the
// Formatted tab, and the raw body otherwise.
func (b *BodyViewer) saveTarget() (body string, ok bool) {
	if b.content.Body == "" {
		return "", false
	}
	if b.active == logic.BodyTabFormatted {
		return b.formatted.Text(), true
	}
	return b.content.Body, true
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
	if _, ok := b.saveTarget(); ok {
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
	body, ok := b.saveTarget()
	if !ok || b.exporting {
		return
	}
	saveToFile(b.deps, &b.exporting, b.suggestedName(), proxyservice.SaveBodyToFileRequest{Body: body, ContentType: b.content.ContentType})
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
