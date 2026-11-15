package widgets

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Spinner keeps the loading status visible when animations are disabled.
func Spinner(c *ui.Context, theme nui.Theme) ui.Element {
	if !theme.Motion.Disabled {
		return ui.Spinner(c)
	}
	return nui.Icon(c, theme, lucide.LoaderCircle(), nui.IconOptions{
		Size: nui.Set(c.Theme().FontSize * 1.25), Color: nui.Set(theme.Muted),
	}).Role(ui.RoleProgress).Range(0, 1, -1)
}
