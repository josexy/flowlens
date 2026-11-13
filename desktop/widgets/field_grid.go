package widgets

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go"
)

// FieldGrid measurements, from FlowLens 1.0.5's HeadersTable: h-10 rows,
// px-2 cells, a w-1.5 resize handle, and its column limits.
const (
	FieldGridRowHeight      float32 = 40
	FieldGridNameMinWidth   float32 = 140
	FieldGridValueMinWidth  float32 = 160
	fieldGridCellPaddingX   float32 = 8
	fieldGridResizeHandle   float32 = 6
	fieldGridEditorInsetX   float32 = 8
	fieldGridEditorInsetTop float32 = 4
)

// FieldGridOptions configures a FieldGrid.
type FieldGridOptions struct {
	// Label names the grid for assistive technology.
	Label string
	// ValueTrailing, when set, builds a control after row's value, such as
	// a cookie's attributes button. Clicking it does not open the editor.
	ValueTrailing func(row int)
}

type fieldGridCell struct {
	row   int
	value bool
	// key identifies the row's data, so the editor closes when the row
	// changes under it.
	key string
}

// fieldGridState is a grid's view state. Like 1.0.5's, it is not saved: the
// name column's width lasts while the grid is shown.
type fieldGridState struct {
	list ui.ListState
	// nameShare is the name column's share of a row's width, 0 for half.
	// A share rather than a width, so the columns keep their proportion as
	// the grid is resized, laid out in the same frame.
	nameShare float32
	// width is a row's width inside its borders in the last frame, which
	// turns a drag of the column's edge into a share.
	width float32

	open       *fieldGridCell
	text       string
	selectNext bool
	focused    bool
}

func fieldGridKey(row int, field KeyValue) string {
	return strconv.Itoa(row) + "\x00" + field.Key + "\x00" + field.Value
}

// FieldGrid shows read-only fields as 1.0.5 did: a bordered grid of one-line
// rows 40 px tall, with row hover and a name column the user resizes by
// dragging its edge. Clicking a cell opens a read-only editor over it that
// grows within the grid, scrolling longer text for selecting and copying,
// and closes on blur or when its row leaves view. Rows keep their order,
// duplicates, casing, and empty values; only the rows in view are built.
func FieldGrid(c *ui.Context, theme nui.Theme, rows []KeyValue, o FieldGridOptions) ui.Element {
	area := ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Clip()
	state := ui.Local(area, "flowlens-field-grid", func() fieldGridState { return fieldGridState{} })
	if state == nil {
		return area
	}
	if open := state.open; open != nil && (open.row >= len(rows) || fieldGridKey(open.row, rows[open.row]) != open.key) {
		state.open = nil
	}
	if open := state.open; open != nil {
		if first, last := state.list.Visible(); open.row < first || open.row > last {
			state.open = nil
		}
	}
	area.Children(func() {
		list := ui.List(c.Key("rows"), &state.list, len(rows), func(index int) {
			fieldGridRow(c, theme, state, rows, index, o, area)
		}).Fill().Label(o.Label)
		if bounds := list.Bounds(); bounds.W > 0 {
			state.width = bounds.W - 2
		}
	})
	return area
}

// fieldGridNameShare returns the name column's share of a row.
func fieldGridNameShare(state *fieldGridState) float32 {
	if state.nameShare <= 0 {
		return 0.5
	}
	return state.nameShare
}

// clampFieldGridName keeps a name column width within 1.0.5's limits in a row
// width wide: at least FieldGridNameMinWidth, and leaving the value at least
// FieldGridValueMinWidth where the row has room for both.
func clampFieldGridName(name, width float32) float32 {
	return min(max(name, FieldGridNameMinWidth), max(FieldGridNameMinWidth, width-FieldGridValueMinWidth))
}

func fieldGridRow(c *ui.Context, theme nui.Theme, state *fieldGridState, rows []KeyValue, index int, o FieldGridOptions, area ui.Element) {
	field := rows[index]
	top := float32(0)
	if index == 0 {
		top = 1
	}
	row := ui.Row(c.Key(index)).FillWidth().Height(max(FieldGridRowHeight, apptheme.TextSize(c, 14)*1.45+12)).AlignItems(ui.Stretch).Background(theme.Background)
	row.BorderWidth(top, 1, 1, 1).BorderColor(theme.Border)
	if row.Hovered() {
		row.Background(theme.Elevated)
	}
	// The columns are sized by the layout of this frame: the name takes its
	// share of the row, giving way down to its minimum where the value would
	// be narrower than its own, and the value takes the rest. Only a row
	// narrower than both minimums clips the value.
	row.Clip()
	row.Children(func() {
		name := ui.Row(c.Key("name")).BasisPercent(fieldGridNameShare(state) * 100).Grow(0).Shrink(1).MinWidth(FieldGridNameMinWidth).AlignItems(ui.Center).PaddingX(fieldGridCellPaddingX).Cursor(ui.CursorText)
		name.BorderWidth(0, 1, 0, 0).BorderColor(theme.Border).Label(field.Key)
		fieldGridCellClick(name, state, index, false, field)
		if state.editing(index, false) {
			area.Children(func() { fieldGridEditor(c, theme, state, name, area) })
		}
		name.Children(func() {
			ui.Text(c, field.Key).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.45).FontWeight(500).TextColor(theme.Text).Hidden(state.editing(index, false))
			fieldGridResizer(c, theme, state, name)
		})
		value := ui.Row(c.Key("value")).Grow(1).Basis(0).Shrink(0).MinWidth(FieldGridValueMinWidth).AlignItems(ui.Center).PaddingX(fieldGridCellPaddingX).Cursor(ui.CursorText)
		fieldGridCellClick(value, state, index, true, field)
		if state.editing(index, true) {
			area.Children(func() { fieldGridEditor(c, theme, state, value, area) })
		}
		value.Children(func() {
			ui.Text(c, field.Value).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.45).TextColor(theme.Text).Hidden(state.editing(index, true))
			if o.ValueTrailing != nil {
				ui.Row(c.Key("trailing")).Shrink(0).MarginStart(4).AlignItems(ui.Center).Children(func() { o.ValueTrailing(index) })
			}
		})
	})
}

func fieldGridCellClick(cell ui.Element, state *fieldGridState, index int, value bool, field KeyValue) {
	cell.OnClick(func() {
		text := field.Key
		if value {
			text = field.Value
		}
		state.open = &fieldGridCell{row: index, value: value, key: fieldGridKey(index, field)}
		state.text, state.selectNext, state.focused = text, true, false
	})
}

func (s *fieldGridState) editing(index int, value bool) bool {
	return s.open != nil && s.open.row == index && s.open.value == value
}

// fieldGridResizer is the name column's edge: dragging it resizes the
// column within its limits, as a share of the row.
func fieldGridResizer(c *ui.Context, theme nui.Theme, state *fieldGridState, name ui.Element) {
	handle := ui.Box(c.Key("resize")).Absolute().Top(0).Bottom(0).Right(-fieldGridResizeHandle / 2).Width(fieldGridResizeHandle).ZIndex(2).Cursor(ui.CursorResizeEW).Label("resize-name-column")
	if dx, _, held := handle.Dragged(); held && dx != 0 {
		if width, current := state.width, name.Bounds().W; width > 0 && current > 0 {
			state.nameShare = clampFieldGridName(current+dx, width) / width
		}
	}
	if handle.Hovered() || handle.Pressed() {
		handle.Background(theme.Primary.Alpha(0.35))
	}
}

// fieldGridEditor shows the open cell's text in a read-only editor over the
// cell. Its grid parent clips it while AttachTo follows the current layout.
func fieldGridEditor(c *ui.Context, theme nui.Theme, state *fieldGridState, cell, area ui.Element) {
	if state.open == nil {
		return
	}
	// Keyed by the cell, so a newly opened cell gets a new editor, which
	// takes the focus.
	box := ui.Box(c.Key("flowlens-field-grid-editor:"+state.open.key+":"+strconv.FormatBool(state.open.value))).AttachTo(cell, ui.AnchorTopStart, ui.AnchorTopStart).Left(fieldGridEditorInsetX).Top(fieldGridEditorInsetTop).Width(max(cell.Bounds().W-2*fieldGridEditorInsetX, 80)).ZIndex(1)
	box.Shadow(0, 8, 20, 0, ui.RGBA(15, 23, 42, 0.12)).Children(func() {
		nui.Textarea(c, theme, &state.text, nui.TextareaOptions{
			InputOptions: nui.InputOptions{ReadOnly: true, Size: nui.MD, Variant: nui.Outline, Label: "cell-editor", Style: func(_, editor, _, _ ui.Element) {
				editor.AutoFocus()
				focused := editor.Focused()
				if focused && state.selectNext {
					editor.SetTextSelection(0, len([]rune(state.text)))
					state.selectNext = false
				}
				if state.focused && !focused {
					state.open = nil
				}
				state.focused = focused
			}},
			Rows: 1, AutoResize: true, MaxRows: gridEditorMaxRows(theme, area.Bounds(), cell.Bounds()),
		}).FillWidth()
	})
	box.Hidden(state.open == nil)
}

// Bound the MD textarea to the room below its cell, leaving space for its
// focus ring. Long values then scroll inside the input instead of growing
// an inaccessible text area past the grid. MD uses a 20px line and 6px
// vertical padding, scaled by the theme, plus a 1px outline on both sides.
func gridEditorMaxRows(theme nui.Theme, viewport, cell ui.Rect) int {
	line := theme.FontSize / 14 * theme.TextScale * 20
	padding := 12*theme.Density + 2
	room := viewport.Y + viewport.H - cell.Y - 2*fieldGridEditorInsetTop
	return max(1, int((room-padding)/max(line, 1)))
}
