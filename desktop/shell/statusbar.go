package shell

import (
	"context"
	"strconv"

	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/runtime"
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
	row := ui.Row(c).FillWidth().Height(statusBarHeight).AlignItems(ui.Center).PaddingX(10).Gap(8)
	row.BorderWidth(1, 0, 0, 0).BorderColor(theme.Border)
	row.Children(func() {
		total, selected := 0, 0
		if a.Counts != nil {
			total, selected = a.Counts()
		}
		text := locales.StatusTotal(strconv.Itoa(total))
		if selected > 0 {
			text += " " + locales.StatusSelected(strconv.Itoa(selected))
		}
		ui.Text(c, text).FontSize(12).TextColor(theme.Muted)
		ui.Box(c).Grow(1)
		a.statusButton(c, theme, "environment", locales.StatusActionEnvironment(), lucide.Info(), a.showEnvironment)
		a.statusButton(c, theme, "github", locales.StatusActionGithub(), lucide.ExternalLink(), a.openGithub)
		updateLabel, updateIcon, updateColor := a.updateStatus()
		a.colorStatusButton(c, theme, "update", updateLabel, updateIcon, updateColor, a.openUpdate)
		layoutLabel, layoutIcon := locales.StatusSwitchHorizontalLayout(), lucide.Columns2()
		if a.Store.Settings.TrafficDetailLayout() == settingservice.TrafficDetailLayoutHorizontal {
			layoutLabel, layoutIcon = locales.StatusSwitchVerticalLayout(), lucide.Rows2()
		}
		a.statusButton(c, theme, "layout", layoutLabel, layoutIcon, a.toggleDetailLayout)
	})
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

func (a *App) statusButton(c *ui.Context, theme nui.Theme, key, label string, icon *ui.SVG, run func()) {
	a.colorStatusButton(c, theme, key, label, icon, nui.Neutral, run)
}

func (a *App) colorStatusButton(c *ui.Context, theme nui.Theme, key, label string, icon *ui.SVG, color nui.Color, run func()) {
	button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{
		Variant: nui.Ghost, Color: color, Size: nui.XS, Square: true, Leading: icon, AccessibleLabel: label,
	}).OnClick(run)
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
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
	a.loadEnvironment()
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
				ui.Text(c, locales.AppLoading()).TextColor(theme.Muted)
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
