package app

import (
	"sync/atomic"

	"github.com/josexy/flowlens/backend/pkg/logger"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	settingsWindowName          = "settings"
	settingsWindowDefaultWidth  = 900
	settingsWindowDefaultHeight = 680
	settingsWindowMinWidth      = 760
	settingsWindowMinHeight     = 560
)

func settingsWindowOptions(useCustomWindowFrame bool, isMacOS bool, icon []byte) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:                       settingsWindowName,
		Title:                      appservice.APP_NAME + " Settings",
		Width:                      settingsWindowDefaultWidth,
		Height:                     settingsWindowDefaultHeight,
		MinWidth:                   settingsWindowMinWidth,
		MinHeight:                  settingsWindowMinHeight,
		StartState:                 application.WindowStateNormal,
		Frameless:                  useCustomWindowFrame && !isMacOS,
		BackgroundColour:           application.NewRGBA(36, 36, 41, 255),
		BackgroundType:             application.BackgroundTypeSolid,
		DefaultContextMenuDisabled: false,
		URL:                        "/#/settings",
		InitialPosition:            application.WindowCentered,
		UseApplicationMenu:         isMacOS,
		Mac: application.MacWindow{
			TitleBar: macTitleBarForFrameMode(useCustomWindowFrame),
			Backdrop: application.MacBackdropNormal,
		},
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: false,
		},
		Linux: application.LinuxWindow{
			Icon:                icon,
			WebviewGpuPolicy:    application.WebviewGpuPolicyOnDemand,
			WindowIsTranslucent: useCustomWindowFrame,
		},
	}
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

func macTitleBarForFrameMode(useCustomWindowFrame bool) application.MacTitleBar {
	if useCustomWindowFrame {
		return application.MacTitleBarHiddenInset
	}
	return application.MacTitleBarDefault
}

func applySavedWindowOptions(options *application.WebviewWindowOptions, windowConfig *settingservice.WindowConfig, screens []*application.Screen) {
	if windowConfig == nil {
		return
	}
	options.Width = clampMin(windowConfig.Width, appservice.MIN_WINDOW_WIDTH)
	options.Height = clampMin(windowConfig.Height, appservice.MIN_WINDOW_HEIGHT)
	if hasWindowPosition(windowConfig) && isWindowRectVisible(windowConfig, screens) {
		options.InitialPosition = application.WindowXY
		options.X = windowConfig.PositionX
		options.Y = windowConfig.PositionY
	} else {
		options.InitialPosition = application.WindowCentered
	}

	// Wails applies StartState before InitialPosition on Windows. Restoring maximized
	// state after runtime ready avoids moving a maximized window back to saved X/Y.
	options.StartState = application.WindowStateNormal
	if shouldShowWindowAfterFrontendReady(windowConfig) {
		options.Hidden = true
	}
}

func shouldShowWindowAfterFrontendReady(windowConfig *settingservice.WindowConfig) bool {
	return windowConfig != nil && (windowConfig.IsMaximized || windowConfig.IsFullScreen)
}

func shouldRestoreWindowStateOnRuntimeReady(windowConfig *settingservice.WindowConfig) bool {
	return !shouldShowWindowAfterFrontendReady(windowConfig)
}

func updateWindowConfigFromRuntimeIfStartupReady(
	settingSvc *settingservice.SettingService,
	window *application.WebviewWindow,
	windowStartupReady *atomic.Bool,
) {
	if windowStartupReady == nil || !windowStartupReady.Load() {
		return
	}
	updateWindowConfigFromRuntime(settingSvc, window)
}

func restoreSavedWindowState(windowConfig *settingservice.WindowConfig, window *application.WebviewWindow, screens []*application.Screen) {
	if windowConfig == nil || window == nil {
		return
	}
	if windowConfig.IsFullScreen {
		window.Fullscreen()
		return
	}
	if windowConfig.IsMaximized {
		window.Maximise()
		return
	}
	restoreSavedWindowPosition(windowConfig, window, screens)
}

func restoreSavedWindowPosition(windowConfig *settingservice.WindowConfig, window *application.WebviewWindow, screens []*application.Screen) {
	if hasWindowPosition(windowConfig) && isWindowRectVisible(windowConfig, screens) {
		window.SetPosition(windowConfig.PositionX, windowConfig.PositionY)
	}
}

func hasWindowPosition(windowConfig *settingservice.WindowConfig) bool {
	return windowConfig.HasPosition || windowConfig.PositionX != 0 || windowConfig.PositionY != 0
}

func updateWindowConfigFromRuntime(settingSvc *settingservice.SettingService, window *application.WebviewWindow) {
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

	isMaximized := window.IsMaximised()
	isFullScreen := window.IsFullscreen()
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

func isWindowRectVisible(windowConfig *settingservice.WindowConfig, screens []*application.Screen) bool {
	if len(screens) == 0 {
		return true
	}
	windowRect := application.Rect{
		X:      windowConfig.PositionX,
		Y:      windowConfig.PositionY,
		Width:  clampMin(windowConfig.Width, appservice.MIN_WINDOW_WIDTH),
		Height: clampMin(windowConfig.Height, appservice.MIN_WINDOW_HEIGHT),
	}
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		if !windowRect.Intersect(screen.WorkArea).IsEmpty() {
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
