package capture

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
)

// The table selects rows as 1.0.5 did. MyGo's Selection keeps its keys, drag
// selection, edge scrolling and right-click choice, and the table rewrites
// what a click chose:
//
//   - A click selects its row alone, and on the only row selected clears the
//     selection.
//   - Shift-click adds its row or takes it out.
//   - Ctrl-click (Cmd-click on macOS) selects a row not selected alone, and
//     opens, closes or moves the detail panel.
//   - A drag selects the rows from the one pressed to the one under the
//     pointer, in place of the others.
//
// MyGo has no hook for a row's click, so the table watches presses with
// ObserveInput, holds the selection as it was while a press is down, and
// applies the click once MyGo has handled it.

// rowPress is a press of the primary button on the table.
type rowPress struct {
	// down is set while the press is held.
	down bool
	// fresh is set until the build after the press, which resolves it.
	fresh bool
	mods  ui.Modifiers
	// y is where the press went down, relative to the table.
	y float32
	// row is the entry pressed, or 0 until known.
	row uint64
	// onRow is set once MyGo took the press as a row's.
	onRow bool
	// dragged is set once a drag selection started from the press.
	dragged bool
}

// rowClick is a press released without a drag.
type rowClick struct {
	mods  ui.Modifiers
	y     float32
	row   uint64
	onRow bool
}

// rowSpan is where a row was in the last frame, in window coordinates.
type rowSpan struct {
	id     uint64
	bounds ui.Rect
}

// rowGesture is the state of the click model between frames.
type rowGesture struct {
	press  rowPress
	clicks []rowClick
	// probing is set while a build finds whether MyGo took a press or a
	// release as a row's: Selected starts it at -1, and MyGo moves it to the
	// row it chose.
	probing bool
	// snapshot and lead are the selection and the entry of Selected as they
	// were before MyGo saw the gesture.
	snapshot []uint64
	lead     uint64
	spans    []rowSpan
	// dragging is whether a drag selection went on in the last build.
	dragging bool
}

// observePointer notes presses and releases of the primary button over the
// table. It runs between frames, before MyGo handles them.
func (t *Table) observePointer(ev ui.InputEvent) {
	if ev.Button != 0 {
		return
	}
	g := &t.gesture
	switch ev.Kind {
	case ui.InputPointerDown:
		g.press = rowPress{down: true, fresh: true, mods: ev.Mods, y: ev.Y}
	case ui.InputPointerUp:
		press := g.press
		if !press.down {
			return
		}
		g.press.down = false
		if ev.Canceled || press.dragged {
			return
		}
		g.clicks = append(g.clicks, rowClick{mods: press.mods, y: press.y, row: press.row, onRow: press.onRow})
	}
}

// holding reports whether a press MyGo took as a row's is down without a
// drag: the selection stays as it was until the release.
func (g *rowGesture) holding() bool {
	return g.press.down && g.press.onRow && !g.press.dragged
}

// beginSelection prepares a build that may see a press or a click: it keeps
// the selection as it is, and starts Selected at -1 to learn which row MyGo
// chooses.
func (t *Table) beginSelection() {
	g := &t.gesture
	g.spans = g.spans[:0]
	g.probing = false
	if !g.press.fresh && len(g.clicks) == 0 && !g.holding() {
		return
	}
	g.snapshot = g.snapshot[:0]
	for id := range t.selection.All() {
		g.snapshot = append(g.snapshot, id)
	}
	g.lead = 0
	if t.selectedRow >= 0 && t.selectedRow < len(t.rows) {
		g.lead = t.rows[t.selectedRow].ID
	}
	g.probing = g.press.fresh || len(g.clicks) > 0
	if g.probing {
		t.selectedRow = -1
	}
}

// noteRowSpan records where row was in the last frame while a build resolves
// a press. anchor is a box of the row that every frame builds.
func (t *Table) noteRowSpan(anchor ui.Element, row int) {
	if t.gesture.probing {
		t.gesture.spans = append(t.gesture.spans, rowSpan{id: t.rows[row].ID, bounds: anchor.Bounds()})
	}
}

// rowAt returns the entry whose row was at y, relative to the table at top,
// in the last frame, or 0.
func (t *Table) rowAt(top, y float32) uint64 {
	at := top + y
	for _, span := range t.gesture.spans {
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

// restoreSelection puts back the selection MyGo started the build with.
func (t *Table) restoreSelection() {
	g := &t.gesture
	t.selection.Clear()
	for _, id := range g.snapshot {
		t.selection.Add(id)
	}
	t.selectedRow = t.rowIndex(g.lead)
}

// resolveSelection applies the click model after the table built, and
// reports whether it handled the selection this build.
func (t *Table) resolveSelection(table ui.Element) bool {
	g := &t.gesture
	dragging := t.list.DragSelecting()
	finishedDrag := g.dragging && !dragging
	g.dragging = dragging
	chosen := -1
	if g.probing && t.selectedRow >= 0 && t.selectedRow < len(t.rows) {
		chosen = t.selectedRow
	}
	top := table.Bounds().Y
	if g.press.fresh {
		g.press.fresh = false
		g.press.row = t.rowAt(top, g.press.y)
		// MyGo chooses a row not selected as the pointer goes down.
		if chosen >= 0 && g.press.down {
			g.press.onRow = true
			g.press.row = t.rows[chosen].ID
		}
	}
	if dragging && g.press.down && !g.press.dragged {
		g.press.dragged, g.press.onRow = true, true
	}
	switch {
	case dragging || finishedDrag:
		g.probing = false
		t.selectDraggedRange()
		if finishedDrag {
			g.press.down = false
		}
		if finishedDrag && t.selectedRow >= 0 && t.selectedRow < len(t.rows) && t.selection.Has(t.rows[t.selectedRow].ID) {
			t.focusEntry(t.rows[t.selectedRow])
		}
		return true
	case g.probing || g.holding():
		t.restoreSelection()
	default:
		return false
	}
	g.probing = false
	clicks := g.clicks
	g.clicks = g.clicks[:0]
	for _, click := range clicks {
		// MyGo moved Selected to the row it clicked: a release elsewhere, as
		// on a header or a scroll bar, clicks no row.
		if !click.onRow && chosen < 0 {
			continue
		}
		row := click.row
		if chosen >= 0 {
			row = t.rows[chosen].ID
		}
		if row == 0 {
			row = t.rowAt(top, click.y)
		}
		t.applyClick(click.mods, row)
	}
	return true
}

// selectDraggedRange makes the rows from the one pressed to Selected the
// selection, as 1.0.5's drag did whatever the modifiers.
func (t *Table) selectDraggedRange() {
	from, to := t.rowIndex(t.gesture.press.row), t.selectedRow
	if from < 0 || to < 0 || to >= len(t.rows) {
		return
	}
	t.selection.Clear()
	for _, entry := range t.rows[min(from, to) : max(from, to)+1] {
		t.selection.Add(entry.ID)
	}
}

// focusEntry makes entry the one the detail panel shows; nil closes it.
func (t *Table) focusEntry(entry *logic.TrafficEntry) {
	if entry == nil && t.source.SelectedEntry() == nil {
		return
	}
	t.source.SelectEntry(entry)
}

// applyClick applies a click with mods on the row of entry id.
func (t *Table) applyClick(mods ui.Modifiers, id uint64) {
	index := t.rowIndex(id)
	if index < 0 {
		return
	}
	entry := t.rows[index]
	focused := t.source.SelectedEntry()
	isFocused := focused != nil && focused.ID == entry.ID
	switch {
	case mods&ui.Shift != 0:
		if !t.selection.Has(entry.ID) {
			t.selection.Add(entry.ID)
			t.selectedRow = index
			t.focusEntry(entry)
			return
		}
		t.selection.Remove(entry.ID)
		if t.selection.Len() == 0 {
			t.selectedRow = -1
			t.focusEntry(nil)
			return
		}
		t.selectedRow = index
		if isFocused {
			for _, row := range t.rows {
				if t.selection.Has(row.ID) {
					t.focusEntry(row)
					break
				}
			}
		}
	case mods&ui.Cmd != 0:
		if !t.selection.Has(entry.ID) {
			t.replaceSelection(index)
		}
		t.selectedRow = index
		if isFocused {
			t.source.SetShowDetail(!t.source.ShowDetail())
			return
		}
		t.focusEntry(entry)
		t.source.SetShowDetail(true)
	default:
		if t.selection.Len() == 1 && t.selection.Has(entry.ID) {
			t.selection.Clear()
			t.selectedRow = -1
			t.focusEntry(nil)
			return
		}
		t.replaceSelection(index)
		t.focusEntry(entry)
	}
}

// syncChoice follows the selection MyGo changed itself, by the keys, Select
// All or a right-click: the focus moves to Selected, or goes with the
// selection. The detail panel stays as it was.
func (t *Table) syncChoice(changed bool) {
	count := t.selection.Len()
	t.source.SetSelectedCount(count)
	if !changed {
		return
	}
	if count == 0 {
		t.focusEntry(nil)
		return
	}
	if t.selectedRow < 0 || t.selectedRow >= len(t.rows) {
		return
	}
	if entry := t.rows[t.selectedRow]; t.selection.Has(entry.ID) {
		t.focusEntry(entry)
	}
}
