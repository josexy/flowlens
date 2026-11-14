package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// Table builds a Nuxt table that fills its pane, with square edges and a
// one-device-pixel header divider. It returns the native Panel, rather than
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
			element.Grow(1).Margin(-y, -x).Padding(y, x)
		case nui.Panel:
			panel = element
			tableHeaderDivider(c, theme, element)
		}
		if style != nil {
			style(slot, element)
		}
	}
	nui.Table(c, theme, state, columns, options)
	return panel
}

func tableHeaderDivider(c *ui.Context, theme nui.Theme, table ui.Element) {
	table.Children(func() {
		// MyGo places its one-DIP divider below an eight-spacing-unit header.
		ui.Box(c.Key("header-divider")).Absolute().Top(c.Theme().Space(8)).Left(0).Right(0).
			Height(1).IgnoreInput(true).Background(theme.Background).Draw(func(p *ui.Painter, bounds ui.Rect) {
			bounds.H = 1 / p.Scale()
			p.Fill(bounds, theme.BorderAccented, 0)
		})
	})
}
