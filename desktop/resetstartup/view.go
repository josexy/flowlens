// Package resetstartup builds the progress window that Restore Initial State
// shows at the next launch, before any application data is opened.
package resetstartup

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	"github.com/josexy/flowlens/desktop/locale"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Service is how the window talks to the application process.
type Service interface {
	Show() error
	Ready() error
	Exit() error
	Retry() error
}

// View is the content of the progress window.
type View struct {
	Service Service
	Config  nui.AppConfig
	// Spawn runs slow work off the main thread and Post returns to it.
	Spawn func(func())
	Post  func(func())

	state     *appservice.ResetStartupState
	failure   string
	pending   bool
	shown     bool
	frames    int
	authorize bool
	ready     bool
}

// Apply shows the state the application process published.
func (v *View) Apply(state appservice.ResetStartupState) {
	v.state, v.failure = &state, ""
	locale.Apply(state.Language)
}

// Fail shows why the state or an action failed.
func (v *View) Fail(err error) { v.failure = err.Error() }

// Show makes the window visible, after which the application may be told that
// a frame has been drawn.
func (v *View) Show() {
	v.Spawn(func() {
		err := v.Service.Show()
		v.Post(func() {
			if err != nil {
				v.Fail(err)
				return
			}
			v.shown = true
		})
	})
}

func (v *View) act(action func() error, keepPending bool) {
	if v.pending {
		return
	}
	v.pending = true
	v.Spawn(func() {
		err := action()
		v.Post(func() {
			if err != nil {
				v.Fail(err)
				v.pending = false
				return
			}
			if !keepPending {
				v.pending = false
			}
		})
	})
}

// finishHandshake waits for two frames in the visible window before allowing
// the application process to delete anything.
func (v *View) finishHandshake(c *ui.Context) {
	if !v.shown || v.authorize {
		return
	}
	v.frames++
	if v.frames < 2 {
		c.After(16 * time.Millisecond)
		return
	}
	v.authorize = true
	v.Spawn(func() {
		err := v.Service.Ready()
		v.Post(func() {
			if err != nil {
				v.Fail(err)
				return
			}
			v.ready = true
		})
	})
}

func stageLabel(stage string) string {
	if stage == "" {
		return ""
	}
	return locales.Catalog.Format(i18n.Locale(), "settingsResetStartup"+strings.ToUpper(stage[:1])+stage[1:], nil)
}

func (v *View) stages(c *ui.Context, theme nui.Theme) {
	state := v.state
	if state == nil || len(state.Stages) == 0 {
		return
	}
	current := slices.Index(state.Stages, state.Stage)
	ui.Column(c.Key("stages")).FillWidth().Gap(8).Role(ui.RoleStatus).Label(stageLabel(state.Stage)).Children(func() {
		for index, stage := range state.Stages {
			failed := current == index && state.Error != ""
			icon, color := lucide.Circle(), theme.Muted
			switch {
			case failed:
				icon, color = lucide.CircleX(), theme.Error
			case current > index:
				icon, color = lucide.CircleCheck(), theme.Success
			case current == index:
				icon, color = lucide.LoaderCircle(), theme.Primary
			}
			ui.Row(c.Key(stage)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(color)
				ui.Text(c, stageLabel(stage)).FontSize(14).TextColor(color)
			})
		}
	})
}

// Build shows the window.
func (v *View) Build(c *ui.Context) {
	theme := nui.App(c, v.Config)
	v.finishHandshake(c)
	problem := v.failure
	if v.state != nil && v.state.Error != "" {
		problem = v.state.Error
	}
	ui.Column(c.Key("reset-startup")).Fill().Background(theme.Background).Padding(24).Gap(16).Children(func() {
		title := locales.SettingsResetStartupTitle()
		if problem != "" {
			title = locales.SettingsResetRecoveryTitle()
		}
		ui.Column(c.Key("head")).FillWidth().Gap(4).Children(func() {
			ui.Text(c, title).FontSize(18).Bold().TextColor(theme.Highlighted).FillWidth()
			if problem != "" {
				ui.Text(c, locales.SettingsResetRecoveryDescription()).FontSize(13).TextColor(theme.Muted).FillWidth()
			}
		})
		v.stages(c, theme)
		if problem != "" {
			ui.Text(c.Key("error"), problem).FontSize(13).TextColor(theme.Error).FillWidth().Role(ui.RoleAlert).Selectable()
		}
		ui.Box(c.Key("space")).Grow(1)
		ui.Row(c.Key("actions")).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			exit := nui.Button(c.Key("exit"), theme, locales.SettingsResetRecoveryExit(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: v.pending})
			if !v.pending {
				exit.OnClick(func() { v.act(v.Service.Exit, true) })
			}
			if v.state != nil && v.state.Error != "" {
				disabled := !v.ready || v.state.Busy || v.pending
				retry := nui.Button(c.Key("retry"), theme, locales.SettingsResetRecoveryRetry(), nui.ButtonOptions{Disabled: disabled, Loading: v.pending})
				if !disabled {
					retry.OnClick(func() { v.act(v.Service.Retry, false) })
				}
			}
		})
	})
}
