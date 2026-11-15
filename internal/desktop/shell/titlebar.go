package shell

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func (a *App) titleBar(c *ui.Context, theme nui.Theme) {
	widgets.WindowFrame{
		Custom: a.Store.Settings.UsesCustomFrame(), MacOS: a.MacOS, Title: locales.AppTitle(),
		Minimize: a.Host.MinimizeWindow, ToggleMaximize: a.Host.ToggleMaximizeWindow, IsMaximized: a.Host.IsWindowMaximized, Close: a.Host.CloseWindow,
	}.Build(c, theme)
}
