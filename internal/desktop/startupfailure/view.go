// Package startupfailure builds the window FlowLens shows when it cannot start,
// so the user sees why and can retry instead of the application vanishing.
package startupfailure

import (
	"github.com/egoist/mygo/ui"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Stage is the startup step that failed.
type Stage string

// Startup steps, in the order they run.
const (
	StageDatabase Stage = "database"
	StageSettings Stage = "settings"
	StageServices Stage = "services"
)

// Failure is why startup stopped.
type Failure struct {
	Stage Stage
	Error string
}

// View is the content of the startup failure window.
type View struct {
	Config nui.AppConfig
	// Retry runs startup again and blocks until it ends. On failure the
	// application reports it with Fail; on success it replaces the window.
	Retry    func()
	OpenLogs func() error
	Quit     func()
	// Spawn runs slow work off the main thread and Post returns to it.
	Spawn func(func())
	Post  func(func())

	failure  Failure
	retrying bool
	opening  bool
	notice   string
}

// Fail shows why the latest attempt failed and allows another.
func (v *View) Fail(failure Failure) {
	v.failure, v.retrying, v.notice = failure, false, ""
}

// Retrying reports whether an attempt is running. The window cannot be closed
// meanwhile: what the attempt opens has to be released first.
func (v *View) Retrying() bool { return v.retrying }

func (v *View) retry() {
	if v.retrying {
		return
	}
	v.retrying, v.notice = true, ""
	v.Spawn(v.Retry)
}

func (v *View) openLogs() {
	if v.opening {
		return
	}
	v.opening, v.notice = true, ""
	v.Spawn(func() {
		err := v.OpenLogs()
		v.Post(func() {
			v.opening = false
			if err != nil {
				v.notice = locales.StartupFailureOpenLogsFailed(err.Error())
			}
		})
	})
}

func stageLabel(stage Stage) string {
	switch stage {
	case StageDatabase:
		return locales.StartupFailureStageDatabase()
	case StageSettings:
		return locales.StartupFailureStageSettings()
	default:
		return locales.StartupFailureStageServices()
	}
}

// Build shows the window.
func (v *View) Build(c *ui.Context) {
	theme := apptheme.Begin(c, v.Config)
	ui.Column(c.Key("startup-failure")).Fill().Background(theme.Background).Padding(24).Gap(16).Children(func() {
		ui.Column(c.Key("head")).FillWidth().Gap(4).Children(func() {
			ui.Text(c, locales.StartupFailureTitle()).FontSize(apptheme.TextSize(c, 18)).Bold().TextColor(theme.Highlighted).FillWidth()
			ui.Text(c, locales.StartupFailureDescription()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted).FillWidth()
		})
		label := stageLabel(v.failure.Stage)
		ui.Row(c.Key("stage")).FillWidth().Gap(8).AlignItems(ui.Center).Role(ui.RoleStatus).Label(label).Children(func() {
			ui.Icon(c, lucide.CircleX()).Size(16, 16).Shrink(0).TextColor(theme.Error)
			ui.Text(c, label).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Error)
		})
		ui.Scroll(c.Key("details")).FillWidth().Grow(1).Basis(0).MinHeight(0).Radius(8).Border(1, theme.Border).Background(theme.Elevated).Children(func() {
			ui.Column(c.Key("text")).FillWidth().Padding(12, 16).Children(func() {
				ui.Text(c.Key("error"), v.failure.Error).FontSize(apptheme.TextSize(c, 13)).TextColor(theme.Text).FillWidth().Role(ui.RoleAlert).Selectable()
			})
		})
		if v.notice != "" {
			ui.Text(c.Key("notice"), v.notice).FontSize(apptheme.TextSize(c, 13)).TextColor(theme.Error).FillWidth().Role(ui.RoleAlert).Selectable()
		}
		ui.Row(c.Key("actions")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			logs := nui.Button(c.Key("logs"), theme, locales.StartupFailureOpenLogs(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Leading: lucide.FolderOpen(), Loading: v.opening})
			if !v.opening {
				logs.OnClick(v.openLogs)
			}
			ui.Box(c.Key("space")).Grow(1)
			quit := nui.Button(c.Key("quit"), theme, locales.StartupFailureQuit(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: v.retrying})
			if !v.retrying {
				quit.OnClick(v.Quit)
			}
			retry := nui.Button(c.Key("retry"), theme, locales.StartupFailureRetry(), nui.ButtonOptions{Leading: lucide.RotateCw(), Loading: v.retrying})
			if !v.retrying {
				retry.OnClick(v.retry)
			}
		})
	})
}
