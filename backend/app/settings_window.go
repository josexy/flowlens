package app

import (
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/settings"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
)

// settingsWindowController opens the single Settings window.
type settingsWindowController struct {
	all     *services
	data    *store.Store
	restart func()
	// exit quits once a reset is waiting for the next launch.
	exit     func()
	canClose func() bool
	blocked  func() bool

	window *mygo.Window
	view   *settings.View
	dirty  atomic.Bool
	asking atomic.Bool
}

func newSettingsWindowController(all *services, data *store.Store, restart func()) *settingsWindowController {
	return &settingsWindowController{all: all, data: data, restart: restart}
}

func (s *settingsWindowController) host() settings.Host {
	all := s.all
	filters := func(patterns []string) []mygo.FileFilter {
		if len(patterns) == 0 {
			return nil
		}
		return []mygo.FileFilter{{Name: locales.SettingsCertificateFileFilter(), Extensions: patterns}, {Name: locales.SettingsAllFilesFilter(), Extensions: []string{"*"}}}
	}
	return settings.Host{
		ListFonts: all.setting.ListSystemFonts,
		CAInfo:    all.setting.GetCACertificateInfo,
		CATrust:   all.setting.GetCurrentCACertificateTrustStatus,
		GenerateCA: func(overwrite bool) error {
			_, err := all.setting.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{Overwrite: overwrite})
			return err
		},
		InstallCA: func(fingerprint string) error {
			_, err := all.setting.InstallCurrentCACertificate(fingerprint)
			return err
		},
		UninstallCA: func(fingerprint string) error {
			_, err := all.setting.UninstallCurrentCACertificate(fingerprint)
			return err
		},
		PickFile: func(title string, patterns ...string) (string, error) {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: s.window, Title: title, Filters: filters(patterns)})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		PickFiles: func(title string, patterns ...string) ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: s.window, Title: title, Multiple: true, Filters: filters(patterns)})
		},
		RestartProxy: s.restart,
		LogStatus:    all.logging.GetLogStatus,
		SetLog:       all.logging.SetLogEnabled,
		SetLevel:     all.logging.SetLogLevel,
		ClearLogs:    all.logging.ClearLogs,
		OpenLogDir:   all.logging.OpenLogDir,
		DataSize:     func() (proxyservice.LocalDataSize, error) { return all.proxy.GetLocalDataSize() },
		ClearCache:   all.proxy.ClearCacheFiles,
		ClearAllData: all.proxy.ClearCacheAndHistory,
		ResetApp:     all.app.ResetApplication,
		PythonStatus: all.python.GetRuntimeStatus,
		TestPython:   all.python.TestInterpreter,
		FindPython:   all.python.DiscoverInterpreters,
		Close: func() {
			if s.window != nil {
				s.window.Destroy()
			}
		},
		DirtyChanged: s.dirty.Store,
		ExitAfterReset: func() {
			if s.exit != nil {
				s.exit()
			}
		},
	}
}

func (s *settingsWindowController) open() {
	if s.blocked != nil && s.blocked() {
		return
	}
	if s.window != nil {
		s.window.Show()
		s.window.Focus()
		return
	}
	dispatcher := shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig {
		if s.data.Settings.Current == nil {
			return nil
		}
		return s.data.Settings.Current.Shortcuts
	})
	options := mygo.WindowOptions{
		Title: locales.MenuSettings(), Width: settingsWindowDefaultWidth, Height: settingsWindowDefaultHeight,
		MinWidth: settingsWindowMinWidth, MinHeight: settingsWindowMinHeight,
	}
	host := s.host()
	host.Frame = secondaryFrame(&options, s.data.Settings.UsesCustomFrame(), locales.MenuSettings(), func() *mygo.Window { return s.window })
	s.view = settings.New(s.data, host, dispatcher, &widgets.Notifier{}, &widgets.ConfirmDialog{})
	s.view.Begin()
	options.Content = ui.View(s.view.Build)
	window := mygo.NewWindow(options)
	s.window = window
	s.data.Runtime.Dispatcher.Register(settingsWindowName, window)
	window.OnClose(func(event *mygo.CloseEvent) {
		if !s.canClose() && !s.view.CloseRequested() {
			event.PreventDefault()
		}
	})
	window.OnClosed(func() {
		s.data.Runtime.Dispatcher.Unregister(settingsWindowName)
		s.dirty.Store(false)
		s.window, s.view = nil, nil
	})
	window.Show()
	window.Focus()
}

// quitGuard asks before unsaved settings are lost to a quit or update.
func (s *settingsWindowController) quitGuard(reason string, proceed func()) bool {
	if !s.dirty.Load() {
		return false
	}
	if !s.asking.CompareAndSwap(false, true) {
		return true
	}
	s.data.Runtime.Dispatcher.Post(func() {
		if s.view == nil {
			s.asking.Store(false)
			proceed()
			return
		}
		if s.window != nil {
			s.window.Show()
			s.window.Focus()
		}
		s.view.AskDiscardBeforeQuit(reason == quitReasonUpdate, func() {
			s.asking.Store(false)
			s.dirty.Store(false)
			proceed()
		}, func() { s.asking.Store(false) })
	})
	return true
}
