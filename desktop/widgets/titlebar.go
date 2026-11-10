package widgets

import (
	"runtime"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/resources"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	titleBarHeight     = 38
	titleBarIconSize   = 18
	windowControlWidth = 42
	windowControlIcon  = 16
)

var (
	appIconOnce   sync.Once
	appIconBitmap *ui.Bitmap
)

// appIcon returns the decoded application icon, or nil when it cannot be read.
func appIcon() *ui.Bitmap {
	appIconOnce.Do(func() {
		if bitmap, err := ui.DecodeBitmap(resources.AppIcon); err == nil {
			appIconBitmap = bitmap
		}
	})
	return appIconBitmap
}

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
	row := ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Stretch).Height(max(bar.Height, titleBarHeight)).Padding(0, bar.Right, 0, bar.Left).DragWindow()
	row.Background(theme.Background).BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderAccented)
	row.Children(func() {
		title := ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(10)
		if f.MacOS {
			title.Justify(ui.Center).PaddingX(88)
		} else {
			title.Padding(0, 0, 0, 12)
		}
		title.Children(func() {
			if icon := appIcon(); icon != nil {
				ui.Image(c, icon).Size(titleBarIconSize, titleBarIconSize).Fit(ui.Contain).Shrink(0)
			}
			weight := 600
			if f.MacOS {
				weight = 500
			}
			ui.Text(c, f.Title).SingleLine().Shrink(1).FontSize(14).FontWeight(weight).TextColor(theme.Text)
		})
		if !f.MacOS {
			f.controls(c, theme)
		}
	})
}

// closeHoverColor is the platform's color for a hovered close button.
func closeHoverColor() ui.Color {
	if runtime.GOOS == "windows" {
		return ui.Hex("#e81123")
	}
	return ui.Hex("#cc0000")
}

func (f WindowFrame) controls(c *ui.Context, theme nui.Theme) {
	maximize, maximizeLabel := lucide.Square(), "Maximize"
	if f.IsMaximized != nil && f.IsMaximized() {
		maximize, maximizeLabel = lucide.Copy(), "Restore"
	}
	control := func(key, label string, icon *ui.SVG, danger bool, run func()) ui.Element {
		button := ui.ButtonBase(c.Key(key)).Width(windowControlWidth).FillHeight().Center().Shrink(0).Label(label)
		color := theme.Text.Alpha(0.9)
		switch {
		case (button.Hovered() || button.Pressed()) && danger:
			button.Background(closeHoverColor())
			color = ui.Hex("#ffffff")
		case button.Hovered() || button.Pressed():
			button.Background(ui.RGBA(128, 128, 128, 0.15))
			color = theme.Text
		}
		button.Children(func() {
			ui.Icon(c, icon).Size(windowControlIcon, windowControlIcon).TextColor(color)
		})
		if run != nil {
			button.OnClick(run)
		}
		return button
	}
	control("minimize", "Minimize", lucide.Minus(), false, f.Minimize)
	control("maximize", maximizeLabel, maximize, false, f.ToggleMaximize).MaximizeButton()
	control("close", "Close", lucide.X(), true, f.Close)
}
