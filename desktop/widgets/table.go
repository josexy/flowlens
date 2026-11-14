package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Table builds a Nuxt table that fills its pane, with square edges and a
// header divider rendered by Nuxt. It returns the native Panel, rather than
// Nuxt's layout root, so input notices, focus and context menus target the table.
// Like any Element, the returned panel is valid only during this build.
func Table[T any](c *ui.Context, theme nui.Theme, state *nui.TableState[T], columns []nui.TableColumn[T], options nui.TableOptions) ui.Element {
	var panel ui.Element
	style := options.Style
	options.Style = func(slot nui.Slot, element ui.Element) {
		switch slot {
		case nui.Root:
			element.Fill().Radius(0).Border(0, ui.Transparent)
		case nui.Item:
			// Extend Nuxt's cell content over the native padding so row
			// backgrounds and hit-test bounds still cover the entire cell.
			y, x := c.Theme().Space(1.5), c.Theme().Space(2.5)
			element.Grow(1).AlignSelf(ui.Stretch).Margin(-y, -x).Padding(y, x)
		case nui.Panel:
			panel = element
		}
		if style != nil {
			style(slot, element)
		}
	}
	nui.Table(c, theme, state, columns, options)
	return panel
}
