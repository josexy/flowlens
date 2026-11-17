package inspect

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
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
	// SelectionFilename is the name Export suggests for the bytes selected,
	// hex-selection.bin when empty.
	SelectionFilename string
	// Exporting, when set, is the busy flag of the owner's Save, which Export
	// shares, as 1.0.5's WebSocket message dialog did: neither saves while
	// the other does.
	Exporting *bool

	identity uint64
	body     string
	binary   bool
	source   logic.HexSource

	list      ui.ListState
	menu      nui.MenuState
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

func (h *HexView) clearChoice() {
	h.hasChoice, h.anchor, h.focus, h.selecting = false, -1, -1, false
	h.press = hexPress{}
}

// SelectionRange returns the chosen bytes, end exclusive.
func (h *HexView) SelectionRange() (int64, int64, bool) {
	if !h.hasChoice {
		return 0, 0, false
	}
	return min(h.anchor, h.focus), max(h.anchor, h.focus) + 1, true
}

// load reads the bytes of body in place. binary marks a body whose bytes are
// not text, which Copy as text renders byte by byte. A body that only grew
// keeps the reading position and the selection.
func (h *HexView) load(identity uint64, body string, binary bool) {
	if h.source != nil && identity == h.identity && body == h.body && binary == h.binary {
		return
	}
	grew := identity == h.identity && binary == h.binary && len(body) >= len(h.body) && strings.HasPrefix(body, h.body)
	if !grew {
		h.clearChoice()
		h.list.ScrollTo(0, ui.Start)
	}
	h.identity, h.body, h.binary = identity, body, binary
	h.source = logic.HexString(body)
}

func charWidth(c *ui.Context, theme nui.Theme) float32 {
	width, _ := c.MeasureText(0, ui.Span{Text: "0", Font: theme.MonoFont, Size: hexFontSize})
	if width <= 0 {
		return hexFontSize * 0.6
	}
	return width
}

// Build shows body, which holds raw bytes. binary marks a body that is not
// text.
func (h *HexView) Build(c *ui.Context, theme nui.Theme, identity uint64, body string, binary bool) {
	if h.clipboard != nil {
		c.WriteClipboard(*h.clipboard)
		h.clipboard = nil
	}
	h.load(identity, body, binary)
	width := h.width
	if width == 0 {
		width = defaultHexWidth
		h.width = width
		// Bounds become available after this first layout. Schedule the pass
		// that measures them even when the window has no animations or input.
		c.Invalidate()
	}
	char := charWidth(c, theme)
	length := h.source.Len()
	layout := logic.NewHexLayout(width, char, length, hexGutter)
	rows := logic.HexRowCount(length, layout.BytesPerRow)
	element := ui.List(c.Key("hex-list"), &h.list, int(rows), func(row int) {
		h.buildRow(c, theme, layout, int64(row), char, length)
	}).Fill().MinHeight(0)
	widgets.ContextMenu(c.Key("hex-menu"), theme, element, &h.menu, h.menuItems, nui.MenuOptions{Label: locales.DetailHex()})
	if bounds := element.Bounds(); bounds.W > 0 && bounds.W != h.width {
		h.width = bounds.W
		// This pass used the old width. Reflow the byte columns before going
		// idle, including after a splitter or window resize.
		c.Invalidate()
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
	h.pointer(c, box, layout, row, char, length)
}

func (h *HexView) columnAt(x float32, layout logic.HexLayout) logic.HexColumn {
	if x >= layout.TextStart {
		return logic.HexColumnText
	}
	return logic.HexColumnHex
}

func (h *HexView) pointer(c *ui.Context, box ui.Element, layout logic.HexLayout, row int64, char float32, length int64) {
	// Register dragging on every build and follow the captured press. Pressed
	// becomes false as soon as the pointer leaves this row's bounds.
	dx, dy, held := box.Dragged()
	if h.press.active && h.press.row == row {
		if held {
			h.dragX += dx
			h.dragY += dy
			y := h.press.y + h.dragY
			targetRow := h.press.row + int64(math.Floor(float64(y/hexRowHeight)))
			h.focus = max(0, logic.HexByteAt(h.press.x+h.dragX, targetRow, h.column, layout, char, length))
		}
		if !held {
			h.press.active = false
		}
		return
	}
	if !held || h.press.active {
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
	if c.Modifiers()&ui.Shift != 0 && h.hasChoice {
		h.focus = byteIndex
		return
	}
	h.anchor, h.focus, h.hasChoice = byteIndex, byteIndex, true
}

// menuItems are the actions on the bytes selected.
func (h *HexView) menuItems() []nui.MenuItem {
	_, _, selected := h.SelectionRange()
	return []nui.MenuItem{
		{ID: "copy-text", Label: locales.DetailHexCopyText(), Disabled: !selected, OnSelect: func() { h.copySelection(logic.HexCopyText) }},
		{ID: "copy-hex", Label: locales.DetailHexCopyHex(), Disabled: !selected, OnSelect: func() { h.copySelection(logic.HexCopyHex) }},
		{ID: "copy-base64", Label: locales.DetailHexCopyBase64(), Disabled: !selected, OnSelect: func() { h.copySelection(logic.HexCopyBase64) }},
		widgets.MenuSeparator("export-separator"),
		{ID: "export", Label: locales.DetailHexExport(), Disabled: !selected || *h.exportBusy(), OnSelect: h.exportSelection},
	}
}

func (h *HexView) copySelection(format logic.HexCopyFormat) {
	start, end, ok := h.SelectionRange()
	if !ok {
		return
	}
	source, utf8Text := h.source, !h.binary
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
	if !ok || *h.exportBusy() {
		return
	}
	var data strings.Builder
	for piece := range h.source.Range(start, end) {
		data.Write(piece)
	}
	name := h.SelectionFilename
	if name == "" {
		name = "hex-selection.bin"
	}
	saveToFile(h.deps, h.exportBusy(), name, proxyservice.SaveBodyToFileRequest{Body: data.String(), ContentType: "application/octet-stream"})
}

// exportBusy returns the flag set while Export saves.
func (h *HexView) exportBusy() *bool {
	if h.Exporting != nil {
		return h.Exporting
	}
	return &h.exporting
}
