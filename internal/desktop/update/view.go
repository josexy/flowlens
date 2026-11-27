// Package update builds the Software Update window.
package update

import (
	"math"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Host is what the window needs from the application.
type Host struct {
	OpenURL func(string) error
	// Close closes the window without asking.
	Close func()
	// Frame is the window's title bar in custom title bar mode.
	Frame widgets.WindowFrame
}

// View is the content of the Software Update window.
type View struct {
	Data     *store.Store
	Host     Host
	Notifier *widgets.Notifier
}

// New returns the window's content.
func New(data *store.Store, host Host, notifier *widgets.Notifier) *View {
	return &View{Data: data, Host: host, Notifier: notifier}
}

func (v *View) report(err error) {
	if err != nil {
		v.Notifier.Error(locales.UpdaterActionFailed(err.Error()))
	}
}

func (v *View) openRelease() {
	release := v.Data.Updater.Snapshot.Release
	if release == nil || !logic.IsSafeUpdateURL(release.ReleaseURL) {
		v.Notifier.Error(locales.UpdaterActionFailed(locales.UpdaterInvalidReleaseUrl()))
		return
	}
	v.report(v.Host.OpenURL(release.ReleaseURL))
}

func (v *View) openLink(address string) {
	if logic.IsSafeUpdateURL(address) {
		v.report(v.Host.OpenURL(address))
	}
}

func (v *View) runPrimary() {
	updater := v.Data.Updater
	switch updater.Phase() {
	case appservice.UpdatePhaseAvailable:
		if updater.SelfUpdate() {
			updater.Download(v.report)
		} else {
			v.openRelease()
		}
	case appservice.UpdatePhaseReady:
		updater.Restart(v.report)
	case appservice.UpdatePhaseError:
		failure := updater.Snapshot.Failure
		switch {
		case failure != nil && failure.Stage == appservice.UpdatePhaseChecking:
			updater.Check(v.report)
		case failure != nil && failure.Stage == appservice.UpdatePhaseReady:
			updater.Restart(v.report)
		default:
			updater.Download(v.report)
		}
	default:
		updater.Check(v.report)
	}
}

func (v *View) closeWindow() {
	if v.Host.Close != nil {
		v.Host.Close()
	}
}

type presentation struct {
	icon               *ui.SVG
	color              ui.Color
	title, description string
}

func (v *View) status(theme nui.Theme) presentation {
	updater := v.Data.Updater
	switch updater.Phase() {
	case appservice.UpdatePhaseChecking:
		return presentation{lucide.Radar(), theme.Info, locales.UpdaterCheckingTitle(), locales.UpdaterCheckingDescription()}
	case appservice.UpdatePhaseUpToDate:
		return presentation{lucide.CircleCheckBig(), theme.Success, locales.UpdaterUpToDateTitle(), locales.UpdaterUpToDateDescription()}
	case appservice.UpdatePhaseAvailable:
		version := ""
		if release := updater.Snapshot.Release; release != nil {
			version = release.Version
		}
		if updater.SelfUpdate() {
			return presentation{lucide.PackageOpen(), theme.Primary, locales.UpdaterAvailableTitle(version), locales.UpdaterAvailableSelfDescription()}
		}
		return presentation{lucide.ExternalLink(), theme.Primary, locales.UpdaterAvailableTitle(version), locales.UpdaterAvailableManualDescription()}
	case appservice.UpdatePhaseInstalling:
		description := locales.UpdaterInstallingDescription()
		if !updater.Snapshot.CanCancel {
			description = locales.UpdaterFinishingDescription()
		}
		return presentation{lucide.Download(), theme.Primary, locales.UpdaterInstallingTitle(), description}
	case appservice.UpdatePhaseReady:
		return presentation{lucide.Power(), theme.Success, locales.UpdaterReadyTitle(), locales.UpdaterReadyDescription()}
	case appservice.UpdatePhaseError:
		description := locales.UpdaterErrorDescription()
		if failure := updater.Snapshot.Failure; failure != nil && failure.Message != "" {
			description = failure.Message
		}
		title := locales.UpdaterErrorTitle()
		if failure := updater.Snapshot.Failure; failure != nil && failure.Stage == appservice.UpdatePhaseReady {
			title = locales.UpdaterRestartErrorTitle()
		}
		return presentation{lucide.CircleAlert(), theme.Error, title, description}
	}
	return presentation{lucide.RefreshCw(), theme.Muted, locales.UpdaterIdleTitle(), locales.UpdaterIdleDescription()}
}

func formatSize(bytes float64) string {
	return logic.FormatFileSizeWith(int64(bytes), logic.FileSizeOptions{Precision: 1})
}

func progressSummary(progress *appservice.UpdateProgress) string {
	written := formatSize(float64(progress.Written))
	total := locales.UpdaterUnknownSize()
	if progress.Total > 0 {
		total = formatSize(float64(progress.Total))
	}
	if progress.Rate > 0 {
		return locales.UpdaterProgressWithRate(formatSize(progress.Rate)+"/s", total, written)
	}
	return locales.UpdaterProgress(total, written)
}

func (v *View) downloadProgress(c *ui.Context, theme nui.Theme) {
	progress := v.Data.Updater.Snapshot.Progress
	known := progress != nil && progress.Total > 0
	percent := 0.0
	if known {
		percent = math.Min(100, math.Max(0, float64(progress.Written)/float64(progress.Total)*100))
	}
	ui.Column(c.Key("progress")).FillWidth().Gap(8).Children(func() {
		nui.Progress(c.Key("bar"), theme, percent, nui.ProgressOptions{Label: locales.UpdaterDownloadProgress(), Size: nui.SM, Indeterminate: !known || !v.Data.Updater.Snapshot.CanCancel})
		if progress != nil && v.Data.Updater.Snapshot.CanCancel {
			ui.Row(c.Key("summary")).FillWidth().Justify(ui.SpaceBetween).Gap(16).Children(func() {
				ui.Text(c, progressSummary(progress)).FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
				if known {
					ui.Text(c, strconv.Itoa(int(math.Round(percent)))+"%").FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
				}
			})
		}
	})
}

func (v *View) statusHeader(c *ui.Context, theme nui.Theme) {
	status := v.status(theme)
	ui.Row(c.Key("status")).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Box(c.Key("icon")).Size(48, 48).Shrink(0).Children(func() {
			if icon := widgets.AppIcon(); icon != nil {
				ui.Image(c.Key("image"), icon).Size(48, 48).Fit(ui.Contain)
			}
		})
		ui.Column(c.Key("text")).Grow(1).Basis(0).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, status.title).FillWidth().FontSize(apptheme.TextSize(c, 20)).LineHeight(1.25).Bold().TextColor(theme.Text)
			ui.Text(c, locales.UpdaterCurrentVersion(v.Data.Updater.Snapshot.CurrentVersion)).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		})
	})
	ui.Row(c.Key("message")).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, status.icon).Size(16, 16).Shrink(0).Margin(2, 0, 0, 0).TextColor(status.color)
		ui.Text(c, status.description).Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.45).TextColor(theme.Toned)
	})
	if v.Data.Updater.Phase() == appservice.UpdatePhaseInstalling {
		v.downloadProgress(c, theme)
	}
}

func publishedAt(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return logic.UnknownValue
	}
	return logic.FormatDateLocal(parsed)
}

func (v *View) notes(c *ui.Context, theme nui.Theme) {
	release := v.Data.Updater.Snapshot.Release
	if release == nil {
		return
	}
	ui.Column(c.Key("notes")).FillWidth().Gap(12).Padding(20, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		ui.Row(c.Key("heading")).FillWidth().AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			ui.Text(c, locales.UpdaterReleaseNotes()).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Text)
			nui.Badge(c.Key("version"), theme, "v"+release.Version, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Subtle, Size: nui.SM})
		})
		if release.PublishedAt != "" {
			ui.Text(c, locales.UpdaterPublishedAt()+": "+publishedAt(release.PublishedAt)).FillWidth().FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		}
		if release.Notes == "" {
			ui.Text(c, locales.UpdaterReleaseNotesEmpty()).FillWidth().FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		} else {
			markdown.View(c.Key("markdown"), release.Notes, &markdown.Options{OnLink: v.openLink})
		}
	})
}

func (v *View) canInstall() bool {
	u := v.Data.Updater
	return u.SelfUpdate() && u.Snapshot.Release != nil && (u.Phase() == appservice.UpdatePhaseAvailable ||
		(u.Phase() == appservice.UpdatePhaseError && u.Snapshot.Failure != nil && u.Snapshot.Failure.Stage == appservice.UpdatePhaseInstalling))
}

func (v *View) footer(c *ui.Context, theme nui.Theme) {
	u := v.Data.Updater
	ui.Column(c.Key("footer")).FillWidth().Shrink(0).Gap(16).Padding(16, 24).Background(theme.Background).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		if v.canInstall() {
			nui.Checkbox(c.Key("restart-choice"), theme, &u.RestartAfterInstall, nui.CheckboxOptions{
				Label: locales.UpdaterRestartAfterInstall(), Description: locales.UpdaterRestartAfterInstallHint(), Disabled: u.ActionPending,
			})
		} else if u.Phase() == appservice.UpdatePhaseInstalling {
			message := locales.UpdaterManualRestartDescription()
			if u.Snapshot.RestartAfterInstall {
				message = locales.UpdaterAutoRestartDescription()
			}
			ui.Text(c, message).FillWidth().FontSize(apptheme.TextSize(c, 12)).LineHeight(1.4).TextColor(theme.Muted)
		}
		ui.Row(c.Key("actions")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			label := locales.UpdaterActionClose()
			if u.Busy() {
				label = locales.UpdaterActionBackground()
			} else if u.Phase() == appservice.UpdatePhaseReady {
				label = locales.UpdaterActionLater()
			}
			nui.Button(c.Key("close"), theme, label, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral}).OnClick(v.closeWindow)
			ui.Spacer(c)
			v.footerActions(c, theme)
		})
	})
}

func (v *View) footerActions(c *ui.Context, theme nui.Theme) {
	u := v.Data.Updater
	if u.Snapshot.CanCancel {
		nui.Button(c.Key("cancel"), theme, locales.UpdaterActionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Loading: u.ActionPending}).OnClick(func() { u.Cancel(v.report) })
		return
	}
	var label string
	var icon *ui.SVG
	switch u.Phase() {
	case appservice.UpdatePhaseAvailable:
		if u.SelfUpdate() {
			label, icon = locales.UpdaterActionDownload(), lucide.Download()
			if u.RestartAfterInstall {
				label = locales.UpdaterActionInstallRestart()
			}
		} else {
			label, icon = locales.UpdaterActionOpenRelease(), lucide.ExternalLink()
		}
	case appservice.UpdatePhaseReady:
		label, icon = locales.UpdaterActionRestart(), lucide.Power()
	case appservice.UpdatePhaseError:
		label, icon = locales.UpdaterActionRetry(), lucide.RefreshCw()
	case appservice.UpdatePhaseIdle, appservice.UpdatePhaseUpToDate:
		label, icon = locales.UpdaterActionCheck(), lucide.Search()
	}
	if label != "" {
		nui.Button(c.Key("primary"), theme, label, nui.ButtonOptions{Leading: icon, Loading: u.ActionPending, Disabled: u.ActionPending}).OnClick(v.runPrimary)
	}
}

// Build shows the window. Operations belong to the service, not this view.
func (v *View) Build(c *ui.Context) {
	theme := apptheme.Begin(c, v.Data.Settings.Theme().Config)
	ui.Column(c.Key("update")).Fill().Background(theme.Background).Children(func() {
		v.Host.Frame.Build(c, theme)
		ui.Scroll(c.Key("scroll")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("content")).FillWidth().MaxWidth(760).Margin(0, ui.Auto, 0, ui.Auto).Gap(20).Padding(24).Children(func() {
				v.statusHeader(c, theme)
				v.notes(c, theme)
			})
		})
		v.footer(c, theme)
	})
	v.Notifier.Show(c, theme)
	widgets.ToastViewport(c, theme)
}
