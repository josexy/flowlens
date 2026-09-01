package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
	"github.com/josexy/flowlens/backend/pkg/logger"
	apicollectionservice "github.com/josexy/flowlens/backend/services/api_collection_service"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	historyservice "github.com/josexy/flowlens/backend/services/history_service"
	loggingservice "github.com/josexy/flowlens/backend/services/logging_service"
	memstatsservice "github.com/josexy/flowlens/backend/services/mem_stats_service"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/backend/services/shortcut_service"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type Assets struct {
	Frontend         fs.FS
	AppIcon          []byte
	TrayTemplateIcon []byte
}

const (
	trayLabelsEventName          = "app:tray-labels"
	openSettingsWindowEventName  = "app:open-settings-window"
	settingsWindowDirtyEventName = "app:settings-window-dirty-changed"
	confirmQuitRequestEventName  = "app:confirm-quit-request"
	quitConfirmedEventName       = "app:quit-confirmed"
	shutdownRequestedEventName   = "app:shutdown-requested"
	shutdownUIReadyEventName     = "app:shutdown-ui-ready"
	shortcutsChangedEventName    = "app:shortcuts-changed"
	shortcutInvokeEventName      = "app:shortcut-invoke"
	globalShortcutDebounce       = 250 * time.Millisecond
	shutdownMinimumVisible       = 300 * time.Millisecond
	shutdownUIReadyTimeout       = 2 * time.Second
)

func Run(assets Assets) {

	isMacOS := runtime.GOOS == "darwin"
	logger.EnableBootstrapOutput(os.Stderr)

	var tray *application.SystemTray
	var settingSvc *settingservice.SettingService
	var shortcutSvc *shortcutservice.ShortcutService
	var pythonPluginSvc *pythonpluginservice.PythonPluginService
	var mainWindowRef atomic.Pointer[application.WebviewWindow]
	var pendingShowMainWindow atomic.Bool
	var windowStartupReady atomic.Bool
	var mainFrontendReady atomic.Bool
	var shutdownEventDispatched atomic.Bool
	var shutdownCoordinator *gracefulShutdownCoordinator
	var requestApplicationQuit func()
	showMainWindow := func() {
		mainWindow := mainWindowRef.Load()
		if mainWindow == nil {
			pendingShowMainWindow.Store(true)
			return
		}
		if mainWindow.IsMinimised() {
			mainWindow.UnMinimise()
		}
		mainWindow.Show().Focus()
	}
	showPendingMainWindow := func() {
		if !pendingShowMainWindow.Load() || !windowStartupReady.Load() {
			return
		}
		if mainWindowRef.Load() == nil {
			return
		}
		pendingShowMainWindow.Store(false)
		showMainWindow()
	}
	requestShowMainWindow := func() {
		pendingShowMainWindow.Store(true)
		showPendingMainWindow()
	}
	appOptions := application.Options{
		Name:        appservice.APP_NAME,
		Description: "FlowLens",
		Icon:        assets.AppIcon,
		Logger:      logger.WailsLogger(),
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceUniqueID(),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				logger.G().Infof("Second FlowLens instance launch requested, showing main window: workingDir=%q", data.WorkingDir)
				requestShowMainWindow()
			},
			ExitCode: 0,
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets.Frontend),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Linux: application.LinuxOptions{
			ProgramName:                   appservice.APP_NAME,
			DisableQuitOnLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		ShouldQuit: func() bool {
			if shutdownCoordinator != nil && shutdownCoordinator.CanQuit() {
				return true
			}
			if requestApplicationQuit == nil {
				return true
			}
			requestApplicationQuit()
			return false
		},
		OnShutdown: func() {
			logger.G().Info("Application shutdown started")
			if shutdownCoordinator != nil {
				if err := shutdownCoordinator.PrepareAndWait(); err != nil && !shutdownCoordinator.CanQuit() {
					logger.G().Errorf("Fallback shutdown preparation completed with errors: %v", err)
				}
			} else {
				if shortcutSvc != nil {
					if err := shortcutSvc.Shutdown(); err != nil {
						logger.G().Warnf("Release global shortcuts on shutdown failed: %v", err)
					}
				}
				if settingSvc != nil {
					if err := settingSvc.Save(); err != nil {
						logger.G().Errorf("Save settings on shutdown failed: %v", err)
					}
				}
				if pythonPluginSvc != nil {
					if err := pythonPluginSvc.Shutdown(); err != nil {
						logger.G().Warnf("Stop Python plugin workers on shutdown failed: %v", err)
					}
				}
			}
			if tray != nil {
				tray.Destroy()
			}
			logger.G().Info("Application shutdown completed")
		},
	}
	var mainWindow *application.WebviewWindow
	app := application.New(appOptions)

	appSvc := appservice.New()
	db, err := appdatabase.Open()
	if err != nil {
		reportStartupFailure("open application database", err)
		return
	}
	settingSvc = settingservice.New(db)
	if err := settingSvc.Load(); err != nil {
		_ = db.Close()
		reportStartupFailure("load application settings", err)
		return
	}
	logConfig, err := settingservice.GetLogConfig(settingSvc)
	if err != nil {
		logConfig = settingservice.LogConfig{
			Enabled: true,
			Level:   string(logger.DefaultLogLevel()),
		}
	}
	if _, err = logger.Configure(logger.Config{
		Enabled:      logConfig.Enabled,
		Level:        logger.NormalizeLogLevel(logConfig.Level),
		LogDir:       logger.DefaultLogDir(),
		MaxSizeBytes: logger.DefaultLogMaxSizeBytes,
		MaxBackups:   logger.DefaultLogMaxBackups,
		Console:      logger.DebugMode(),
		DebugMode:    logger.DebugMode(),
	}); err != nil {
		logger.G().Errorf("Configure logger failed: %v", err)
	}
	defer logger.Close()
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.G().Warnf("Close application database failed: %v", closeErr)
		}
	}()
	logger.G().Info("FlowLens startup initialized")
	proxySvc := proxyservice.New(settingSvc)
	pythonPluginSvc, err = pythonpluginservice.New(db, settingSvc)
	if err != nil {
		reportStartupFailure("initialize Python plugin service", err)
		return
	}
	proxySvc.SetHTTPRequestPluginRunner(pythonPluginSvc)
	memStatsSvc := memstatsservice.New()
	historySvc := historyservice.New(settingSvc, proxySvc)
	proxySvc.SetHistoryCleaner(historySvc)
	apiCollectionSvc := apicollectionservice.New(db)
	if err := apiCollectionSvc.Load(); err != nil {
		logger.G().Errorf("Load API collection failed: %v", err)
		return
	}
	loggingSvc := loggingservice.New(settingSvc)
	var toggleProxyShortcutInFlight atomic.Bool
	invokeGlobalShortcut := func(commandID string) {
		switch commandID {
		case shortcutservice.CommandShowMainWindow:
			application.InvokeSync(showMainWindow)
		case shortcutservice.CommandToggleProxy:
			if !toggleProxyShortcutInFlight.CompareAndSwap(false, true) {
				return
			}
			application.InvokeSync(func() {
				showMainWindow()
				if window := mainWindowRef.Load(); window != nil {
					window.DispatchWailsEvent(&application.CustomEvent{
						Name: shortcutInvokeEventName,
						Data: map[string]any{"commandId": commandID},
					})
				}
			})
			time.AfterFunc(globalShortcutDebounce, func() {
				toggleProxyShortcutInFlight.Store(false)
			})
		}
	}
	shortcutSvc = shortcutservice.NewWithNotifier(
		app.GlobalShortcut,
		settingSvc,
		invokeGlobalShortcut,
		func(result shortcutservice.ShortcutApplyResult) {
			app.Event.Emit(shortcutsChangedEventName, map[string]any{
				"sourceWindow": "backend",
				"config":       result.Config,
				"runtimeState": result.RuntimeState,
				"warnings":     result.Warnings,
				"applied":      result.Applied,
				"errorCode":    result.ErrorCode,
			})
		},
	)

	app.RegisterService(application.NewService(appSvc))
	app.RegisterService(application.NewService(settingSvc))
	app.RegisterService(application.NewService(shortcutSvc))
	app.RegisterService(application.NewService(loggingSvc))
	app.RegisterService(application.NewService(pythonPluginSvc))
	app.RegisterService(application.NewService(proxySvc))
	app.RegisterService(application.NewService(memStatsSvc))
	app.RegisterService(application.NewService(historySvc))
	app.RegisterService(application.NewService(apiCollectionSvc))

	var settingsWindowDirty atomic.Bool

	if isMacOS {
		appMenu := application.NewMenu()
		appMenu.AddRole(application.AppMenu)
		appMenu.AddRole(application.EditMenu)
		appMenu.AddRole(application.WindowMenu)
		app.Menu.Set(appMenu)
	}

	initialWindowConfig := currentWindowConfig(settingSvc)
	activeWindowFrameMode := windowFrameModeFromConfig(initialWindowConfig)
	settingservice.SetActiveWindowFrameMode(settingSvc, activeWindowFrameMode)
	useCustomWindowFrame := activeWindowFrameMode == settingservice.WindowFrameModeCustom
	windowOptions := application.WebviewWindowOptions{
		Name:                       "main",
		Title:                      appservice.APP_NAME,
		Width:                      appservice.DEFAULT_WINDOW_WIDTH,
		Height:                     appservice.DEFAULT_WINDOW_HEIGHT,
		MinWidth:                   appservice.MIN_WINDOW_WIDTH,
		MinHeight:                  appservice.MIN_WINDOW_HEIGHT,
		StartState:                 application.WindowStateNormal,
		Frameless:                  useCustomWindowFrame && !isMacOS,
		BackgroundColour:           application.NewRGBA(36, 36, 41, 255),
		EnableFileDrop:             true,
		BackgroundType:             application.BackgroundTypeSolid,
		DefaultContextMenuDisabled: false,
		URL:                        "/",
		UseApplicationMenu:         isMacOS,
		Mac: application.MacWindow{
			TitleBar: macTitleBarForFrameMode(useCustomWindowFrame),
			Backdrop: application.MacBackdropNormal,
		},
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: false,
		},
		Linux: application.LinuxWindow{
			Icon:                assets.AppIcon,
			WebviewGpuPolicy:    application.WebviewGpuPolicyOnDemand,
			WindowIsTranslucent: useCustomWindowFrame,
		},
	}
	applySavedWindowOptions(&windowOptions, initialWindowConfig, app.Screen.GetAll())

	mainWindow = app.Window.NewWithOptions(windowOptions)
	mainWindowRef.Store(mainWindow)
	showPendingMainWindow()

	showSettingsWindow := func() {
		if settingsWindow, ok := app.Window.GetByName(settingsWindowName); ok && settingsWindow != nil {
			if settingsWindow.IsMinimised() {
				settingsWindow.UnMinimise()
			}
			settingsWindow.Show().Focus()
			return
		}

		settingsWindowDirty.Store(false)
		settingsWindow := app.Window.NewWithOptions(settingsWindowOptions(useCustomWindowFrame, isMacOS, assets.AppIcon))
		settingsWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			settingsWindowDirty.Store(false)
		})
	}

	dispatchShutdownRequest := func() {
		if !mainFrontendReady.Load() || shutdownCoordinator == nil || !shutdownCoordinator.InProgress() {
			return
		}
		window := mainWindowRef.Load()
		if window == nil || !shutdownEventDispatched.CompareAndSwap(false, true) {
			return
		}
		window.DispatchWailsEvent(&application.CustomEvent{Name: shutdownRequestedEventName})
	}
	shutdownCoordinator = newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		uiReadyTimeout: shutdownUIReadyTimeout,
		minimumVisible: shutdownMinimumVisible,
		showLoading: func() {
			application.InvokeAsync(func() {
				if settingsWindow, ok := app.Window.GetByName(settingsWindowName); ok && settingsWindow != nil {
					settingsWindow.Hide()
				}
				requestShowMainWindow()
				dispatchShutdownRequest()
			})
		},
		prepare: func() error {
			var shutdownErrors []error
			appendShutdownError := func(step string, err error) {
				if err == nil {
					return
				}
				wrapped := fmt.Errorf("%s: %w", step, err)
				shutdownErrors = append(shutdownErrors, wrapped)
				logger.G().Errorf("Shutdown step failed: %v", wrapped)
			}

			if shortcutSvc != nil {
				appendShutdownError("release global shortcuts", shortcutSvc.Shutdown())
			}
			if settingSvc != nil {
				appendShutdownError("save settings", settingSvc.Save())
			}
			if memStatsSvc != nil {
				appendShutdownError("stop memory monitoring", memStatsSvc.Shutdown())
			}
			if historySvc != nil {
				appendShutdownError("wait for history maintenance", historySvc.Shutdown())
			}
			if proxySvc != nil {
				appendShutdownError("stop proxy services", proxySvc.Shutdown())
			}
			if pythonPluginSvc != nil {
				appendShutdownError("stop Python plugin workers", pythonPluginSvc.Shutdown())
			}

			return errors.Join(shutdownErrors...)
		},
		onPrepareError: func(err error) {
			logger.G().Errorf("Application shutdown preparation completed with errors: %v", err)
		},
		quit: app.Quit,
	})
	requestApplicationQuit = func() {
		logger.G().Info("Application quit requested")
		if shutdownCoordinator.InProgress() {
			return
		}
		if settingsWindowDirty.Load() {
			if _, ok := app.Window.GetByName(settingsWindowName); ok {
				showSettingsWindow()
				app.Event.Emit(confirmQuitRequestEventName)
				return
			}
			settingsWindowDirty.Store(false)
		}
		shutdownCoordinator.Request()
	}
	tray = app.SystemTray.New()
	if isMacOS {
		tray.SetTemplateIcon(assets.TrayTemplateIcon)
	} else {
		tray.SetIcon(assets.AppIcon)
	}
	tray.OnClick(showMainWindow).
		OnRightClick(func() {
			tray.OpenMenu()
		})
	tray.SetTooltip(appservice.APP_NAME)
	updateTrayMenu(tray, fallbackTrayLabels(currentLanguage(settingSvc)), showMainWindow, requestApplicationQuit)
	app.Event.On(openSettingsWindowEventName, func(event *application.CustomEvent) {
		showSettingsWindow()
	})
	app.Event.On(settingsWindowDirtyEventName, func(event *application.CustomEvent) {
		if event.Sender != settingsWindowName {
			return
		}
		dirty, ok := boolFromEventData(event.Data)
		if !ok {
			logger.G().Warnf("Ignore invalid settings dirty event payload: %#v", event.Data)
			return
		}
		settingsWindowDirty.Store(dirty)
	})
	app.Event.On(quitConfirmedEventName, func(event *application.CustomEvent) {
		if event.Sender != settingsWindowName {
			return
		}
		settingsWindowDirty.Store(false)
		requestApplicationQuit()
	})
	app.Event.On(shutdownUIReadyEventName, func(event *application.CustomEvent) {
		if event.Sender != "" && event.Sender != "main" {
			return
		}
		shutdownCoordinator.UIReady()
	})
	app.Event.On(trayLabelsEventName, func(event *application.CustomEvent) {
		labels, ok := parseTrayLabels(event.Data)
		if !ok {
			logger.G().Warnf("Ignore invalid tray labels event payload: %#v", event.Data)
			return
		}
		updateTrayMenu(tray, labels, showMainWindow, requestApplicationQuit)
	})

	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		details := event.Context().DropTargetDetails()
		var target string
		if details != nil && len(details.Attributes) > 0 {
			target = details.Attributes["data-file-drop-target"]
		}
		proxySvc.EmitRequestEditorFileDrop(event.Context().DroppedFiles(), target)
	})
	mainWindow.OnWindowEvent(events.Common.WindowRuntimeReady, func(event *application.WindowEvent) {
		if shouldRestoreWindowStateOnRuntimeReady(initialWindowConfig) {
			restoreSavedWindowState(initialWindowConfig, mainWindow, app.Screen.GetAll())
			windowStartupReady.Store(true)
			showPendingMainWindow()
		}
	})
	app.Event.OnMultiple("app:frontend-ready", func(event *application.CustomEvent) {
		mainFrontendReady.Store(true)
		if shouldShowWindowAfterFrontendReady(initialWindowConfig) {
			restoreSavedWindowState(initialWindowConfig, mainWindow, app.Screen.GetAll())
		}
		mainWindow.Show()
		windowStartupReady.Store(true)
		showPendingMainWindow()
		dispatchShutdownRequest()
		if result, startErr := shortcutSvc.Start(); startErr != nil {
			logger.G().Errorf("Initialise global shortcuts failed: %v", startErr)
		} else if result != nil && !result.Applied {
			logger.G().Warnf(
				"Initialise global shortcuts was not fully applied: code=%s error=%s",
				result.ErrorCode,
				result.ErrorMessage,
			)
		}
	}, 1)
	mainWindow.OnWindowEvent(events.Common.WindowDidMove, func(event *application.WindowEvent) {
		updateWindowConfigFromRuntimeIfStartupReady(settingSvc, mainWindow, &windowStartupReady)
	})
	mainWindow.OnWindowEvent(events.Common.WindowDidResize, func(event *application.WindowEvent) {
		updateWindowConfigFromRuntimeIfStartupReady(settingSvc, mainWindow, &windowStartupReady)
	})
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if shutdownCoordinator.InProgress() {
			logger.G().Info("Main window close ignored while application shutdown is in progress")
			showMainWindow()
			e.Cancel()
			return
		}
		updateWindowConfigFromRuntimeIfStartupReady(settingSvc, mainWindow, &windowStartupReady)
		if err := settingSvc.Save(); err != nil {
			logger.G().Errorf("Save settings on window close failed: %v", err)
		}
		e.Cancel()
		if currentMainWindowCloseBehavior(settingSvc) == settingservice.MainWindowCloseBehaviorQuit {
			logger.G().Info("Main window close intercepted, requesting application quit")
			requestApplicationQuit()
			return
		}
		logger.G().Info("Main window close intercepted, hiding to tray")
		mainWindow.Hide()
	})

	runErr := app.Run()

	if runErr != nil {
		logger.G().Errorf("Application run failed: %v", runErr)
	}
}
