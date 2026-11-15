package widgets

import (
	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// PaneToolbar keeps the content at the same height across traffic and editor tabs.
func PaneToolbar(c *ui.Context, leading, actions func()) {
	// Fit one line of pill tabs, their padding, and 2 DIP above and below.
	ui.Row(c).FillWidth().Shrink(0).Height(apptheme.TextSize(c, 16)+20).AlignItems(ui.Center).Justify(ui.End).Gap(8).Padding(2, 10).Children(func() {
		if leading != nil {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(leading)
		}
		if actions != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(4).Children(actions)
		}
	})
}

// PaneTitle builds a toolbar with a text label before the actions.
func PaneTitle(c *ui.Context, theme nui.Theme, title string, actions func()) {
	PaneToolbar(c, func() {
		ui.Text(c, title).Shrink(1).MinWidth(0).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
	}, actions)
}

// PaneContent fills the space below a toolbar with common side and bottom insets.
func PaneContent(c *ui.Context, content func()) {
	ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Padding(0, 10, 10, 10).Children(content)
}

// PaneWarning shows a notice below the content so it does not shift its top edge.
func PaneWarning(c *ui.Context, theme nui.Theme, message string) {
	if message == "" {
		return
	}
	ui.Box(c).FillWidth().Shrink(0).Padding(0, 10, 8, 10).Children(func() {
		nui.Alert(c, theme, nui.AlertOptions{Description: message, Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()})
	})
}
