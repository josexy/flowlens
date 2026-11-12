package widgets

import "github.com/egoist/mygo/ui"

// RowActivator has the rows of a table open as buttons do, as 1.0.5's rows
// of console entries and SSE messages did: a click on a row, or Enter on the
// row the arrow keys moved to, activates it. Rows never show as chosen; while
// the keyboard has the focus, the keys' row shows the focus ring. A dialog
// a row opens gives the focus back to the table as it closes.
//
// MyGo's table reports no click on a row, and a choice of the row already
// chosen changes nothing, so the activator gives the table a choice it
// empties after each build: a click always chooses anew, and a release over
// the table tells a click from the keys.
type RowActivator struct {
	cursor    int
	selection ui.Selection[int]
	released  bool
	observer  func(ui.InputEvent)
}

// NewRowActivator returns an activator of list's rows, keyed by row index.
func NewRowActivator(list *ui.ListState) *RowActivator {
	a := &RowActivator{cursor: -1}
	a.observer = func(ev ui.InputEvent) {
		if ev.Kind == ui.InputPointerUp && ev.Button == 0 && !ev.Canceled {
			a.released = true
		}
	}
	list.Selected, list.Selection, list.DragSelect = &a.cursor, &a.selection, false
	return a
}

// Attach watches the pointer over table. Call it on the table on every
// build.
func (a *RowActivator) Attach(table ui.Element) ui.Element {
	return table.ObserveInput(a.observer)
}

// Activated returns the row of count rows a click or Enter activated in this
// build, if any. Call it after the table builds.
func (a *RowActivator) Activated(table ui.Element, count int) (int, bool) {
	clicked := a.released && a.selection.Len() > 0
	a.released = false
	a.selection.Clear()
	a.cursor = min(a.cursor, count-1)
	if a.cursor < 0 || !clicked && !table.Submitted() {
		return -1, false
	}
	return a.cursor, true
}
