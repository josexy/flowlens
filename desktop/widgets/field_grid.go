package widgets

import (
	"strconv"

	"github.com/egoist/mygo/ui"
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
	// nameWidth is the name column's width, 0 for half the grid.
	nameWidth float32
	width     float32

	open       *fieldGridCell
	openAt     ui.Rect
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
// grows to show the whole text, for selecting and copying, and closes when
// it loses the focus. Rows keep their order, duplicates, casing, and empty
// values, and only the rows in view are built.
func FieldGrid(c *ui.Context, theme nui.Theme, rows []KeyValue, o FieldGridOptions) ui.Element {
	area := ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0)
	state := ui.Local(area, "flowlens-field-grid", func() fieldGridState { return fieldGridState{} })
	if state == nil {
		return area
	}
	if open := state.open; open != nil && (open.row >= len(rows) || fieldGridKey(open.row, rows[open.row]) != open.key) {
		state.open = nil
	}
	area.Children(func() {
		list := ui.List(c.Key("rows"), &state.list, len(rows), func(index int) {
			fieldGridRow(c, theme, state, rows, index, o)
		}).Fill().Label(o.Label)
		if bounds := list.Bounds(); bounds.W > 0 {
			state.width = bounds.W
		}
	})
	fieldGridEditor(c, theme, state)
	return area
}

// fieldGridNameWidth returns the name column's width in a grid width wide.
func fieldGridNameWidth(state *fieldGridState, width float32) float32 {
	if state.nameWidth <= 0 {
		return max(width/2, 0)
	}
	return clampFieldGridName(state.nameWidth, width)
}

func clampFieldGridName(name, width float32) float32 {
	return min(max(name, FieldGridNameMinWidth), max(FieldGridNameMinWidth, width-FieldGridValueMinWidth))
}

func fieldGridRow(c *ui.Context, theme nui.Theme, state *fieldGridState, rows []KeyValue, index int, o FieldGridOptions) {
	field := rows[index]
	top := float32(0)
	if index == 0 {
		top = 1
	}
	row := ui.Row(c.Key(index)).FillWidth().Height(FieldGridRowHeight).AlignItems(ui.Stretch).Background(theme.Background)
	row.BorderWidth(top, 1, 1, 1).BorderColor(theme.Border)
	if row.Hovered() {
		row.Background(theme.Elevated)
	}
	nameWidth := fieldGridNameWidth(state, state.width-2)
	row.Children(func() {
		name := ui.Row(c.Key("name")).Width(nameWidth).Shrink(0).MinWidth(0).AlignItems(ui.Center).PaddingX(fieldGridCellPaddingX).Cursor(ui.CursorText)
		name.BorderWidth(0, 1, 0, 0).BorderColor(theme.Border).Label(field.Key)
		fieldGridCellClick(name, state, index, false, field)
		name.Children(func() {
			ui.Text(c, field.Key).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(14).LineHeight(1.45).FontWeight(500).TextColor(theme.Text)
			fieldGridResizer(c, theme, state)
		})
		value := ui.Row(c.Key("value")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).PaddingX(fieldGridCellPaddingX).Cursor(ui.CursorText)
		fieldGridCellClick(value, state, index, true, field)
		value.Children(func() {
			ui.Text(c, field.Value).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(14).LineHeight(1.45).TextColor(theme.Text)
			if o.ValueTrailing != nil {
				ui.Row(c.Key("trailing")).Shrink(0).MarginStart(4).AlignItems(ui.Center).Children(func() { o.ValueTrailing(index) })
			}
		})
	})
}

func fieldGridCellClick(cell ui.Element, state *fieldGridState, index int, value bool, field KeyValue) {
	if open := state.open; open != nil && open.row == index && open.value == value {
		state.openAt = cell.Bounds()
	}
	cell.OnClick(func() {
		text := field.Key
		if value {
			text = field.Value
		}
		state.open = &fieldGridCell{row: index, value: value, key: fieldGridKey(index, field)}
		state.openAt, state.text, state.selectNext, state.focused = cell.Bounds(), text, true, false
	})
}

// fieldGridResizer is the name column's edge: dragging it resizes the
// column within its limits.
func fieldGridResizer(c *ui.Context, theme nui.Theme, state *fieldGridState) {
	handle := ui.Box(c.Key("resize")).Absolute().Top(0).Bottom(0).Right(-fieldGridResizeHandle / 2).Width(fieldGridResizeHandle).ZIndex(2).Cursor(ui.CursorResizeEW).Label("resize-name-column")
	if dx, _, held := handle.Dragged(); held && dx != 0 {
		width := state.width - 2
		state.nameWidth = clampFieldGridName(fieldGridNameWidth(state, width)+dx, width)
	}
	if handle.Hovered() || handle.Pressed() {
		handle.Background(theme.Primary.Alpha(0.35))
	}
}

// fieldGridEditor shows the open cell's text in a read-only editor over the
// cell, above the grid so it can grow past the row.
func fieldGridEditor(c *ui.Context, theme nui.Theme, state *fieldGridState) {
	if state.open == nil {
		return
	}
	at := state.openAt
	ui.Overlay(c, func() {
		// Keyed by the cell, so a newly opened cell gets a new editor, which
		// takes the focus.
		box := ui.Box(c.Key("flowlens-field-grid-editor:" + state.open.key + ":" + strconv.FormatBool(state.open.value))).Absolute().Left(at.X + fieldGridEditorInsetX).Top(at.Y + fieldGridEditorInsetTop).Width(max(at.W-2*fieldGridEditorInsetX, 80))
		box.Shadow(0, 8, 20, 0, ui.RGBA(15, 23, 42, 0.12)).Children(func() {
			nui.Textarea(c, theme, &state.text, nui.TextareaOptions{
				InputOptions: nui.InputOptions{ReadOnly: true, Variant: nui.Outline, Label: "cell-editor", Style: func(_, editor, _, _ ui.Element) {
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
				Rows: 1, AutoResize: true,
			}).FillWidth()
		})
	})
}
