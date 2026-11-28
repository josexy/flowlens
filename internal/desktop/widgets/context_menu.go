package widgets

import (
	"slices"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// ContextMenu gives anchor a context menu drawn in the window,
// nui.ContextMenu, as 1.0.5's UContextMenu was: a right-click opens it at the
// pointer, and the Menu key or Shift+F10 at the anchor's corner. items builds
// the menu when it opens, its locale/revisions change or an action runs. The
// optional revisions must be comparable and cover changes to the items and
// their callbacks. A menu whose items come out empty
// does not open. Its OnSelect callbacks run after the build, as other
// actions do.
//
// state belongs to this anchor alone (MenuStates gives one to each row of a
// list). The menu takes over the anchor's HandleInput: an anchor that handles
// its input itself cannot have one. Key c in a list.
func ContextMenu(c *ui.Context, theme nui.Theme, anchor ui.Element, state *nui.MenuState, items func() []nui.MenuItem, o nui.MenuOptions, revisions ...any) ui.Element {
	if !o.MaxHeight.Set {
		// Let ordinary menus show every item when the window has room. nui
		// constrains and positions the scrollable popup within the viewport.
		_, height := c.Size()
		o.MaxHeight = nui.Set(max(float32(0), height-24))
	}
	prepareMenu(c, anchor, state, items, revisions, true)
	return nui.ContextMenu(c, theme, anchor, state, o)
}

// DropdownMenu submits options on initialization or a revision change, even
// while closed, so ArrowUp can focus the last item on its first opening.
// Each anchor owns its state; asynchronous providers include their revision.
func DropdownMenu(c *ui.Context, theme nui.Theme, anchor ui.Element, state *nui.MenuState, items func() []nui.MenuItem, o nui.MenuOptions, revisions ...any) ui.Element {
	prepareMenu(c, anchor, state, items, revisions, false)
	return nui.DropdownMenu(c, theme, anchor, state, o)
}

type menuSubmission struct {
	ready     bool
	submitted bool
	locale    string
	revisions []any
}

func prepareMenu(c *ui.Context, anchor ui.Element, state *nui.MenuState, build func() []nui.MenuItem, revisions []any, lazy bool) {
	if !anchor.Valid() {
		return
	}
	data := ui.Local(anchor, state, func() menuSubmission { return menuSubmission{} })
	if lazy && !state.Open {
		if data.submitted {
			state.SetItems(nil)
			*data = menuSubmission{}
		}
		return
	}
	if data.ready && data.locale == c.Locale() && slices.Equal(data.revisions, revisions) {
		return
	}
	items := build()
	if lazy && len(items) == 0 {
		state.Close()
	}
	var invalidateAfterAction func([]nui.MenuItem) []nui.MenuItem
	invalidateAfterAction = func(items []nui.MenuItem) []nui.MenuItem {
		items = slices.Clone(items)
		for i := range items {
			item := &items[i]
			if action := item.OnSelect; action != nil {
				item.OnSelect = func() { data.ready = false; action() }
			}
			if action := item.OnCheckedChange; action != nil {
				item.OnCheckedChange = func(checked bool) { data.ready = false; action(checked) }
			}
			item.Children = invalidateAfterAction(item.Children)
		}
		return items
	}
	state.SetItems(invalidateAfterAction(items))
	data.submitted = true
	data.ready, data.locale, data.revisions = true, c.Locale(), slices.Clone(revisions)
}

// MenuSeparator is a separator of a menu, which needs an ID of its own.
func MenuSeparator(id string) nui.MenuItem {
	return nui.MenuItem{ID: id, Kind: nui.MenuSeparator}
}

// MenuStates keeps one nui.MenuState for each row of a list that has a
// context menu, by the row's identity. A row's state is forgotten once its
// menu is closed and the row is no longer built.
type MenuStates struct {
	states map[string]*nui.MenuState
	built  map[string]bool
}

// For returns the menu state of the row key, and notes that it is built.
func (m *MenuStates) For(key string) *nui.MenuState {
	if m.states == nil {
		m.states, m.built = map[string]*nui.MenuState{}, map[string]bool{}
	}
	state := m.states[key]
	if state == nil {
		state = &nui.MenuState{}
		m.states[key] = state
	}
	m.built[key] = true
	return state
}

// Sweep forgets the closed menus of the rows not built since the last Sweep.
// Call it once a build, after the rows.
func (m *MenuStates) Sweep() {
	for key, state := range m.states {
		if !m.built[key] && !state.Open {
			state.Close()
			delete(m.states, key)
		}
	}
	clear(m.built)
}

// SweepHidden closes and forgets menus whose anchors were not built. Virtual
// sidebars use this so a removed or scrolled-out row cannot leave a stale menu.
func (m *MenuStates) SweepHidden() {
	for key, state := range m.states {
		if !m.built[key] {
			state.Close()
			*state = nui.MenuState{}
			delete(m.states, key)
		}
	}
	clear(m.built)
}

// Reset closes every menu before releasing the owner's state.
func (m *MenuStates) Reset() {
	for _, state := range m.states {
		state.Close()
		*state = nui.MenuState{}
	}
	m.states, m.built = nil, nil
}

// Menu icons are 20 DIP at the menus' MD size, and a swatch 12 DIP across.
const (
	menuIconSize   float32 = 20
	menuSwatchSize float32 = 12
)

// SwatchMenuItem is a menu item led by a round swatch of color, a hex as
// #ef4444, as 1.0.5 drew a highlight color: 12 DIP across with a 1 DIP
// outline of 20% black, where an item's icon goes. An item's Icon takes the
// color of its text, so the swatch is drawn rather than given as one.
func SwatchMenuItem(id, label, color string, onSelect func()) nui.MenuItem {
	return nui.MenuItem{ID: id, Label: label, OnSelect: onSelect, Build: func(c *ui.Context, _ nui.Theme) {
		ui.Box(c.Key("swatch")).Size(menuIconSize, menuIconSize).Shrink(0).Center().Children(func() {
			ui.Box(c.Key("dot")).Size(menuSwatchSize, menuSwatchSize).Radius(menuSwatchSize/2).
				Background(ui.Hex(color)).Border(1, ui.RGBA(0, 0, 0, 0.2)).Label("swatch " + color)
		})
		ui.Text(c.Key("label"), label).SingleLine().Grow(1).MinWidth(0)
	}}
}

// PointMenu is a context menu for an area that handles its input itself,
// whose HandleInput ContextMenu cannot take over: the area's handler passes
// it the input (Take), and the menu opens where the right-click went down,
// anchored to a point built in the area (Build).
type PointMenu struct {
	State    nui.MenuState
	x, y     float32
	revision uint64
}

// Take opens the menu at a right-click, at (ev.X, ev.Y) of the area, and
// reports whether it took ev.
func (m *PointMenu) Take(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputPointerDown || ev.Button != 1 {
		return false
	}
	m.x, m.y = ev.X, ev.Y
	m.revision++
	m.State.Open = true
	return true
}

// Build builds the menu among the area's children.
func (m *PointMenu) Build(c *ui.Context, theme nui.Theme, items func() []nui.MenuItem, o nui.MenuOptions, revisions ...any) {
	point := ui.Box(c.Key("menu-point")).Absolute().Left(m.x).Top(m.y).Size(0, 0).AccessHidden(true)
	ContextMenu(c.Key("menu"), theme, point, &m.State, items, o, append(revisions, m.revision)...)
}
