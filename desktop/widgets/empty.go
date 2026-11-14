package widgets

import (
	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// EmptyState is the borderless, small empty state the panes share: an icon,
// a title and an optional description, centered where nothing is to show.
func EmptyState(c *ui.Context, theme nui.Theme, options nui.EmptyOptions) ui.Element {
	options.Variant, options.Size = nui.None, nui.SM
	options.UI = nui.Styles{
		nui.Root:        {PaddingX: nui.Set[float32](16), PaddingY: nui.Set[float32](16), Gap: nui.Set[float32](12)},
		nui.Header:      {Gap: nui.Set[float32](6)},
		nui.Label:       {FontSize: nui.Set(apptheme.TextSize(c, 14)), LineHeight: nui.Set[float32](20.0 / 14.0)},
		nui.Description: {FontSize: nui.Set(apptheme.TextSize(c, 14)), LineHeight: nui.Set[float32](20.0 / 14.0)},
	}
	return nui.Empty(c, theme, options)
}
