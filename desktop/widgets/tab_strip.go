package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// TabStripOptions configures a TabStrip.
type TabStripOptions struct {
	// Label names the tabs for assistive technology.
	Label string
	// PaddingX insets the tabs from the strip's edges; the border still runs
	// the strip's full width.
	PaddingX float32
	// Trailing builds what sits at the strip's end, such as response chips.
	Trailing func()
	// OnChange, when set, reports the tab the user chooses. It runs after
	// the build, so a view that derives value afresh each build records
	// the choice here rather than by comparing value after TabStrip.
	OnChange func(id string)
}

// TabStrip is a row of underlined tabs whose bottom border runs across the
// whole strip, as FlowLens 1.0.5 drew request and response tabs, rather than
// only under the tabs. The tabs scroll sideways when they overflow, with the
// wheel too, and trailing content stays at the end.
func TabStrip(c *ui.Context, theme nui.Theme, value *string, items []nui.TabItem, o TabStripOptions) ui.Element {
	row := ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Stretch).Gap(8).PaddingX(o.PaddingX)
	row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Label("tab-strip")
	row.Children(func() {
		// The tabs reach 1 px into the row's border, so the active underline
		// covers it, as Nuxt's indicator covers its list's border.
		strip := ui.ScrollHorizontal(c.Key("strip")).Grow(1).Basis(0).MinWidth(0).Scrollbars(false).Margin(0, 0, -1, 0)
		WheelScroll(c, strip).Children(func() {
			nui.Tabs(c, theme, value, items, nui.TabsOptions{
				Label: o.Label, Content: nui.Set(false), Underline: true, OnChange: o.OnChange,
				Style: func(slot nui.Slot, element ui.Element) {
					if slot == nui.List {
						element.BorderColor(ui.Transparent)
					}
				},
			})
		})
		if o.Trailing != nil {
			ui.Row(c.Key("trailing")).Shrink(0).AlignItems(ui.Center).Gap(4).Children(o.Trailing)
		}
	})
	return row
}
