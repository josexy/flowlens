package theme

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Begin applies the appearance and accessibility scale at a window's root.
// MyGo already handles monitor DPI. Windows' text scale is a separate setting:
// WebView scales the whole page with it, so fixed native geometry and text must
// follow it together. Consume that factor once, instead of scaling nui text a
// second time while leaving custom controls and code editors unchanged.
func Begin(c *ui.Context, config nui.AppConfig) nui.Theme {
	preferences := c.Preferences()
	scale := preferences.TextScale
	if scale <= 0 {
		scale = 1
	}
	c.SetZoom(scale)
	preferences.TextScale = 1
	return nui.Window(c, config.Resolve(c.Theme().Dark, preferences))
}
