// Package shell builds the main window: its title bar, sidebars, content area,
// and status bar.
package shell

import (
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
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

// SidePanel is a panel that slides over the window from its edge, as a
// Slideover: the window behind it takes no input while it shows.
type SidePanel interface {
	Visible() bool
	// Build builds the panel. It is called every frame, shown or not, so
	// the panel can slide out.
	Build(c *ui.Context, theme nui.Theme)
}

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
	// SidePanel, when set, slides over the window while it is visible.
	SidePanel SidePanel
	Counts    TrafficCounts
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
	theme := theme.Begin(c, a.Store.Settings.Theme().Config)
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
	widgets.ToastViewport(c, theme)
	if a.Confirm.Open() {
		a.Dispatcher.NoteModal()
	}
	a.Confirm.Build(c, theme)
	a.environmentModal(c, theme)
	if a.SidePanel != nil {
		if a.SidePanel.Visible() {
			a.Dispatcher.NoteModal()
		}
		a.SidePanel.Build(c, theme)
	}
	for _, overlay := range a.Overlays {
		overlay(c, theme)
	}
	a.shutdownOverlay(c, theme)
	a.Dispatcher.Install(c)
}

func (a *App) workspace(c *ui.Context, theme nui.Theme) {
	ui.Row(c.Key("workspace-row")).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
		a.workspaceMain(c, theme)
	})
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
	// As 1.0.5's: a modal the user cannot dismiss, with the title, what is
	// happening, and a spinner, over a workbench that takes no input.
	a.Dispatcher.NoteModal()
	open := true
	nui.ModalWithOptions(c.Key("shutdown"), theme, &open, nui.ModalOptions{
		Title: locales.AppShutdownTitle(), Description: locales.AppShutdownDescription(),
		Width: nui.Set[float32](384), Dismissible: nui.Set(false), CloseButton: nui.Set(false),
	}, func(palette nui.Theme) {
		ui.Row(c.Key("shutdown-progress")).FillWidth().Justify(ui.Center).Padding(8).Children(func() {
			widgets.Spinner(c, palette).Size(32, 32).TextColor(palette.Primary).Label(locales.AppShutdownTitle())
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
