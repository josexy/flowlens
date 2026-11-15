// Package app assembles FlowLens: it starts the services and opens the windows.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/capture"
	"github.com/josexy/flowlens/internal/desktop/collection"
	"github.com/josexy/flowlens/internal/desktop/editor"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/locale"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/memstats"
	"github.com/josexy/flowlens/internal/desktop/plugins"
	"github.com/josexy/flowlens/internal/desktop/pythonapi"
	"github.com/josexy/flowlens/internal/desktop/rewrite"
	"github.com/josexy/flowlens/internal/desktop/settings"
	"github.com/josexy/flowlens/internal/desktop/shell"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appdatabase "github.com/josexy/flowlens/internal/pkg/database"
	"github.com/josexy/flowlens/internal/pkg/logger"
	"github.com/josexy/flowlens/internal/serviceruntime"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	loggingservice "github.com/josexy/flowlens/internal/services/logging_service"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const (
	globalShortcutDebounce    = 250 * time.Millisecond
	shutdownMinimumVisible    = 300 * time.Millisecond
	shutdownUIReadyTimeout    = 2 * time.Second
	shortcutsChangedEventName = "app:shortcuts-changed"
)

// startupSteps are the stages that must happen in this order: a second instance
// has to exit before it can touch the database.
type startupSteps struct {
	enableBootstrapLogging func()
	acquireInstanceLock    func() bool
	prepareReset           func() (*resetManifest, *resetStartupClient, error)
	openDatabase           func() (*sql.DB, error)
	launch                 func(*sql.DB, *resetManifest, *resetStartupClient)
	reportFailure          func(stage string, err error)
}

func (s startupSteps) run() {
	s.enableBootstrapLogging()
	if !s.acquireInstanceLock() {
		return
	}
	reset, progress, err := s.prepareReset()
	if progress != nil {
		defer func() { _ = progress.Close() }()
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			// Without the progress window, keep the reset marker and exit
			// before opening SQLite or any service.
			s.reportFailure("restore initial state", err)
		}
		return
	}
	db, err := s.openDatabase()
	if err != nil {
		s.reportFailure("open application database", err)
		return
	}
	s.launch(db, reset, progress)
}

// Run starts the application and returns when it exits.
func Run() {
	if os.Getenv(resetStartupEnvironment) != "" {
		runResetStartupWindow()
		return
	}
	mygo.App.SetName(applicationName())
	startupSteps{
		enableBootstrapLogging: func() { logger.EnableBootstrapOutput(os.Stderr) },
		acquireInstanceLock:    mygo.App.RequestSingleInstanceLock,
		prepareReset:           prepareResetStartup,
		openDatabase:           appdatabase.Open,
		launch:                 launch,
		reportFailure:          reportStartupFailure,
	}.run()
}

type services struct {
	bus           *serviceruntime.Bus
	app           *appservice.AppService
	setting       *settingservice.SettingService
	rewrite       *rewriteservice.RewriteService
	proxy         *proxyservice.ProxyService
	python        *pythonpluginservice.PythonPluginService
	memStats      *memstatsservice.MemStatsService
	history       *historyservice.HistoryService
	apiCollection *apicollectionservice.APICollectionService
	logging       *loggingservice.LoggingService
	shortcut      *shortcutservice.ShortcutService
	lifecycle     *serviceruntime.Lifecycle
}

func newServices(db *sql.DB, invokeShortcut func(string), shortcutsChanged func(shortcutservice.ShortcutApplyResult)) (*services, error) {
	bus := serviceruntime.NewBus()
	appSvc := appservice.New()
	appSvc.SetEventBus(bus)
	configureUpdates(appSvc)
	settingSvc := settingservice.New(db)
	if err := settingSvc.Load(); err != nil {
		return nil, fmt.Errorf("load application settings: %w", err)
	}
	configureLogging(settingSvc)
	logger.G().Info("FlowLens startup initialized")

	rewriteSvc := rewriteservice.New(db)
	rewriteservice.SetChangedHandler(rewriteSvc, func(event rewriteservice.Changed) {
		bus.Emit(rewriteservice.ChangedEvent, event)
	})
	proxySvc := proxyservice.New(settingSvc)
	proxySvc.SetEventBus(bus)
	proxyservice.SetRewriteService(proxySvc, rewriteSvc)
	pythonSvc, err := pythonpluginservice.New(db, settingSvc)
	if err != nil {
		return nil, fmt.Errorf("initialize Python plugin service: %w", err)
	}
	pythonSvc.SetEventBus(bus)
	proxySvc.SetHTTPRequestPluginRunner(pythonSvc)
	memStatsSvc := memstatsservice.New()
	memStatsSvc.SetEventBus(bus)
	historySvc := historyservice.New(settingSvc, proxySvc)
	proxySvc.SetHistoryCleaner(historySvc)
	apiCollectionSvc := apicollectionservice.New(db)
	if err := apiCollectionSvc.Load(); err != nil {
		return nil, fmt.Errorf("load API collection: %w", err)
	}
	shortcutSvc := shortcutservice.NewWithNotifier(newSystemShortcuts(), settingSvc, invokeShortcut, func(result shortcutservice.ShortcutApplyResult) {
		bus.Emit(shortcutsChangedEventName, result)
		shortcutsChanged(result)
	})
	loggingSvc := loggingservice.New(settingSvc)

	lifecycle := serviceruntime.NewLifecycle(context.Background())
	lifecycle.Register(appSvc, settingSvc, shortcutSvc, loggingSvc, pythonSvc, proxySvc, memStatsSvc, historySvc, apiCollectionSvc)
	// The rule reader starts after synchronous database consumers: SQLite's
	// single connection must not make them wait for rule compilation.
	lifecycle.Register(rewriteSvc)
	return &services{
		bus: bus, app: appSvc, setting: settingSvc, rewrite: rewriteSvc, proxy: proxySvc, python: pythonSvc,
		memStats: memStatsSvc, history: historySvc, apiCollection: apiCollectionSvc, logging: loggingSvc,
		shortcut: shortcutSvc, lifecycle: lifecycle,
	}, nil
}

func configureLogging(settingSvc *settingservice.SettingService) {
	config, err := settingservice.GetLogConfig(settingSvc)
	if err != nil {
		config = settingservice.LogConfig{Enabled: true, Level: string(logger.DefaultLogLevel())}
	}
	if _, err := logger.Configure(logger.Config{
		Enabled:      config.Enabled,
		Level:        logger.NormalizeLogLevel(config.Level),
		LogDir:       logger.DefaultLogDir(),
		MaxSizeBytes: logger.DefaultLogMaxSizeBytes,
		MaxBackups:   logger.DefaultLogMaxBackups,
		Console:      logger.DebugMode(),
		DebugMode:    logger.DebugMode(),
	}); err != nil {
		logger.G().Errorf("Configure logger failed: %v", err)
	}
}

func launch(db *sql.DB, startupReset *resetManifest, startupProgress *resetStartupClient) {
	defer func() {
		if err := db.Close(); err != nil {
			logger.G().Warnf("Close application database failed: %v", err)
		}
	}()
	defer logger.Close()

	var showMain atomic.Pointer[func()]
	var application *shell.App
	invoker := &shortcutInvoker{
		onMain: mygo.RunOnMain,
		show: func() {
			if show := showMain.Load(); show != nil {
				(*show)()
			}
		},
		toggleProxy:   func() { application.Dispatcher.Invoke(shortcuts.CaptureToggleProxy) },
		debounce:      globalShortcutDebounce,
		afterDebounce: func(delay time.Duration, run func()) { time.AfterFunc(delay, run) },
	}
	invokeShortcut := invoker.invoke
	rt := store.NewRuntime()
	var data *store.Store
	all, err := newServices(db, invokeShortcut, func(result shortcutservice.ShortcutApplyResult) {
		rt.Dispatcher.Post(func() { data.Settings.ShortcutsChanged(result) })
	})
	if err != nil {
		reportStartupFailure("start services", err)
		return
	}
	data = store.New(rt, store.Dependencies{
		Settings: store.SettingsDependencies{
			Backend: all.setting, Shortcuts: all.shortcut, Proxy: all.proxy, Python: all.python, ApplyLocale: locale.Apply,
			ApplyThemeMode: applyNativeThemeMode,
		},
		Traffic:    all.proxy,
		Proxy:      all.proxy,
		History:    all.history,
		Requests:   all.proxy,
		Saved:      all.apiCollection,
		Collection: all.apiCollection,
		Rewrite:    all.rewrite,
		Plugins:    all.python,
		Updater:    all.app,
		MemStats:   all.memStats,
		PluginEvents: func(registry func(store.PluginRegistryEvent), status func(store.PluginStatusEvent)) (off func()) {
			offRegistry := all.bus.On(pythonpluginservice.RegistryEventName, func(event serviceruntime.Event) {
				if value, ok := event.Data.(store.PluginRegistryEvent); ok {
					rt.Dispatcher.Post(func() { registry(value) })
				}
			})
			offStatus := all.bus.On(pythonpluginservice.StatusEventName, func(event serviceruntime.Event) {
				if value, ok := event.Data.(store.PluginStatusEvent); ok {
					rt.Dispatcher.Post(func() { status(value) })
				}
			})
			return func() { offRegistry(); offStatus() }
		},
		RewriteChanges: func(notify func(int64)) (off func()) {
			return all.bus.On(rewriteservice.ChangedEvent, func(event serviceruntime.Event) {
				if changed, ok := event.Data.(rewriteservice.Changed); ok {
					rt.Dispatcher.Post(func() { notify(changed.Revision) })
				}
			})
		},
	})
	if err := data.Settings.Load(); err != nil {
		reportStartupFailure("load application settings", err)
		return
	}
	initialWindowConfig := currentWindowConfig(all.setting)
	settingservice.SetActiveWindowFrameMode(all.setting, windowFrameModeFromConfig(initialWindowConfig))
	data.Settings.ActiveFrameMode = all.setting.GetActiveWindowFrameMode()
	if err := all.lifecycle.Start(); err != nil {
		reportStartupFailure("start services", err)
		return
	}
	logger.SetDirectoryOpener(mygo.Shell.OpenPath)

	mygo.App.WhenReady(func() {
		openMainWindow(all, data, initialWindowConfig, startupReset, startupProgress, &showMain, &application)
	})
	if err := mygo.App.Run(); err != nil {
		logger.G().Errorf("Application run failed: %v", err)
	}
	logger.G().Info("Application shutdown started")
	if err := all.lifecycle.Stop(); err != nil {
		logger.G().Warnf("Stop services: %v", err)
	}
	rt.Close()
	logger.G().Info("Application shutdown completed")
}

func openMainWindow(
	all *services,
	data *store.Store,
	windowConfig *settingservice.WindowConfig,
	startupReset *resetManifest,
	startupProgress *resetStartupClient,
	showMain *atomic.Pointer[func()],
	application **shell.App,
) {
	locale.Apply(data.Settings.Language())
	if err := theme.RegisterFonts(); err != nil {
		logger.G().Warnf("Register fonts: %v", err)
	}

	useCustomFrame := data.Settings.UsesCustomFrame()
	macOS := runtime.GOOS == "darwin"
	displays := screenRects()
	placement := mainWindowPlacement(windowConfig, displays)

	var window *mygo.Window
	app := shell.New(data, shell.Host{
		MinimizeWindow: func() { window.Minimize() },
		ToggleMaximizeWindow: func() {
			if window.IsMaximized() {
				window.Unmaximize()
			} else {
				window.Maximize()
			}
		},
		IsWindowMaximized: func() bool { return window.IsMaximized() },
		CloseWindow:       func() { window.Close() },
		OpenURL:           mygo.Shell.OpenExternal,
		Environment:       all.app.GetEnvironmentInfo,
	})
	*application = app
	icons := widgets.NewProcessIcons(data.Runtime.Jobs, all.proxy.GetProcessIcon)
	captureWorkspace := capture.New(data, capture.Host{
		ListLocalIPs: all.setting.ListLocalIPv4Addresses,
		Resend:       all.proxy.ResendRequest,
		Export: &capture.ExportHost{
			Generation: all.proxy.GetTrafficExportGeneration, ExportLive: all.proxy.ExportTraffic, ExportHistory: all.history.ExportTraffic,
			ExportLiveHAR: all.proxy.ExportHAR, ExportHistoryHAR: all.history.ExportHAR,
			PickSavePath: func(filename, filterName, extension string) (string, error) {
				return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, DefaultPath: filename, Filters: []mygo.FileFilter{{Name: filterName, Extensions: []string{extension}}}})
			},
			PickDirectory: func(title string) (string, error) {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: title, Directory: true})
				if err != nil || len(paths) == 0 {
					return "", err
				}
				return paths[0], nil
			},
		},
		OpenEditor: func(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView, focus bool, historyKey string, done func(error)) {
			previous := data.Workspace.ActiveKey
			source := logic.DraftCaptureEdit
			if historyKey != "" {
				source = logic.DraftHistoryEdit
			}
			data.Workspace.OpenFromTraffic(entry, body, source, historyKey, func(_ string, err error) {
				if !focus {
					data.Workspace.Activate(previous)
				}
				done(err)
			})
		},
		PickHARFiles: func() ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Multiple: true, Filters: []mygo.FileFilter{{Name: locales.HarImportFileFilter(), Extensions: []string{"har"}}}})
		},
	}, app.Dispatcher, &app.Notifier, &app.Confirm, icons)
	detail := inspect.NewPanel(inspect.LiveData{Traffic: data.Traffic}, inspect.Dependencies{
		Runtime: data.Runtime, Settings: data.Settings, Notifier: &app.Notifier, Icons: icons,
		PickSavePath: func(filename string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, DefaultPath: filename})
		},
		SaveBody: all.proxy.SaveBodyToFile,
	})
	detail.OnClose = captureWorkspace.Table.CloseDetails
	captureWorkspace.Detail = detail.Build
	historyDetail := inspect.NewPanel(store.HistoryDetailData{History: data.History}, inspect.Dependencies{
		Runtime: data.Runtime, Settings: data.Settings, Notifier: &app.Notifier, Icons: icons,
		PickSavePath: func(filename string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, DefaultPath: filename})
		},
		SaveBody: all.proxy.SaveBodyToFile,
	})
	historyDetail.OnClose = captureWorkspace.HistoryTable.CloseDetails
	captureWorkspace.HistoryDetail = historyDetail.Build
	collections := collection.New(data, &app.Notifier, &app.Confirm)
	requestEditor := editor.New(data, editor.Host{
		SaveTab:        collections.SaveTab,
		ShowPythonHelp: func() { data.Workbench.OpenPythonHelp(pythonapi.ScopeInline) },
		Dialogs:        collections.Dialogs,
		ResolveFile: func(path string) (*logic.RequestFileValue, error) {
			file, err := all.proxy.ResolveRequestDraftFile(path)
			if err != nil || file == nil {
				return nil, err
			}
			return &logic.RequestFileValue{Path: file.Path, Name: file.Name, Size: file.Size}, nil
		},
		PickFile: func() (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		PythonEnabled: func() bool {
			current := data.Settings.Current
			return current != nil && current.PythonPluginConfig != nil && current.PythonPluginConfig.Enabled
		},
		Inspect: inspect.Dependencies{
			Runtime: data.Runtime, Settings: data.Settings, Notifier: &app.Notifier, Icons: icons,
			PickSavePath: func(filename string) (string, error) {
				return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, DefaultPath: filename})
			},
			SaveBody: all.proxy.SaveBodyToFile,
		},
	}, app.Dispatcher, &app.Notifier, &app.Confirm)
	captureWorkspace.TabBar = requestEditor.TabBar
	captureWorkspace.RequestTab = requestEditor.Build
	rules := rewrite.New(data, rewrite.Host{
		Preview: all.rewrite.Preview, PreviewBody: all.rewrite.PreviewBody,
		Kbds:   captureWorkspace.Kbds,
		Select: func() { data.Workbench.ShowSection(store.WorkspaceRewriteRules) },
	}, app.Dispatcher, &app.Notifier)
	helpPanel := plugins.NewHelp(data.Workbench, &app.Notifier, func() string { return locale.Tag(data.Settings.Language()) })
	helpPanel.Context = func() string {
		return string(data.Workbench.ActiveSection) + "|" + string(data.Workbench.ActiveContent) + "|" + data.Workspace.ActiveKey
	}
	app.SidePanel = helpPanel
	pluginsView := plugins.New(data, app.Dispatcher, &app.Notifier)
	pluginsView.Kbds = captureWorkspace.Kbds
	pluginsView.OpenHelp = data.Workbench.OpenPythonHelp
	pluginsView.Select = func() { data.Workbench.ShowSection(store.WorkspacePythonPlugins) }
	app.Surfaces[store.WorkspacePythonPlugins] = shell.Surface{Sidebar: pluginsView.Sidebar, Content: pluginsView.Content}
	app.Surfaces[store.WorkspaceRewriteRules] = shell.Surface{Sidebar: rules.Sidebar, Content: rules.Content}
	statistics := memstats.New(data.MemStats)
	statistics.Select = func() { data.Workbench.ShowSection(store.WorkspaceMemStats) }
	app.Surfaces[store.WorkspaceMemStats] = shell.Surface{Sidebar: statistics.Sidebar, Content: statistics.Content}
	quitGuard := &rewriteQuitGuard{}
	app.Overlays = append(app.Overlays, func(c *ui.Context, theme nui.Theme) {
		quitGuard.setDirty(rules.Dirty())
		rules.Overlay(c, theme)
	})
	var lifecycle *appLifecycle
	settingsWindow := newSettingsWindowController(all, data, func() { captureWorkspace.Restart() })
	updateWindow := &updateWindowController{data: data}
	app.Host.OpenSettings = settingsWindow.open
	app.Host.OpenUpdate = updateWindow.open
	exitAfterReset := func() { go lifecycle.RequestQuit() }
	settingsWindow.exit = exitAfterReset
	app.Overlays = append(app.Overlays, settings.ResetOverlay(data, app.Dispatcher, exitAfterReset))
	app.Surfaces[store.WorkspaceAPICollection] = shell.Surface{Sidebar: collections.Sidebar, Content: captureWorkspace.Content}
	app.Surfaces[store.WorkspaceCategory] = shell.Surface{Sidebar: captureWorkspace.CategorySidebar, Content: captureWorkspace.Content}
	data.History.LoadList()
	app.Surfaces[store.WorkspaceCapture] = shell.Surface{Sidebar: captureWorkspace.Sidebar, Content: captureWorkspace.Content}
	app.Counts = func() (int, int) { return activeTrafficCounts(data) }

	options := mygo.WindowOptions{
		Title:     appservice.APP_NAME,
		Width:     placement.Width,
		Height:    placement.Height,
		MinWidth:  appservice.MIN_WINDOW_WIDTH,
		MinHeight: appservice.MIN_WINDOW_HEIGHT,
		Hidden:    true,
		Content:   ui.View(app.View),
	}
	if placement.Positioned {
		options.X, options.Y = placement.X, placement.Y
	}
	if useCustomFrame {
		options.TitleBarStyle = mygo.TitleBarHidden
		options.HideWindowButtons = !macOS
	}
	window = mygo.NewWindow(options)
	data.Runtime.Dispatcher.SetMain(window)
	data.Events.Connect(all.bus, data.Runtime.Dispatcher.Post)
	data.Workspace.ConnectPluginLogs(all.bus, data.Runtime.Dispatcher.Post)
	data.Proxy.Initialize(all.bus)
	data.Traffic.Initialize(func(err error) {
		if err != nil {
			logger.G().Errorf("Load captured traffic: %v", err)
		}
	})

	var ready atomic.Bool
	show := func() {
		if window.IsMinimized() {
			window.Restore()
		}
		window.Show()
		window.Focus()
	}
	showMain.Store(&show)
	mygo.App.OnSecondInstance(func([]string, string) {
		logger.G().Info("Second FlowLens instance launch requested, showing main window")
		mygo.RunOnMain(show)
	})

	lifecycle = &appLifecycle{
		window:   window,
		settings: all.setting,
		saveGeom: func() { updateWindowConfigFromRuntimeIfStartupReady(all.setting, window, &ready) },
	}
	lifecycle.coordinator = newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		uiReadyTimeout: shutdownUIReadyTimeout,
		minimumVisible: shutdownMinimumVisible,
		showLoading: func() {
			data.Runtime.Dispatcher.Post(func() {
				data.ShuttingDown = true
				show()
			})
		},
		prepare:        func() error { return prepareShutdown(all, lifecycle) },
		onPrepareError: func(err error) { logger.G().Errorf("Application shutdown preparation completed with errors: %v", err) },
		quit: func() {
			if lifecycle.relaunch.Load() {
				mygo.App.Relaunch()
				return
			}
			mygo.App.Quit()
		},
	})
	busy := func() bool { return lifecycle.resetting.Load() || lifecycle.coordinator.InProgress() }
	settingsWindow.blocked, updateWindow.blocked = busy, busy
	settingsWindow.canClose, updateWindow.canClose = lifecycle.coordinator.CanQuit, lifecycle.coordinator.CanQuit
	restartForUpdate := &updateRestartHandler{
		blocked: func(proceed func()) bool { return lifecycle.blockedByGuards(quitReasonUpdate, proceed) },
		restart: lifecycle.restartForUpdate,
		failed:  func(err error) { all.bus.Emit(appservice.UpdateRestartFailedEventName, err.Error()) },
	}
	all.bus.On(appservice.UpdateUserRestartEventName, func(serviceruntime.Event) { go restartForUpdate.request() })
	appservice.SetResetApplicationHandler(all.app, (&resetRequest{
		lifecycle:     lifecycle,
		language:      func() string { return currentLanguage(all.setting) },
		begin:         func() (resetGuard, error) { return settingservice.BeginApplicationReset(all.setting) },
		cancelUpdates: func() { appservice.CancelUpdateForReset(all.app) },
		saveManifest:  saveResetManifest,
		clearMarker:   clearResetMarker,
		committed: func() {
			settingsWindow.dirty.Store(false)
			ready.Store(false)
		},
	}).run)
	data.Updater.Initialize(all.bus, data.Runtime.Dispatcher.Post)
	data.MemStats.Initialize(all.bus, data.Runtime.Dispatcher.Post)
	app.Host.ShutdownUIReady = func() { lifecycle.coordinator.UIReady() }
	lifecycle.guards = append(lifecycle.guards, settingsWindow.quitGuard)
	lifecycle.guards = append(lifecycle.guards, func(reason string, proceed func()) bool {
		blocked, request := quitGuard.request(reason)
		if request != nil {
			data.Runtime.Dispatcher.Post(func() {
				show()
				rules.AskQuit(rewrite.QuitPrompt{Reason: request.Reason, Respond: func(decision string) {
					if _, confirmed := quitGuard.confirm(request.RequestID, request.Reason, decision); confirmed {
						proceed()
					}
				}})
			})
		}
		return blocked
	})

	mygo.App.OnBeforeQuit(func(event *mygo.QuitEvent) {
		if lifecycle.BeforeQuit() {
			event.PreventDefault()
		}
	})
	window.OnClose(func(event *mygo.CloseEvent) {
		if lifecycle.MainWindowClosing() {
			event.PreventDefault()
		}
	})
	window.OnMove(func() { updateWindowConfigFromRuntimeIfStartupReady(all.setting, window, &ready) })
	window.OnResize(func() { updateWindowConfigFromRuntimeIfStartupReady(all.setting, window, &ready) })

	tray, trayErr := newAppTray(show, lifecycle.RequestQuit)
	lifecycle.trayAvailable.Store(trayErr == nil)
	if trayErr != nil {
		logger.G().Warnf("System tray unavailable: %v", trayErr)
	} else {
		mygo.App.OnQuit(tray.destroy)
	}

	window.OnReadyToShow(func() {
		restoreSavedWindowState(windowConfig, window, screenRects())
		show()
		if startupProgress != nil {
			if err := startupProgress.Close(); err != nil {
				logger.G().Warnf("Close reset startup window: %v", err)
			}
		}
		ready.Store(true)
		if startupReset != nil && startupReset.Phase == resetCleaned {
			if err := clearResetMarker(); err != nil {
				logger.G().Warnf("Remove completed reset marker: %v", err)
			}
			app.Notifier.WithTimeout(5 * time.Second).Success(locales.SettingsResetApplicationSuccess())
		}
		go startGlobalShortcuts(all.shortcut)
	})
}

// activeTrafficCounts counts the traffic of the active workspace tab: a saved
// session's, or the live capture's.
func activeTrafficCounts(data *store.Store) (total, selected int) {
	history := data.History
	fromHistory := false
	if data.Workspace != nil && history != nil {
		tab := data.Workspace.ActiveTab()
		switch {
		case tab.Type == logic.TabTypeHistory:
			fromHistory = true
		case tab.HTTP != nil:
			fromHistory = tab.HTTP.Source == logic.DraftHistoryEdit
		case tab.WebSocket != nil:
			fromHistory = tab.WebSocket.Source == logic.DraftHistoryEdit
		}
	}
	if !fromHistory {
		return int(data.Traffic.Statistics.Total), data.Traffic.SelectedCount
	}
	traffic := history.Traffic
	if history.LoadingHistory || history.SelectedKey != traffic.Key {
		return len(traffic.Entries), 0
	}
	return len(traffic.Entries), traffic.SelectedCount
}

func startGlobalShortcuts(shortcut *shortcutservice.ShortcutService) {
	result, err := shortcut.Start()
	switch {
	case err != nil:
		logger.G().Errorf("Initialise global shortcuts failed: %v", err)
	case result != nil && !result.Applied:
		logger.G().Warnf("Initialise global shortcuts was not fully applied: code=%s error=%s", result.ErrorCode, result.ErrorMessage)
	}
}

func screenRects() []windowRect {
	displays := mygo.Screen.Displays()
	rects := make([]windowRect, len(displays))
	for index, display := range displays {
		area := display.WorkArea
		rects[index] = windowRect{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height}
	}
	return rects
}

// prepareShutdown stops background work before the process exits, in the order
// the services depend on each other.
func prepareShutdown(all *services, lifecycle *appLifecycle) error {
	var failures []error
	note := func(step string, err error) {
		if err != nil {
			wrapped := fmt.Errorf("%s: %w", step, err)
			failures = append(failures, wrapped)
			logger.G().Errorf("Shutdown step failed: %v", wrapped)
		}
	}
	all.app.ShutdownUpdater()
	note("stop rewrite rule loading", all.rewrite.Shutdown())
	note("release global shortcuts", all.shortcut.Shutdown())
	if !lifecycle.resetting.Load() {
		note("save settings", all.setting.Save())
	}
	note("stop memory monitoring", all.memStats.Shutdown())
	note("wait for history maintenance", all.history.Shutdown())
	note("stop proxy services", all.proxy.Shutdown())
	note("stop Python plugin workers", all.python.Shutdown())
	return errors.Join(failures...)
}
