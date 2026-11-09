package app

import (
	"runtime"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/josexy/flowlens/desktop/widgets"

	"github.com/josexy/flowlens/backend/pkg/logger"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

const (
	settingsWindowName          = "settings"
	settingsWindowDefaultWidth  = 900
	settingsWindowDefaultHeight = 680
	settingsWindowMinWidth      = 760
	settingsWindowMinHeight     = 560
	updaterWindowName           = "updater"
	updaterWindowDefaultWidth   = 600
	updaterWindowDefaultHeight  = 640
	updaterWindowMinWidth       = 520
	updaterWindowMinHeight      = 520
)

// secondaryFrame prepares a Settings or Software Update window for the active
// title bar mode and returns its title bar.
func secondaryFrame(options *mygo.WindowOptions, custom bool, title string, window func() *mygo.Window) widgets.WindowFrame {
	macOS := runtime.GOOS == "darwin"
	if custom {
		options.TitleBarStyle = mygo.TitleBarHidden
		options.HideWindowButtons = !macOS
	}
	act := func(run func(*mygo.Window)) func() {
		return func() {
			if current := window(); current != nil {
				run(current)
			}
		}
	}
	return widgets.WindowFrame{
		Custom: custom, MacOS: macOS, Title: title,
		Minimize: act((*mygo.Window).Minimize),
		ToggleMaximize: act(func(current *mygo.Window) {
			if current.IsMaximized() {
				current.Unmaximize()
			} else {
				current.Maximize()
			}
		}),
		IsMaximized: func() bool {
			current := window()
			return current != nil && current.IsMaximized()
		},
		Close: act((*mygo.Window).Close),
	}
}

// windowRect is a rectangle in screen coordinates.
type windowRect struct {
	X, Y, Width, Height int
}

func (r windowRect) intersects(other windowRect) bool {
	return r.Width > 0 && r.Height > 0 && other.Width > 0 && other.Height > 0 &&
		r.X < other.X+other.Width && other.X < r.X+r.Width &&
		r.Y < other.Y+other.Height && other.Y < r.Y+r.Height
}

// windowPlacement is where a window opens: centered unless Positioned.
type windowPlacement struct {
	Width, Height int
	X, Y          int
	Positioned    bool
}

// mainWindowPlacement resolves the saved configuration against the connected
// displays' work areas. Maximized and full-screen states are restored after the
// window exists, once it has its normal bounds.
func mainWindowPlacement(windowConfig *settingservice.WindowConfig, screens []windowRect) windowPlacement {
	placement := windowPlacement{
		Width:  appservice.DEFAULT_WINDOW_WIDTH,
		Height: appservice.DEFAULT_WINDOW_HEIGHT,
	}
	if windowConfig == nil {
		return placement
	}
	placement.Width = clampMin(windowConfig.Width, appservice.MIN_WINDOW_WIDTH)
	placement.Height = clampMin(windowConfig.Height, appservice.MIN_WINDOW_HEIGHT)
	if hasWindowPosition(windowConfig) && isWindowRectVisible(windowConfig, screens) {
		placement.Positioned = true
		placement.X = windowConfig.PositionX
		placement.Y = windowConfig.PositionY
	}
	return placement
}

// geometryWindow is what saving and restoring geometry needs from a window.
type geometryWindow interface {
	IsMaximized() bool
	IsFullScreen() bool
	Size() (width, height int)
	Position() (x, y int)
	Maximize()
	SetFullScreen(bool)
	SetPosition(x, y int)
}

func currentWindowConfig(settingSvc *settingservice.SettingService) *settingservice.WindowConfig {
	windowConfig, err := settingSvc.GetWindowConfig()
	if err != nil || windowConfig == nil {
		return nil
	}
	copied := *windowConfig
	return &copied
}

func windowFrameModeFromConfig(windowConfig *settingservice.WindowConfig) settingservice.WindowFrameMode {
	if windowConfig == nil {
		return settingservice.WindowFrameModeCustom
	}
	if windowConfig.FrameMode == settingservice.WindowFrameModeSystem {
		return settingservice.WindowFrameModeSystem
	}
	return settingservice.WindowFrameModeCustom
}

func currentMainWindowCloseBehavior(settingSvc *settingservice.SettingService) settingservice.MainWindowCloseBehavior {
	behavior, err := settingservice.GetMainWindowCloseBehavior(settingSvc)
	if err != nil {
		return settingservice.MainWindowCloseBehaviorHideToTray
	}
	return normalizeMainWindowCloseBehavior(behavior)
}

func normalizeMainWindowCloseBehavior(behavior settingservice.MainWindowCloseBehavior) settingservice.MainWindowCloseBehavior {
	if behavior == settingservice.MainWindowCloseBehaviorQuit {
		return settingservice.MainWindowCloseBehaviorQuit
	}
	return settingservice.MainWindowCloseBehaviorHideToTray
}

func updateWindowConfigFromRuntimeIfStartupReady(
	settingSvc *settingservice.SettingService,
	window geometryWindow,
	windowStartupReady *atomic.Bool,
) {
	if windowStartupReady == nil || !windowStartupReady.Load() {
		return
	}
	updateWindowConfigFromRuntime(settingSvc, window)
}

func restoreSavedWindowState(windowConfig *settingservice.WindowConfig, window geometryWindow, screens []windowRect) {
	if windowConfig == nil || window == nil {
		return
	}
	if windowConfig.IsFullScreen {
		window.SetFullScreen(true)
		return
	}
	if windowConfig.IsMaximized {
		window.Maximize()
		return
	}
	restoreSavedWindowPosition(windowConfig, window, screens)
}

func restoreSavedWindowPosition(windowConfig *settingservice.WindowConfig, window geometryWindow, screens []windowRect) {
	if hasWindowPosition(windowConfig) && isWindowRectVisible(windowConfig, screens) {
		window.SetPosition(windowConfig.PositionX, windowConfig.PositionY)
	}
}

func hasWindowPosition(windowConfig *settingservice.WindowConfig) bool {
	return windowConfig.HasPosition || windowConfig.PositionX != 0 || windowConfig.PositionY != 0
}

func updateWindowConfigFromRuntime(settingSvc *settingservice.SettingService, window geometryWindow) {
	if window == nil {
		return
	}

	windowConfig, err := settingSvc.GetWindowConfig()
	if err != nil || windowConfig == nil {
		windowConfig = &settingservice.WindowConfig{}
	} else {
		copied := *windowConfig
		windowConfig = &copied
	}

	isMaximized := window.IsMaximized()
	isFullScreen := window.IsFullScreen()
	windowConfig.IsMaximized = isMaximized
	windowConfig.IsFullScreen = isFullScreen

	width, height := window.Size()
	if !isMaximized && !isFullScreen && width >= appservice.MIN_WINDOW_WIDTH && height >= appservice.MIN_WINDOW_HEIGHT {
		x, y := window.Position()
		windowConfig.PositionX = x
		windowConfig.PositionY = y
		windowConfig.Width = width
		windowConfig.Height = height
		windowConfig.HasPosition = true
	}

	if err := settingSvc.UpdateWindowConfig(windowConfig); err != nil {
		logger.G().Errorf("Update window config failed: %v", err)
	}
}

func isWindowRectVisible(windowConfig *settingservice.WindowConfig, screens []windowRect) bool {
	if len(screens) == 0 {
		return true
	}
	rect := windowRect{
		X:      windowConfig.PositionX,
		Y:      windowConfig.PositionY,
		Width:  clampMin(windowConfig.Width, appservice.MIN_WINDOW_WIDTH),
		Height: clampMin(windowConfig.Height, appservice.MIN_WINDOW_HEIGHT),
	}
	for _, screen := range screens {
		if rect.intersects(screen) {
			return true
		}
	}
	return false
}

func clampMin(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
