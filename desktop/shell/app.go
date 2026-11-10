// Package shell builds the main window: its title bar, sidebars, content area,
// and status bar.
package shell

import (
	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

// Host is what the shell needs from the desktop environment.
type Host struct {
	MinimizeWindow       func()
	ToggleMaximizeWindow func()
	IsWindowMaximized    func() bool
	CloseWindow          func()
	OpenURL              func(url string) error
	OpenSettings         func()
	OpenUpdate           func()
	Environment          func() appservice.EnvironmentInfo
	// ShutdownUIReady reports that the shutdown indicator has been drawn.
	ShutdownUIReady func()
}

// Surface is the sidebar and content of a workbench section.
type Surface struct {
	Sidebar func(c *ui.Context, theme nui.Theme)
	Content func(c *ui.Context, theme nui.Theme)
}

// SidePanel is a panel docked beside the content on wide windows and laid
// over it on narrow ones.
type SidePanel interface {
	Visible() bool
	BuildDocked(c *ui.Context, theme nui.Theme)
	BuildOverlay(c *ui.Context, theme nui.Theme)
	// Forget notes that the panel is closed.
	Forget()
}

// sidePanelDockWidth is the narrowest workspace that docks the side panel.
const sidePanelDockWidth float32 = 1100

// TrafficCounts supplies the status bar's numbers.
type TrafficCounts func() (total, selected int)

// App is the main window's interface.
type App struct {
	Store      *store.Store
	Host       Host
	Dispatcher *shortcuts.Dispatcher
	Notifier   widgets.Notifier
	Confirm    widgets.ConfirmDialog
	Surfaces   map[store.WorkspaceSection]Surface
	// Overlays are built every frame, whichever section is on show.
	Overlays []func(c *ui.Context, theme nui.Theme)
	// SidePanel, when set, shows beside the content.
	SidePanel      SidePanel
	workspaceWidth float32
	Counts         TrafficCounts
	// MacOS selects the platform's title bar layout.
	MacOS bool

	environment environmentDialog
	shutdownUI  bool
}

// New returns the main window's interface.
func New(data *store.Store, host Host) *App {
	app := &App{
		Store:    data,
		Host:     host,
		Surfaces: map[store.WorkspaceSection]Surface{},
		MacOS:    shortcuts.IsMacOS(),
	}
	app.Dispatcher = shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig {
		if data.Settings.Current == nil {
			return nil
		}
		return data.Settings.Current.Shortcuts
	})
	return app
}

// View builds the main window.
func (a *App) View(c *ui.Context) {
	theme := nui.App(c, a.Store.Settings.Theme().Config)
	a.Dispatcher.Begin()
	a.declareShortcuts()
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		a.titleBar(c, theme)
		ui.Row(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			a.primarySidebar(c, theme)
			a.workspace(c, theme)
		})
		a.statusBar(c, theme)
	})
	a.Notifier.Show(c, theme)
	nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 3})
	if a.Confirm.Open() {
		a.Dispatcher.NoteModal()
	}
	a.Confirm.Build(c, theme)
	a.environmentModal(c, theme)
	if a.SidePanel != nil && a.SidePanel.Visible() && !a.sidePanelDocked() {
		a.SidePanel.BuildOverlay(c, theme)
	}
	for _, overlay := range a.Overlays {
		overlay(c, theme)
	}
	a.shutdownOverlay(c, theme)
	a.Dispatcher.Install(c)
}

func (a *App) workspace(c *ui.Context, theme nui.Theme) {
	row := ui.Row(c.Key("workspace-row")).Grow(1).Basis(0).MinWidth(0).FillHeight()
	if bounds := row.Bounds(); bounds.W > 0 {
		a.workspaceWidth = bounds.W
	}
	row.Children(func() {
		a.workspaceMain(c, theme)
		if a.sidePanelDocked() {
			a.SidePanel.BuildDocked(c, theme)
		}
	})
	if a.SidePanel != nil && !a.SidePanel.Visible() {
		a.SidePanel.Forget()
	}
}

func (a *App) sidePanelDocked() bool {
	return a.SidePanel != nil && a.SidePanel.Visible() && a.workspaceWidth >= sidePanelDockWidth
}

func (a *App) workspaceMain(c *ui.Context, theme nui.Theme) {
	workbench := a.Store.Workbench
	surface := a.Surfaces[workbench.ActiveSection]
	content := func() {
		ui.Column(c).Fill().MinHeight(0).Background(theme.MutedBackground).Children(func() {
			if surface.Content != nil {
				surface.Content(c, theme)
				return
			}
			emptyContent(c, theme)
		})
	}
	if !workbench.SecondarySidebarVisible {
		ui.Box(c).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(content)
		return
	}
	nui.Splitter(c.Key("secondary-sidebar"), theme, &workbench.SecondarySidebarPercent, nui.SplitterOptions{
		Percent: true,
		Min:     nui.Set[float32](15),
		Max:     nui.Set[float32](42),
	}, func() {
		ui.Column(c).Fill().MinHeight(0).Background(theme.Background).Children(func() {
			if surface.Sidebar != nil {
				surface.Sidebar(c, theme)
			}
		})
	}, content).Grow(1).Basis(0).MinWidth(0).FillHeight()
}

func emptyContent(c *ui.Context, theme nui.Theme) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Text(c, locales.CommonNoContent()).TextColor(theme.Muted)
	})
}

func (a *App) shutdownOverlay(c *ui.Context, theme nui.Theme) {
	if !a.Store.ShuttingDown {
		return
	}
	ui.Overlay(c, func() {
		ui.Column(c).Absolute().Top(0).Left(0).Fill().Center().Gap(8).Background(theme.Background.Alpha(0.85)).Children(func() {
			ui.Text(c, locales.AppShutdownTitle()).Bold().TextColor(theme.Highlighted)
			ui.Text(c, locales.AppLoading()).TextColor(theme.Muted)
		})
	})
	if !a.shutdownUI {
		a.shutdownUI = true
		if a.Host.ShutdownUIReady != nil {
			a.Host.ShutdownUIReady()
		}
	}
}

func (a *App) declareShortcuts() {
	workbench := a.Store.Workbench
	for commandID, section := range map[string]store.WorkspaceSection{
		shortcuts.WorkbenchCapture:       store.WorkspaceCapture,
		shortcuts.WorkbenchCategory:      store.WorkspaceCategory,
		shortcuts.WorkbenchAPICollection: store.WorkspaceAPICollection,
		shortcuts.WorkbenchRewriteRules:  store.WorkspaceRewriteRules,
		shortcuts.WorkbenchPythonPlugins: store.WorkspacePythonPlugins,
		shortcuts.WorkbenchMemStats:      store.WorkspaceMemStats,
	} {
		a.Dispatcher.Handle(shortcuts.Handler{CommandID: commandID, Run: func() { workbench.ActivateSection(section) }})
	}
	a.Dispatcher.Handle(shortcuts.Handler{CommandID: shortcuts.AppOpenSettings, Run: a.openSettings})
}

func (a *App) openSettings() {
	if a.Host.OpenSettings != nil {
		a.Host.OpenSettings()
	}
}
