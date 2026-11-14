package widgets

import (
	"slices"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// ReorderListState keeps native list drags tied to the order and revision at
// pointer-down. Refreshes and filtering invalidate a drag without moving data.
type ReorderListState struct {
	list     ui.ListState
	ids      []string
	revision string
	enabled  bool
	armed    bool
}

// Build uses MyGo's row preview, insertion marker, edge scrolling and cancel
// handling. IDs are the reorderable prefix; any remaining rows are drafts.
// Configure the returned element before calling Rows.
func (s *ReorderListState) Build(c *ui.Context, ids []string, revision string, enabled bool, count int, reorder func([]string)) ui.Element {
	if !slices.Equal(ids, s.ids) || revision != s.revision || !enabled {
		s.armed = false
	}
	s.ids, s.revision, s.enabled = slices.Clone(ids), revision, enabled
	s.list.KeepOffset = true
	s.list.Reorder = nil
	if enabled {
		s.list.Reorder = func(rows []int, to int) {
			if !s.armed || len(rows) != 1 || rows[0] < 0 || rows[0] >= len(ids) {
				return
			}
			s.armed = false
			from := rows[0]
			to = min(max(to, 0), len(ids))
			if to > from {
				to--
			}
			if to == from {
				return
			}
			next := slices.Delete(slices.Clone(ids), from, from+1)
			next = slices.Insert(next, to, ids[from])
			reorder(next)
		}
	}
	return ui.List(c, &s.list, count).ObserveInput(s.observe)
}

func (s *ReorderListState) observe(event ui.InputEvent) {
	if event.Kind == ui.InputPointerDown && event.Button == 0 {
		s.armed = s.enabled
	}
	if event.Kind == ui.InputPointerUp && event.Canceled {
		s.armed = false
	}
}

// ReorderGrip is the common affordance for native list reordering. MyGo
// transfers a moving press from this control to its enclosing list row.
func ReorderGrip(c *ui.Context, theme nui.Theme, label string, enabled bool) {
	grip := ui.Icon(c, lucide.GripVertical()).Size(14, 14).Shrink(0).TextColor(theme.Dimmed).Label(label).OnClick(func() {})
	if enabled {
		grip.Cursor(ui.CursorGrab)
	} else {
		grip.Opacity(0.4)
		// Keep a disabled grip's pointer moves out of the enclosing row.
		grip.Dragged()
	}
}
