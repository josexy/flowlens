package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const titleBarHeight = 38

// WindowFrame is the title bar a window draws in custom title bar mode.
type WindowFrame struct {
	Custom bool
	// MacOS keeps the native window controls.
	MacOS          bool
	Title          string
	Minimize       func()
	ToggleMaximize func()
	IsMaximized    func() bool
	Close          func()
}

// Build draws the title bar, or nothing in system title bar mode.
func (f WindowFrame) Build(c *ui.Context, theme nui.Theme) {
	if !f.Custom {
		return
	}
	bar := c.TitleBar()
	row := ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right, 0, bar.Left).DragWindow()
	row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	row.Children(func() {
		title := ui.Row(c).Grow(1).Basis(0).AlignItems(ui.Center).Gap(8).PaddingX(12)
		if f.MacOS {
			title.Justify(ui.Center)
		}
		title.Children(func() {
			ui.Text(c, f.Title).FontSize(14).Bold().TextColor(theme.Text)
		})
		if !f.MacOS {
			f.controls(c, theme)
		}
	})
}

func (f WindowFrame) controls(c *ui.Context, theme nui.Theme) {
	maximize := lucide.Square()
	if f.IsMaximized != nil && f.IsMaximized() {
		maximize = lucide.Copy()
	}
	control := func(key, label string, icon *ui.SVG, run func()) ui.Element {
		button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{
			Variant: nui.Ghost, Color: nui.Neutral, Square: true, Leading: icon, AccessibleLabel: label,
		})
		if run != nil {
			button.OnClick(run)
		}
		return button
	}
	control("minimize", "Minimize", lucide.Minus(), f.Minimize)
	control("maximize", "Maximize", maximize, f.ToggleMaximize).MaximizeButton()
	control("close", "Close", lucide.X(), f.Close)
}
