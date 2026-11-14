package shell

import (
	"context"
	"strconv"

	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	statusBarHeight  = 28
	githubProjectURL = "https://github.com/josexy/flowlens"
)

type environmentDialog struct {
	open    bool
	loading bool
	info    *appservice.EnvironmentInfo
}

func (a *App) statusBar(c *ui.Context, theme nui.Theme) {
	row := ui.Row(c).FillWidth().MinHeight(statusBarHeight).Shrink(0).AlignItems(ui.Center).Justify(ui.End).PaddingX(16).Background(theme.Background)
	row.Draw(func(p *ui.Painter, bounds ui.Rect) {
		p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: 1 / p.Scale()}, theme.BorderAccented, 0)
	})
	row.Children(func() {
		total, selected := 0, 0
		if a.Counts != nil {
			total, selected = a.Counts()
		}
		text := locales.StatusTotal(strconv.Itoa(total))
		if selected > 0 {
			text += " " + locales.StatusSelected(strconv.Itoa(selected))
		}
		ui.Row(c).Absolute().Left(192).Right(192).Top(0).Bottom(0).Center().IgnoreInput(true).Children(func() {
			ui.Text(c, text).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).FontWeight(500).TextColor(theme.Muted)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(2).Shrink(0).Children(func() {
			a.statusButton(c, theme, "environment", locales.StatusActionEnvironment(), lucide.Info(), false, a.showEnvironment)
			a.statusButton(c, theme, "github", locales.StatusActionGithub(), widgets.GitHubIcon(), false, a.openGithub)
			updateLabel, updateIcon, updateColor := a.updateStatus()
			a.colorStatusButton(c, theme, "update", updateLabel, updateIcon, updateColor, false, a.openUpdate)
			if a.showLayoutToggle() {
				settings := a.Store.Settings
				layoutLabel, layoutIcon := locales.StatusSwitchHorizontalLayout(), lucide.Rows2()
				if settings.TrafficDetailLayout() == settingservice.TrafficDetailLayoutHorizontal {
					layoutLabel, layoutIcon = locales.StatusSwitchVerticalLayout(), lucide.Columns2()
				}
				a.statusButton(c, theme, "layout", layoutLabel, layoutIcon, settings.Current == nil || settings.SavingTrafficDetailConfig, a.toggleDetailLayout)
			}
		})
	})
}

// showLayoutToggle reports whether a traffic table, which the layout applies
// to, is on show.
func (a *App) showLayoutToggle() bool {
	if a.Store.Workbench.ActiveContent != store.ContentTraffic {
		return false
	}
	if a.Store.Workspace == nil {
		return true
	}
	switch a.Store.Workspace.ActiveTab().Type {
	case logic.TabTypeCapture, logic.TabTypeHistory:
		return true
	}
	return false
}

// updateStatus describes the update state for the status bar.
func (a *App) updateStatus() (string, *ui.SVG, nui.Color) {
	updater := a.Store.Updater
	if updater == nil {
		return locales.StatusActionUpdate(), lucide.RefreshCw(), nui.Neutral
	}
	switch updater.Phase() {
	case appservice.UpdatePhaseReady:
		return locales.StatusUpdateReady(), lucide.CircleCheck(), nui.Primary
	case appservice.UpdatePhaseError:
		return locales.StatusUpdateError(), lucide.CircleAlert(), nui.Error
	case appservice.UpdatePhaseChecking:
		return locales.StatusUpdateChecking(), lucide.LoaderCircle(), nui.Neutral
	case appservice.UpdatePhaseDownloading:
		return locales.UpdaterDownloadingTitle(), lucide.LoaderCircle(), nui.Primary
	case appservice.UpdatePhaseVerifying:
		return locales.UpdaterVerifyingTitle(), lucide.LoaderCircle(), nui.Primary
	case appservice.UpdatePhasePreparing:
		return locales.UpdaterPreparingTitle(), lucide.LoaderCircle(), nui.Primary
	case appservice.UpdatePhaseAvailable:
		return locales.StatusUpdateAvailable(), lucide.Download(), nui.Primary
	}
	return locales.StatusActionUpdate(), lucide.RefreshCw(), nui.Neutral
}

func (a *App) statusButton(c *ui.Context, theme nui.Theme, key, label string, icon *ui.SVG, disabled bool, run func()) {
	a.colorStatusButton(c, theme, key, label, icon, nui.Neutral, disabled, run)
}

// colorStatusButton is a small icon button, muted until hovered unless color
// calls attention to it.
func (a *App) colorStatusButton(c *ui.Context, theme nui.Theme, key, label string, icon *ui.SVG, color nui.Color, disabled bool, run func()) {
	button := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(6).Center().Label(label).Disabled(disabled)
	foreground := theme.Muted
	switch color {
	case nui.Primary:
		foreground = theme.Primary
	case nui.Error:
		foreground = theme.Error
	}
	switch {
	case disabled:
		foreground = foreground.Alpha(0.5)
	case button.Pressed():
		button.Background(theme.Primary.Alpha(0.08))
		foreground = theme.Primary
	case button.Hovered():
		button.Background(theme.Primary.Alpha(0.06))
		foreground = theme.Primary
	}
	if !disabled {
		button.OnClick(run)
	}
	button.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(foreground) })
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{Placement: nui.Set(nui.TopCenter)})
}

func (a *App) openGithub() {
	if a.Host.OpenURL == nil {
		return
	}
	if err := a.Host.OpenURL(githubProjectURL); err != nil {
		a.Notifier.Error(locales.StatusGithubOpenFailed(err.Error()))
	}
}

func (a *App) openUpdate() {
	if a.Host.OpenUpdate != nil {
		a.Host.OpenUpdate()
	}
}

func (a *App) toggleDetailLayout() {
	a.Store.Settings.ToggleTrafficDetailLayout(func(err error) {
		if err != nil {
			a.Notifier.Error(locales.StatusLayoutSaveFailed(err.Error()))
		}
	})
}

func (a *App) showEnvironment() {
	a.environment.open = true
	if a.environment.info == nil && !a.environment.loading {
		a.loadEnvironment()
	}
}

func (a *App) loadEnvironment() {
	if a.Host.Environment == nil {
		return
	}
	a.environment.loading = true
	runtime.Run(a.Store.Runtime.Jobs, "shell/environment", func(context.Context) (appservice.EnvironmentInfo, error) {
		return a.Host.Environment(), nil
	}, func(info appservice.EnvironmentInfo, err error) {
		a.environment.loading = false
		if err != nil {
			a.Notifier.Error(locales.StatusEnvironmentLoadFailed(err.Error()))
			return
		}
		a.environment.info = &info
	})
}

func (a *App) environmentModal(c *ui.Context, theme nui.Theme) {
	if !a.environment.open {
		return
	}
	a.Dispatcher.NoteModal()
	nui.Modal(c.Key("environment"), theme, &a.environment.open, locales.StatusEnvironmentTitle(), func(theme nui.Theme) {
		info := a.environment.info
		if info == nil {
			if a.environment.loading {
				ui.Row(c).FillWidth().MinHeight(112).Center().Gap(8).Children(func() {
					ui.Spinner(c).Size(16, 16).TextColor(theme.Muted)
					ui.Text(c, locales.AppLoading()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
				})
			}
			return
		}
		widgets.KeyValueTable(c, theme, []widgets.KeyValue{
			{Key: locales.StatusEnvironmentAppVersion(), Value: info.AppVersion},
			{Key: locales.StatusEnvironmentGo(), Value: info.GoVersion},
			{Key: locales.StatusEnvironmentMygo(), Value: info.MyGoVersion},
			{Key: locales.StatusEnvironmentBuildCommit(), Value: info.BuildCommit},
			{Key: locales.StatusEnvironmentPlatform(), Value: info.GOOS + "/" + info.GOARCH},
		})
	})
}
