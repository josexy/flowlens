package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/settings"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
)

// settingsWindowController opens the single Settings window.
type settingsWindowController struct {
	all      *services
	data     *store.Store
	restart  func()
	canClose func() bool
	blocked  func() bool

	window *mygo.Window
	view   *settings.View
	// asking is true while the window asks about unsaved changes.
	asking bool
}

func newSettingsWindowController(all *services, data *store.Store, restart func()) *settingsWindowController {
	return &settingsWindowController{all: all, data: data, restart: restart}
}

// dialogOptions opens picker's dialog over the Settings window.
func (s *settingsWindowController) dialogOptions(picker settings.FilePicker, multiple bool) mygo.OpenDialogOptions {
	options := openDialogOptions(picker, multiple)
	options.Parent = s.window
	return options
}

// openDialogOptions is a Settings file dialog: its title, its message where
// the platform shows one, and picker's files, named as picker names them,
// before all files.
func openDialogOptions(picker settings.FilePicker, multiple bool) mygo.OpenDialogOptions {
	options := mygo.OpenDialogOptions{Title: picker.Title, Message: picker.Message, Multiple: multiple}
	if len(picker.Patterns) > 0 {
		name := picker.FilterName
		if name == "" {
			name = locales.SettingsCertificateFileFilter()
		}
		options.Filters = []mygo.FileFilter{{Name: name, Extensions: picker.Patterns}, {Name: locales.SettingsAllFilesFilter(), Extensions: []string{"*"}}}
	}
	return options
}

func (s *settingsWindowController) host() settings.Host {
	all := s.all
	return settings.Host{
		ListFonts: func() []settingservice.FontOption {
			// Use the same family names as the renderer. Registry font-file
			// labels such as "SimSun & NSimSun" are not usable family names.
			// MyGo allows this enumeration from the settings background job.
			families := ui.FontFamilies()
			options := make([]settingservice.FontOption, len(families))
			for index, family := range families {
				options[index] = settingservice.FontOption{Label: family.Name, Value: family.Name}
			}
			return options
		},
		CAInfo:  all.setting.GetCACertificateInfo,
		CATrust: all.setting.GetCurrentCACertificateTrustStatus,
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
		PickFile: func(picker settings.FilePicker) (string, error) {
			paths, err := mygo.Dialog.Open(s.dialogOptions(picker, false))
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		},
		PickFiles: func(picker settings.FilePicker) ([]string, error) {
			return mygo.Dialog.Open(s.dialogOptions(picker, true))
		},
		RestartProxy: s.restart,
		LogStatus:    all.logging.GetLogStatus,
		SetLog:       all.logging.SetLogEnabled,
		SetLevel:     all.logging.SetLogLevel,
		ClearLogs:    all.logging.ClearLogs,
		OpenLogDir:   all.logging.OpenLogDir,
		DataSize:     func() (proxyservice.LocalDataSize, error) { return all.proxy.GetLocalDataSize() },
		PythonStatus: all.python.GetRuntimeStatus,
		TestPython:   all.python.TestInterpreter,
		FindPython:   all.python.DiscoverInterpreters,
		Close: func() {
			if s.window != nil {
				s.window.Destroy()
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
		s.window, s.view, s.asking = nil, nil, false
	})
	window.Show()
	window.Focus()
}

// quitGuard asks before unsaved settings are lost to a quit or update. It
// runs on the main thread and reads the window's state directly.
func (s *settingsWindowController) quitGuard(reason string, proceed func()) bool {
	if s.view == nil || !s.view.Dirty() {
		return false
	}
	if s.asking {
		return true
	}
	s.asking = true
	if s.window != nil {
		s.window.Show()
		s.window.Focus()
	}
	s.view.AskDiscardBeforeQuit(reason == quitReasonUpdate, func() {
		s.asking = false
		proceed()
	}, func() { s.asking = false })
	return true
}
