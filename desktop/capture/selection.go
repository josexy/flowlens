package capture

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
)

// rowSpan is where a row was in the last frame, in window coordinates.
type rowSpan struct {
	id     uint64
	bounds ui.Rect
}

// observePointer only locates context menus. MyGo owns left-button selection,
// modifiers, drag selection, and keyboard navigation.
func (t *Table) observePointer(ev ui.InputEvent) {
	if ev.Button == 1 && ev.Kind == ui.InputPointerDown {
		y := ev.Y
		t.menuPress = &y
	}
}

func (t *Table) noteRowSpan(anchor ui.Element, row int) {
	if t.menuPress != nil {
		t.menuSpans = append(t.menuSpans, rowSpan{id: t.rows[row].ID, bounds: anchor.Bounds()})
	}
}

func (t *Table) rowAt(top, y float32) uint64 {
	at := top + y
	for _, span := range t.menuSpans {
		if span.bounds.H > 0 && at >= span.bounds.Y && at < span.bounds.Y+span.bounds.H {
			return span.id
		}
	}
	return 0
}

func (t *Table) rowIndex(id uint64) int {
	if id == 0 {
		return -1
	}
	return slices.IndexFunc(t.rows, func(entry *logic.TrafficEntry) bool { return entry.ID == id })
}

// focusEntry updates inspection without opening a closed panel.
func (t *Table) focusEntry(entry *logic.TrafficEntry) {
	if t.source.SelectedEntry() == entry {
		return
	}
	t.source.SelectEntry(entry)
}

// syncChoice follows native selection without rewriting its cursor or anchor.
// The keyboard cursor may be on a deselected row; inspection must still refer
// to a selected entry, or close when the selection is empty.
func (t *Table) syncChoice(changed bool) {
	t.source.SetSelectedCount(t.selection.Len())
	if !changed {
		return
	}
	if t.selection.Len() == 0 {
		t.focusEntry(nil)
		return
	}
	if t.selectedRow >= 0 && t.selectedRow < len(t.rows) {
		if entry := t.rows[t.selectedRow]; t.selection.Has(entry.ID) {
			t.focusEntry(entry)
			return
		}
	}
	if focused := t.source.SelectedEntry(); focused != nil && t.selection.Has(focused.ID) {
		return
	}
	for _, entry := range t.rows {
		if t.selection.Has(entry.ID) {
			t.focusEntry(entry)
			return
		}
	}
}

func (t *Table) showDetails(entry *logic.TrafficEntry) {
	if entry == nil || t.rowIndex(entry.ID) < 0 || !t.selection.Has(entry.ID) {
		return
	}
	t.focusEntry(entry)
	t.source.SetShowDetail(true)
	t.focusRequested = true
}

func (t *Table) openFocusedDetails() {
	if t.selectedRow >= 0 && t.selectedRow < len(t.rows) {
		t.showDetails(t.rows[t.selectedRow])
	}
}

// CloseDetails hides inspection without changing selection and returns focus
// to the table after the workspace has removed the detail split.
func (t *Table) CloseDetails() {
	t.source.SetShowDetail(false)
	t.focusRequested = true
}
