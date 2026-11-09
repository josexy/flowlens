package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// KeyValue is one row of a KeyValueTable.
type KeyValue struct {
	Key, Value string
}

// KeyValueTable shows rows in the order given, keeping duplicates, casing, and
// empty values. Text can be selected and copied.
func KeyValueTable(c *ui.Context, theme nui.Theme, rows []KeyValue) ui.Element {
	return ui.Column(c).FillWidth().Border(1, theme.Border).Radius(theme.Radius).Clip().Children(func() {
		for index, row := range rows {
			c.Key(index)
			ui.Row(c).FillWidth().AlignItems(ui.Stretch).Children(func() {
				keyCell := ui.Box(c).BasisPercent(35).Grow(0).Shrink(0).Padding(6, 8)
				valueCell := ui.Box(c).Grow(1).Basis(0).Padding(6, 8)
				if index > 0 {
					keyCell.BorderWidth(1, 1, 0, 0)
					valueCell.BorderWidth(1, 0, 0, 0)
				} else {
					keyCell.BorderWidth(0, 1, 0, 0)
				}
				keyCell.BorderColor(theme.Border)
				valueCell.BorderColor(theme.Border)
				keyCell.Children(func() {
					ui.Text(c, row.Key).Bold().TextColor(theme.Highlighted).Selectable()
				})
				valueCell.Children(func() {
					ui.Text(c, row.Value).TextColor(theme.Text).Selectable()
				})
			})
		}
	})
}
