package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// TableHeaderDivider draws a one-device-pixel divider under a MyGo table header.
func TableHeaderDivider(c *ui.Context, theme nui.Theme, table ui.Element) {
	table.Children(func() {
		// MyGo places its one-DIP divider below an eight-spacing-unit header.
		ui.Box(c.Key("header-divider")).Absolute().Top(c.Theme().Space(8)).Left(0).Right(0).
			Height(1).IgnoreInput(true).Background(theme.Background).Draw(func(p *ui.Painter, bounds ui.Rect) {
			bounds.H = 1 / p.Scale()
			p.Fill(bounds, theme.BorderAccented, 0)
		})
	})
}
