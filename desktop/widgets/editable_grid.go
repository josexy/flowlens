package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// EditableGrid measurements, from FlowLens 1.0.5's EditableKeyValueTable:
// h-10 rows, 46 px enable and action columns, p-1.5 cells, and the key
// column's limits.
const (
	EditableGridRowHeight     float32 = 40
	EditableGridSideColumn    float32 = 46
	EditableGridKeyMinWidth   float32 = 140
	EditableGridValueMinWidth float32 = 160
	editableGridCellPadding   float32 = 6
)

// EditableGridField names a text cell of a row.
type EditableGridField int

// The text cells of a row.
const (
	EditableGridKey EditableGridField = iota
	EditableGridValue
)

// EditableGridOptions describes the rows of an EditableGrid. The grid reads
// and writes rows through the pointers these functions return, so it works
// for any row type.
type EditableGridOptions struct {
	// Label names the grid for assistive technology.
	Label string
	// Rows is how many rows there are.
	Rows int
	// RowKey identifies row i across frames; nil uses its key field pointer.
	// Callers replacing or removing rows must change that pointer or RowKey.
	RowKey func(i int) any
	// Enabled, Key, and Value return row i's fields. Value returning nil
	// leaves the value cell to ValueCell.
	Enabled    func(i int) *bool
	Key, Value func(i int) *string
	// KeyLabel and ValueLabel name row i's cells; EnabledLabel its box.
	KeyLabel, ValueLabel, EnabledLabel func(i int) string
	// KeyPlaceholder and ValuePlaceholder show, muted, in empty cells.
	KeyPlaceholder, ValuePlaceholder string
	// Resizable lets the user drag the key column's edge.
	Resizable bool
	// KeyTrailing builds marks after a key while it is not being edited,
	// such as an invalid-name icon.
	KeyTrailing func(i int)
	// ValueCell builds row i's value cell itself when Value returns nil,
	// such as a form-data file chip.
	ValueCell func(i int)
	// Actions builds the last column of row i, such as a remove button.
	Actions func(i int)
	// Row, when set, receives each row's element, for row-wide input such
	// as dropped files.
	Row func(i int, row ui.Element)
}

type editableGridCell struct {
	row      any
	index    int
	field    EditableGridField
	text     *string
	original string
	focus    *ui.Handle
}

type editableGridState struct {
	list        ui.ListState
	keyWidth    float32
	width       float32
	open        *editableGridCell
	select_     bool
	focused     bool
	committed   bool
	canceled    bool
	composing   bool
	returnFocus bool
}

// EditableGrid builds a compact, virtualized key/value grid. Its text cells
// use native button activation and an anchored Textarea with native Tab order.
// Enter or blur commits; Escape restores the value from when editing began.
func EditableGrid(c *ui.Context, theme nui.Theme, o EditableGridOptions) ui.Element {
	area := ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0)
	state := ui.Local(area, "flowlens-editable-grid", func() editableGridState { return editableGridState{} })
	if state == nil {
		return area
	}
	if open := state.open; open != nil && (open.index >= o.Rows || editableGridRowKey(o, open.index) != open.row || editableGridField(o, open.index, open.field) != open.text) {
		state.open = nil
		area.Focusable().TabStop(false).Focus()
	}
	area.Children(func() {
		list := ui.List(c.Key("rows"), &state.list, o.Rows, func(index int) {
			if index < o.Rows {
				editableGridRow(c, theme, state, o, index)
			}
		}).Fill().Label(o.Label)
		if bounds := list.Bounds(); bounds.W > 0 {
			state.width = bounds.W
		}
	})
	return area
}

func editableGridField(o EditableGridOptions, index int, field EditableGridField) *string {
	if field == EditableGridValue {
		return o.Value(index)
	}
	return o.Key(index)
}

func editableGridRowKey(o EditableGridOptions, index int) any {
	if o.RowKey == nil {
		return o.Key(index)
	}
	return o.RowKey(index)
}

// keyColumnWidth returns the key column's width, or 0 to share the room
// with the value column.
func (s *editableGridState) keyColumnWidth() float32 {
	if s.keyWidth <= 0 {
		return 0
	}
	return clampEditableGridKey(s.keyWidth, s.width)
}

func clampEditableGridKey(key, width float32) float32 {
	limit := max(EditableGridKeyMinWidth, width-2-2*EditableGridSideColumn-EditableGridValueMinWidth)
	return min(max(key, EditableGridKeyMinWidth), limit)
}

func editableGridRow(c *ui.Context, theme nui.Theme, state *editableGridState, o EditableGridOptions, index int) {
	top := float32(0)
	if index == 0 {
		top = 1
	}
	row := ui.Row(c.Key(editableGridRowKey(o, index))).FillWidth().Height(EditableGridRowHeight).AlignItems(ui.Stretch).Background(theme.Background)
	row.BorderWidth(top, 1, 1, 1).BorderColor(theme.Border)
	if row.Hovered() {
		row.Background(theme.Elevated)
	}
	if o.Row != nil {
		o.Row(index, row)
	}
	row.Children(func() {
		enable := ui.Row(c.Key("enabled")).Width(EditableGridSideColumn).Shrink(0).Center().BorderWidth(0, 1, 0, 0).BorderColor(theme.Border)
		enable.Children(func() {
			box := nui.Checkbox(c.Key("enabled"), theme, o.Enabled(index), nui.CheckboxOptions{})
			if o.EnabledLabel != nil {
				box.Label(o.EnabledLabel(index))
			}
		})
		key := ui.ButtonBase(c.Key("key-cell")).Justify(ui.Start).FocusRing(true).MinWidth(0).Gap(6).Padding(editableGridCellPadding).Cursor(ui.CursorText).BorderWidth(0, 1, 0, 0).BorderColor(theme.Border)
		if width := state.keyColumnWidth(); width > 0 {
			key.Width(width).Shrink(0)
		} else {
			key.Grow(1).Basis(0)
		}
		key.Children(func() {
			editableGridText(c, theme, *o.Key(index), o.KeyPlaceholder)
			if o.KeyTrailing != nil && !state.editing(index, EditableGridKey) {
				o.KeyTrailing(index)
			}
			if o.Resizable {
				editableGridResizer(c, theme, state, key)
			}
			editableGridTextCell(c, theme, state, o, key, index, EditableGridKey)
		})
		var value ui.Element
		if o.Value(index) != nil {
			value = ui.ButtonBase(c.Key("value-cell")).Justify(ui.Start).FocusRing(true)
		} else {
			value = ui.Row(c.Key("value-cell"))
		}
		value.Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Padding(editableGridCellPadding).BorderWidth(0, 1, 0, 0).BorderColor(theme.Border)
		if text := o.Value(index); text != nil {
			value.Cursor(ui.CursorText)
			value.Children(func() {
				editableGridText(c, theme, *text, o.ValuePlaceholder)
				editableGridTextCell(c, theme, state, o, value, index, EditableGridValue)
			})
		} else if o.ValueCell != nil {
			value.Children(func() { o.ValueCell(index) })
		}
		ui.Row(c.Key("actions")).Width(EditableGridSideColumn).Shrink(0).Center().Children(func() {
			if o.Actions != nil {
				o.Actions(index)
			}
		})
	})
}

func (s *editableGridState) editing(index int, field EditableGridField) bool {
	return s.open != nil && s.open.index == index && s.open.field == field
}

func editableGridText(c *ui.Context, theme nui.Theme, text, placeholder string) {
	color := theme.Text
	if text == "" {
		text, color = placeholder, theme.Muted
	}
	ui.Text(c, text).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(14).LineHeight(1.45).TextColor(color)
}

func editableGridTextCell(c *ui.Context, theme nui.Theme, state *editableGridState, o EditableGridOptions, cell ui.Element, index int, field EditableGridField) {
	label := o.KeyLabel
	if field == EditableGridValue {
		label = o.ValueLabel
	}
	if label != nil {
		cell.Label(label(index))
	}
	focus := ui.Local(cell, "cell-focus", func() ui.Handle { return ui.Handle{} })
	cell.Bind(focus)
	cell.OnClick(func() {
		text := editableGridField(o, index, field)
		state.open = &editableGridCell{row: editableGridRowKey(o, index), index: index, field: field, text: text, original: *text, focus: focus}
		state.select_, state.focused, state.committed, state.canceled, state.returnFocus = true, false, false, false, false
		state.composing = false
	})
	if state.editing(index, field) {
		// Keep the anchor in the native focus order, but skip its Tab stop
		// while its editor occupies that position.
		cell.TabStop(false)
		editableGridEditor(c, theme, state, cell)
	}
}

// editableGridResizer is the key column's edge; it highlights in the accent
// color while hovered, as 1.0.5's.
func editableGridResizer(c *ui.Context, theme nui.Theme, state *editableGridState, key ui.Element) {
	handle := ui.Box(c.Key("resize")).Absolute().Top(0).Bottom(0).Right(-3).Width(6).ZIndex(2).Cursor(ui.CursorResizeEW).Label("resize-key-column")
	if dx, _, held := handle.Dragged(); held && dx != 0 {
		width := state.keyWidth
		if width <= 0 {
			width = key.Bounds().W
		}
		state.keyWidth = clampEditableGridKey(width+dx, state.width)
	}
	if handle.Hovered() || handle.Pressed() {
		handle.Background(theme.Primary.Alpha(0.35))
	}
}

// editableGridEditor is the open cell's editor, above the grid so it can
// grow past the row. It edits the row's field in place.
func editableGridEditor(c *ui.Context, theme nui.Theme, state *editableGridState, cell ui.Element) {
	open := state.open
	text := open.text
	ui.Overlay(c, func() {
		// AttachTo keeps native Tab order next to the source cell and also
		// keeps a long editor inside the window when the grid is near an edge.
		box := ui.Box(c.Key(open)).AttachTo(cell, ui.AnchorTopStart, ui.AnchorTopStart).Left(8).Top(4).Width(max(cell.Bounds().W-16, 80))
		box.Shadow(0, 8, 20, 0, ui.RGBA(15, 23, 42, 0.12)).Children(func() {
			nui.Textarea(c, theme, text, nui.TextareaOptions{
				InputOptions: nui.InputOptions{Variant: nui.Outline, Label: "cell-editor", Style: func(_, editor, _, _ ui.Element) {
					editor.AutoFocus()
					// Composition confirmation must not become a grid command
					// even on platforms forwarding its final key to the input.
					editor.OnTextKey(func(k ui.TextKey) bool {
						if state.composing && (k.Key == ui.KeyEnter || k.Key == ui.KeyEscape) {
							return true
						}
						if k.Mods&^ui.Shift != 0 {
							return false
						}
						switch k.Key {
						case ui.KeyEnter:
							state.committed, state.returnFocus = true, true
							return true
						case ui.KeyEscape:
							state.committed, state.canceled, state.returnFocus = true, true, true
							return true
						}
						return false // Tab follows the native focus order.
					})
					focused := editor.Focused()
					if focused && state.select_ {
						editor.SetTextSelection(0, len([]rune(*text)))
						state.select_ = false
					}
					if state.focused && !focused {
						state.committed = true
					}
					state.focused = focused
				}},
				Rows: 1, AutoResize: true,
			}).FillWidth()
		})
		box.ObserveInput(func(ev ui.InputEvent) {
			switch ev.Kind {
			case ui.InputCompose:
				state.composing = ev.Text != ""
			case ui.InputText:
				state.composing = false
			}
		})
		// Blur is observed while building the Textarea. Hide this pass's
		// overlay immediately: an idle native window may not build another
		// frame just because state.open is cleared below.
		if state.committed {
			box.Hidden(true)
		}
	})
	if state.committed {
		if state.canceled {
			*text = open.original
		}
		if state.returnFocus {
			open.focus.Focus(c)
		}
		state.open = nil
	}
}
