package widgets

import (
	"maps"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Table builds a Nuxt table that fills its pane, with square edges and a
// header divider rendered by Nuxt. It returns the native Panel, rather than
// Nuxt's layout root, so input notices, focus and context menus target the table.
// Like any Element, the returned panel is valid only during this build.
func Table[T any](c *ui.Context, theme nui.Theme, state *nui.TableState[T], columns []nui.TableColumn[T], options nui.TableOptions) ui.Element {
	// Nuxt's header rule and MyGo's column dividers both use BorderAccented.
	// Match the page border within this table's theme snapshot.
	theme.BorderAccented = theme.Border
	// Nuxt draws its root border as an inset shadow. Disable it in the
	// recipe so it does not add an extra line beside a pane's divider.
	styles := make(nui.Styles, len(options.UI)+1)
	maps.Copy(styles, options.UI)
	root := styles[nui.Root]
	root.BorderWidth = nui.Set[float32](0)
	styles[nui.Root] = root
	options.UI = styles

	var panel ui.Element
	style := options.Style
	options.Style = func(slot nui.Slot, element ui.Element) {
		switch slot {
		case nui.Root:
			element.Fill().Radius(0)
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
