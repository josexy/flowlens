package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// ContextMenu gives anchor a context menu drawn in the window,
// nui.ContextMenu, as 1.0.5's UContextMenu was: a right-click opens it at the
// pointer, and the Menu key or Shift+F10 at the anchor's corner. items builds
// the menu; it is called only while the menu is open, so rows that each have
// a menu cost nothing until one opens, and a menu whose items come out empty
// does not open. Its OnSelect callbacks run after the build, as other
// actions do.
//
// state belongs to this anchor alone (MenuStates gives one to each row of a
// list). The menu takes over the anchor's HandleInput: an anchor that handles
// its input itself cannot have one. Key c in a list.
func ContextMenu(c *ui.Context, theme nui.Theme, anchor ui.Element, state *nui.MenuState, items func() []nui.MenuItem, o nui.MenuOptions) ui.Element {
	var built []nui.MenuItem
	if state.Open {
		if built = items(); len(built) == 0 {
			state.Open = false
		}
	}
	return nui.ContextMenu(c, theme, anchor, state, built, o)
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
			delete(m.states, key)
		}
	}
	clear(m.built)
}

// Len returns how many menu states are kept.
func (m *MenuStates) Len() int { return len(m.states) }

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
	State nui.MenuState
	x, y  float32
}

// Take opens the menu at a right-click, at (ev.X, ev.Y) of the area, and
// reports whether it took ev.
func (m *PointMenu) Take(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputPointerDown || ev.Button != 1 {
		return false
	}
	m.x, m.y = ev.X, ev.Y
	m.State.Open = true
	return true
}

// Build builds the menu among the area's children.
func (m *PointMenu) Build(c *ui.Context, theme nui.Theme, items func() []nui.MenuItem, o nui.MenuOptions) {
	point := ui.Box(c.Key("menu-point")).Absolute().Left(m.x).Top(m.y).Size(0, 0).AccessHidden(true)
	ContextMenu(c.Key("menu"), theme, point, &m.State, items, o)
}
