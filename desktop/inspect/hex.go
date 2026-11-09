package inspect

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	hexRowHeight = 22
	hexFontSize  = 13
	hexGutter    = 14

	defaultHexWidth = 720
)

// HexView shows bytes in offset, hexadecimal, and text columns.
type HexView struct {
	deps Dependencies

	identity uint64
	body     string
	encoding string
	source   logic.HexSource
	loading  bool
	failed   bool

	list      ui.ListState
	selecting bool
	column    logic.HexColumn
	anchor    int64
	focus     int64
	hasChoice bool
	press     hexPress
	dragX     float32
	dragY     float32
	exporting bool
	clipboard *string
	sequence  uint64
	width     float32
	// RowBuilds counts built rows, to verify that a frame's work follows the
	// rows in view.
	RowBuilds int
}

type hexPress struct {
	active bool
	row    int64
	x, y   float32
}

// NewHexView returns an empty view.
func NewHexView(deps Dependencies) *HexView { return &HexView{deps: deps, anchor: -1, focus: -1} }

func (h *HexView) clearChoice() { h.hasChoice, h.anchor, h.focus, h.selecting = false, -1, -1, false }

// SelectionRange returns the chosen bytes, end exclusive.
func (h *HexView) SelectionRange() (int64, int64, bool) {
	if !h.hasChoice {
		return 0, 0, false
	}
	return min(h.anchor, h.focus), max(h.anchor, h.focus) + 1, true
}

// load prepares the bytes of body. Text is read in place and Base64 is decoded
// off the main thread. A body that only grew keeps the reading position and
// the selection.
func (h *HexView) load(identity uint64, body, encoding string) {
	if h.source != nil || h.loading {
		if identity == h.identity && body == h.body && encoding == h.encoding {
			return
		}
	}
	grew := identity == h.identity && encoding == h.encoding && len(body) >= len(h.body) && strings.HasPrefix(body, h.body)
	if !grew {
		h.clearChoice()
		h.list.ScrollTo(0, ui.Start)
	}
	h.identity, h.body, h.encoding, h.failed = identity, body, encoding, false
	if encoding != "base64" {
		h.source, h.loading = logic.HexString(body), false
		return
	}
	h.source, h.loading = nil, true
	h.sequence++
	sequence := h.sequence
	runtime.Run(h.deps.Runtime.Jobs, fmt.Sprintf("inspect:hex:%p", h), func(context.Context) (*logic.HexBytes, error) {
		data, err := logic.DecodeHexInput(body, encoding)
		if err != nil {
			return nil, err
		}
		store := &logic.HexBytes{}
		store.Append(data)
		return store, nil
	}, func(store *logic.HexBytes, err error) {
		if sequence != h.sequence {
			return
		}
		h.loading = false
		if err != nil {
			h.failed = true
			return
		}
		h.source = store
	})
}

func charWidth(c *ui.Context, theme nui.Theme) float32 {
	width, _ := c.MeasureText(0, ui.Span{Text: "0", Font: theme.MonoFont, Size: hexFontSize})
	if width <= 0 {
		return hexFontSize * 0.6
	}
	return width
}

// Build shows body.
func (h *HexView) Build(c *ui.Context, theme nui.Theme, identity uint64, body, encoding string) {
	if h.clipboard != nil {
		c.WriteClipboard(*h.clipboard)
		h.clipboard = nil
	}
	h.load(identity, body, encoding)
	switch {
	case h.failed:
		emptyState(c, theme, lucide.TriangleAlert(), locales.DetailHexDecodeFailed())
		return
	case h.loading || h.source == nil:
		ui.Column(c).Fill().Center().Gap(8).Children(func() {
			ui.Spinner(c).Size(24, 24).TextColor(theme.Muted)
			ui.Text(c, locales.DetailHexLoading()).FontSize(13).TextColor(theme.Muted)
		})
		return
	}
	width := h.width
	if width == 0 {
		width = defaultHexWidth
	}
	char := charWidth(c, theme)
	length := h.source.Len()
	layout := logic.NewHexLayout(width, char, length, hexGutter)
	rows := logic.HexRowCount(length, layout.BytesPerRow)
	element := ui.List(c.Key("hex-list"), &h.list, int(rows), func(row int) {
		h.buildRow(c, theme, layout, int64(row), char, length)
	}).Fill().MinHeight(0)
	element.ContextMenu(func(menu *ui.Menu) { h.menu(menu, length) })
	if bounds := element.Bounds(); bounds.W > 0 {
		h.width = bounds.W
	}
}

func (h *HexView) inChoice(index int64) bool {
	start, end, ok := h.SelectionRange()
	return ok && index >= start && index < end
}

func rowText(source logic.HexSource, row int64, perRow int) (hexText, text string, first int64, count int) {
	first = row * int64(perRow)
	var hexOut, textOut strings.Builder
	for offset := range perRow {
		value, ok := source.Get(first + int64(offset))
		if !ok {
			break
		}
		if offset > 0 {
			hexOut.WriteByte(' ')
		}
		fmt.Fprintf(&hexOut, "%02X", value)
		if value >= 0x20 && value < 0x7f {
			textOut.WriteByte(value)
		} else {
			textOut.WriteByte('.')
		}
		count++
	}
	return hexOut.String(), textOut.String(), first, count
}

func (h *HexView) buildRow(c *ui.Context, theme nui.Theme, layout logic.HexLayout, row int64, char float32, length int64) {
	h.RowBuilds++
	hexText, text, first, count := rowText(h.source, row, layout.BytesPerRow)
	offset := fmt.Sprintf("%0*X", layout.OffsetDigits, first)
	box := ui.Box(c.Key(row)).FillWidth().Height(hexRowHeight).PaddingX(8)
	box.Draw(func(painter *ui.Painter, rect ui.Rect) {
		for cell := range count {
			if !h.inChoice(first + int64(cell)) {
				continue
			}
			left := rect.X + 8 + layout.HexStart + float32(cell)*3*char
			painter.Fill(ui.Rect{X: left, Y: rect.Y + 2, W: char*2 + 2, H: hexRowHeight - 4}, theme.Primary.Alpha(0.3), 2)
			textLeft := rect.X + 8 + layout.TextStart + float32(cell)*char
			painter.Fill(ui.Rect{X: textLeft, Y: rect.Y + 2, W: char, H: hexRowHeight - 4}, theme.Primary.Alpha(0.3), 2)
		}
	})
	box.Children(func() {
		mono := func(left float32, value string, color ui.Color) {
			ui.Text(c, value).Font(theme.MonoFont).FontSize(hexFontSize).SingleLine().TextColor(color).Absolute().Left(left + 8).Top(2)
		}
		mono(0, offset, theme.Muted)
		mono(layout.HexStart, hexText, theme.Text)
		mono(layout.TextStart, text, theme.Toned)
	})
	h.pointer(box, layout, row, char, length)
}

func (h *HexView) columnAt(x float32, layout logic.HexLayout) logic.HexColumn {
	if x >= layout.TextStart {
		return logic.HexColumnText
	}
	return logic.HexColumnHex
}

func (h *HexView) pointer(box ui.Element, layout logic.HexLayout, row int64, char float32, length int64) {
	if h.press.active && h.press.row == row {
		if dx, dy, moved := box.Dragged(); moved {
			h.dragX += dx
			h.dragY += dy
			y := h.press.y + h.dragY
			targetRow := h.press.row + int64(y/hexRowHeight)
			if y < 0 {
				targetRow--
			}
			h.focus = logic.HexByteAt(h.press.x+h.dragX, targetRow, h.column, layout, char, length)
		}
		if !box.Pressed() {
			h.press.active = false
		}
		return
	}
	if !box.Pressed() || h.press.active {
		return
	}
	x, y, ok := box.PointerPosition()
	if !ok {
		return
	}
	x -= 8
	h.press = hexPress{active: true, row: row, x: x, y: y}
	h.dragX, h.dragY = 0, 0
	h.column = h.columnAt(x, layout)
	byteIndex := logic.HexByteAt(x, row, h.column, layout, char, length)
	if box.ClickModifiers()&ui.Shift != 0 && h.hasChoice {
		h.focus = byteIndex
		return
	}
	h.anchor, h.focus, h.hasChoice = byteIndex, byteIndex, true
}

func (h *HexView) menu(menu *ui.Menu, length int64) {
	_, _, selected := h.SelectionRange()
	if menu.Item(locales.DetailHexCopyText()).Disabled(!selected).Chosen() {
		h.copySelection(logic.HexCopyText)
	}
	if menu.Item(locales.DetailHexCopyHex()).Disabled(!selected).Chosen() {
		h.copySelection(logic.HexCopyHex)
	}
	if menu.Item(locales.DetailHexCopyBase64()).Disabled(!selected).Chosen() {
		h.copySelection(logic.HexCopyBase64)
	}
	menu.Separator()
	if menu.Item(locales.DetailHexExport()).Disabled(!selected || h.exporting).Chosen() {
		h.exportSelection()
	}
	_ = length
}

func (h *HexView) copySelection(format logic.HexCopyFormat) {
	start, end, ok := h.SelectionRange()
	if !ok {
		return
	}
	source, utf8Text := h.source, h.encoding != "base64"
	runtime.Run(h.deps.Runtime.Jobs, fmt.Sprintf("inspect:hex-copy:%p", h), func(ctx context.Context) (string, error) {
		text, err := logic.SerializeHexSelection(ctx, source, start, end, format, utf8Text)
		if err != nil || format != logic.HexCopyText {
			return text, err
		}
		return logic.EscapeHexClipboardNull(ctx, text)
	}, func(text string, err error) {
		if err != nil {
			h.deps.Notifier.Error(locales.DetailBodyCopyFailed(err.Error()))
			return
		}
		h.clipboard = &text
		h.deps.Notifier.Success(locales.DetailHexSelectionCopied())
	})
}

func (h *HexView) exportSelection() {
	start, end, ok := h.SelectionRange()
	if !ok || h.exporting {
		return
	}
	var data strings.Builder
	for piece := range h.source.Range(start, end) {
		data.Write(piece)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(data.String()))
	saveToFile(h.deps, &h.exporting, "hex-selection.bin", proxyservice.SaveBodyToFileRequest{Body: encoded, BodyEncoding: "base64", ContentType: "application/octet-stream"})
}
