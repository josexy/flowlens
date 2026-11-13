package widgets

import (
	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go"
)

// KeyValue is one row of a KeyValueTable.
type KeyValue struct {
	Key, Value string
}

// KeyValueTable shows rows in the order given, keeping duplicates, casing, and
// empty values. Text can be selected and copied.
func KeyValueTable(c *ui.Context, theme nui.Theme, rows []KeyValue) ui.Element {
	return ui.Column(c).FillWidth().Border(1, theme.Border).Background(theme.Background).Clip().Children(func() {
		for index, row := range rows {
			c.Key(index)
			line := ui.Row(c).FillWidth().MinHeight(40).AlignItems(ui.Stretch)
			if line.Hovered() {
				line.Background(theme.Elevated)
			}
			line.Children(func() {
				keyCell := ui.Row(c).BasisPercent(50).Grow(0).Shrink(0).MinWidth(0).AlignItems(ui.Center).Padding(6, 8)
				valueCell := ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Padding(6, 8)
				if index > 0 {
					keyCell.BorderWidth(1, 1, 0, 0)
					valueCell.BorderWidth(1, 0, 0, 0)
				} else {
					keyCell.BorderWidth(0, 1, 0, 0)
				}
				keyCell.BorderColor(theme.Border)
				valueCell.BorderColor(theme.Border)
				keyCell.Children(func() {
					ui.Text(c, row.Key).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.45).FontWeight(500).TextColor(theme.Text).Selectable()
				})
				valueCell.Children(func() {
					ui.Text(c, row.Value).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.45).TextColor(theme.Text).Selectable()
				})
			})
		}
	})
}
