// Package update builds the Software Update window.
package update

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/flowlens/resources"
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

	confirmClose bool
	blocked      bool
	closing      bool
}

// New returns the window's content.
func New(data *store.Store, host Host, notifier *widgets.Notifier) *View {
	return &View{Data: data, Host: host, Notifier: notifier}
}

// CloseRequested decides whether the window may close now.
func (v *View) CloseRequested() bool {
	switch v.Data.Updater.Phase() {
	case appservice.UpdatePhaseChecking, appservice.UpdatePhaseDownloading:
		v.blocked, v.confirmClose = false, true
		return false
	case appservice.UpdatePhaseVerifying, appservice.UpdatePhasePreparing:
		v.blocked = true
		return false
	}
	return true
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

func (v *View) cancelAndClose() {
	if v.closing {
		return
	}
	v.closing = true
	v.Data.Updater.Cancel(func(err error) {
		v.closing = false
		if err != nil {
			v.report(err)
			return
		}
		v.confirmClose = false
		v.closeWindow()
	})
}

func stepForPhase(phase appservice.UpdatePhase) int {
	switch phase {
	case appservice.UpdatePhaseChecking:
		return 0
	case appservice.UpdatePhaseAvailable, appservice.UpdatePhaseDownloading:
		return 1
	case appservice.UpdatePhaseVerifying:
		return 2
	case appservice.UpdatePhasePreparing:
		return 3
	case appservice.UpdatePhaseReady:
		return 4
	}
	return -1
}

// stepProgress gives the step in progress, or -1, and the last completed step.
func stepProgress(snapshot appservice.UpdateSnapshot) (active, completed int) {
	failed := -1
	if snapshot.Failure != nil {
		failed = stepForPhase(snapshot.Failure.Stage)
	}
	var value int
	switch snapshot.Phase {
	case appservice.UpdatePhaseUpToDate:
		return -1, 0
	case appservice.UpdatePhaseReady:
		return -1, 3
	case appservice.UpdatePhaseError:
		value = max(failed, 0)
		active = value
		if failed >= 4 {
			active = -1
		}
	default:
		value = max(stepForPhase(snapshot.Phase), 0)
		active = value
	}
	return active, min(value-1, 3)
}

func (v *View) steps(c *ui.Context, theme nui.Theme) {
	snapshot := v.Data.Updater.Snapshot
	active, completed := stepProgress(snapshot)
	failed := snapshot.Phase == appservice.UpdatePhaseError
	items := []struct {
		title string
		icon  *ui.SVG
	}{
		{locales.UpdaterPhaseCheck(), lucide.Search()},
		{locales.UpdaterPhaseDownload(), lucide.Download()},
		{locales.UpdaterPhaseVerify(), lucide.ShieldCheck()},
		{locales.UpdaterPhasePrepare(), lucide.PackageCheck()},
	}
	ui.Row(c.Key("steps")).FillWidth().AlignItems(ui.Center).Label(locales.UpdaterProgressLabel()).Children(func() {
		for index, item := range items {
			if index > 0 {
				color := theme.Border
				if index <= completed+1 && completed >= 0 {
					color = theme.Primary
				}
				ui.Box(c.Key(index*10 + 1)).Grow(1).Basis(0).Height(1).Background(color)
			}
			icon, background, foreground := item.icon, theme.Elevated, theme.Muted
			titleColor := theme.Muted
			switch {
			case index <= completed:
				icon, background, foreground, titleColor = lucide.Check(), theme.Primary, theme.Background, theme.Text
			case failed && index == active:
				icon, background, foreground, titleColor = lucide.CircleAlert(), theme.Error.Alpha(0.12), theme.Error, theme.Error
			case index == active:
				background, foreground, titleColor = theme.Primary.Alpha(0.12), theme.Primary, theme.Text
			}
			ui.Column(c.Key(index * 10)).AlignItems(ui.Center).Gap(4).PaddingX(6).Children(func() {
				ui.Box(c.Key("circle")).Size(28, 28).Radius(14).Background(background).Center().Children(func() {
					ui.Icon(c, icon).Size(14, 14).TextColor(foreground)
				})
				ui.Text(c, item.title).SingleLine().FontSize(apptheme.TextSize(c, 12)).TextColor(titleColor)
			})
		}
	})
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
	case appservice.UpdatePhaseDownloading:
		return presentation{lucide.CloudDownload(), theme.Info, locales.UpdaterDownloadingTitle(), locales.UpdaterDownloadingDescription()}
	case appservice.UpdatePhaseVerifying:
		return presentation{lucide.ShieldCheck(), theme.Warning, locales.UpdaterVerifyingTitle(), locales.UpdaterVerifyingDescription()}
	case appservice.UpdatePhasePreparing:
		return presentation{lucide.PackageCheck(), theme.Warning, locales.UpdaterPreparingTitle(), locales.UpdaterPreparingDescription()}
	case appservice.UpdatePhaseReady:
		return presentation{lucide.Power(), theme.Success, locales.UpdaterReadyTitle(), locales.UpdaterReadyDescription()}
	case appservice.UpdatePhaseError:
		description := locales.UpdaterErrorDescription()
		if failure := updater.Snapshot.Failure; failure != nil && failure.Message != "" {
			description = failure.Message
		}
		return presentation{lucide.CircleAlert(), theme.Error, locales.UpdaterErrorTitle(), description}
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
	cardSection(c, theme, "progress").Gap(8).Children(func() {
		nui.Progress(c.Key("bar"), theme, percent, nui.ProgressOptions{Label: locales.UpdaterDownloadingTitle(), Size: nui.SM, Indeterminate: !known})
		if progress != nil {
			ui.Row(c.Key("summary")).FillWidth().Justify(ui.SpaceBetween).Gap(16).Children(func() {
				ui.Text(c, progressSummary(progress)).FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
				if known {
					ui.Text(c, strconv.Itoa(int(math.Round(percent)))+"%").FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
				}
			})
		}
	})
}

// cardSection is a part of the status card below a rule.
func cardSection(c *ui.Context, theme nui.Theme, key string) ui.Element {
	return ui.Column(c.Key(key)).FillWidth().Padding(16, 20).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border)
}

func detail(c *ui.Context, theme nui.Theme, key, label, value string, strong bool) {
	ui.Row(c.Key(key)).FillWidth().Gap(20).Children(func() {
		ui.Text(c, label).Width(110).Shrink(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		text := ui.Text(c, value).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).Font(theme.MonoFont).TextColor(theme.Toned)
		if strong {
			text.FontWeight(600).TextColor(theme.Text)
		}
	})
}

func (v *View) statusCard(c *ui.Context, theme nui.Theme) {
	updater := v.Data.Updater
	status := v.status(theme)
	nui.Card(c.Key("status"), theme, nui.CardOptions{
		Decoration: nui.Decoration{UI: nui.Styles{
			nui.Root: {Radius: nui.Set(float32(12)), BorderColor: nui.Set(theme.Border)},
			nui.Body: {PaddingX: nui.Set(float32(0)), PaddingY: nui.Set(float32(0)), Gap: nui.Set(float32(0))},
		}},
	}, func() {
		ui.Row(c.Key("line")).FillWidth().Gap(16).Padding(20).Children(func() {
			ui.Box(c.Key("icon")).Size(44, 44).Shrink(0).Radius(8).Background(status.color.Alpha(0.1)).Center().Children(func() {
				ui.Icon(c, status.icon).Size(24, 24).TextColor(status.color)
			})
			ui.Column(c.Key("text")).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, status.title).FontSize(apptheme.TextSize(c, 18)).LineHeight(24.0 / 18.0).Bold().TextColor(theme.Text).FillWidth()
				ui.Text(c, status.description).FontSize(apptheme.TextSize(c, 14)).LineHeight(20.0 / 14.0).TextColor(theme.Muted).FillWidth()
			})
		})
		if updater.Phase() == appservice.UpdatePhaseDownloading {
			v.downloadProgress(c, theme)
		}
		release := updater.Snapshot.Release
		if release == nil {
			return
		}
		cardSection(c, theme, "release").Gap(8).Children(func() {
			detail(c, theme, "version", locales.UpdaterReleaseVersion(), "v"+release.Version, true)
			if release.PublishedAt != "" {
				detail(c, theme, "published", locales.UpdaterPublishedAt(), publishedAt(release.PublishedAt), false)
			}
			if release.ArtifactName != "" {
				value := release.ArtifactName
				if release.ArtifactSize > 0 {
					value += " · " + formatSize(float64(release.ArtifactSize))
				}
				detail(c, theme, "artifact", locales.UpdaterArtifact(), value, false)
			}
		})
	}).FillWidth().Margin(10, 0, 0, 0).Shadow(0, 14, 30, -26, ui.RGBA(15, 23, 42, 0.35))
}

func publishedAt(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return logic.UnknownValue
	}
	return logic.FormatUnixMicrosLocal(parsed.UnixMicro())
}

func (v *View) notes(c *ui.Context, theme nui.Theme) {
	updater := v.Data.Updater
	release := updater.Snapshot.Release
	if release == nil {
		return
	}
	ui.Column(c.Key("notes")).FillWidth().Margin(6, 0, 0, 0).Gap(8).Children(func() {
		ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
			ui.Text(c, locales.UpdaterReleaseNotes()).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Text)
			label, color := locales.UpdaterModeManual(), nui.Info
			if updater.SelfUpdate() {
				label, color = locales.UpdaterModeSelf(), nui.Success
			}
			nui.Badge(c.Key("mode"), theme, label, nui.BadgeOptions{Color: color, Variant: nui.Subtle, Size: nui.SM})
		})
		ui.Scroll(c.Key("body")).FillWidth().MinHeight(96).MaxHeight(224).Radius(8).Border(1, theme.Border).Background(theme.Background).Children(func() {
			ui.Column(c.Key("text")).FillWidth().Padding(12, 16).Children(func() {
				if release.Notes == "" {
					ui.Text(c.Key("empty"), locales.UpdaterReleaseNotesEmpty()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
					return
				}
				markdown.View(c.Key("markdown"), release.Notes, &markdown.Options{OnLink: v.openLink})
			})
		})
	})
}

func (v *View) footer(c *ui.Context, theme nui.Theme) {
	ui.Row(c.Key("footer")).FillWidth().Shrink(0).Justify(ui.Center).Padding(12, 28).Background(theme.Elevated).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		ui.Row(c.Key("actions")).FillWidth().MaxWidth(768).Justify(ui.End).AlignItems(ui.Center).Gap(10).Children(func() { v.footerActions(c, theme) })
	})
}

func (v *View) footerActions(c *ui.Context, theme nui.Theme) {
	updater := v.Data.Updater
	phase := updater.Phase()
	switch {
	case updater.Snapshot.CanCancel:
		nui.Button(c.Key("cancel"), theme, locales.UpdaterActionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.X(), Loading: updater.ActionPending}).OnClick(func() { updater.Cancel(v.report) })
	case !updater.Busy():
		label := locales.UpdaterActionClose()
		if phase == appservice.UpdatePhaseReady {
			label = locales.UpdaterActionLater()
		}
		nui.Button(c.Key("close"), theme, label, nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).OnClick(v.closeWindow)
	}
	var label string
	var icon *ui.SVG
	switch phase {
	case appservice.UpdatePhaseAvailable:
		if updater.SelfUpdate() {
			label, icon = locales.UpdaterActionDownload(), lucide.Download()
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
		button := nui.Button(c.Key("primary"), theme, label, nui.ButtonOptions{Leading: icon, Loading: updater.ActionPending})
		if !updater.ActionPending {
			button.OnClick(v.runPrimary)
		}
	}
}

func (v *View) closeDialog(c *ui.Context, theme nui.Theme) {
	if !v.confirmClose {
		return
	}
	nui.ModalWithOptions(c.Key("close-confirm"), theme, &v.confirmClose, nui.ModalOptions{
		Title: locales.UpdaterCloseConfirmTitle(), Description: locales.UpdaterCloseConfirmDescription(), Width: nui.Set[float32](480), Dismissible: nui.Set(!v.closing),
		Footer: func(c *ui.Context, theme nui.Theme) {
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				back := nui.Button(c.Key("back"), theme, locales.UpdaterActionReturn(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: v.closing})
				cancel := nui.Button(c.Key("cancel-close"), theme, locales.UpdaterActionCancelAndClose(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Error, Loading: v.closing})
				background := nui.Button(c.Key("background"), theme, locales.UpdaterActionBackground(), nui.ButtonOptions{Leading: lucide.Minimize2(), Disabled: v.closing})
				if !v.closing {
					back.OnClick(func() { v.confirmClose = false })
					cancel.OnClick(v.cancelAndClose)
					background.OnClick(func() {
						v.confirmClose = false
						v.closeWindow()
					})
				}
			})
		},
	}, func(nui.Theme) {})
}

var appIcon = sync.OnceValue(func() ui.ImageSource {
	icon, err := ui.DecodeImage(resources.AppIcon)
	if err != nil {
		return nil
	}
	return icon
})

// Build shows the window.
func (v *View) Build(c *ui.Context) {
	updater := v.Data.Updater
	theme := apptheme.Begin(c, v.Data.Settings.Theme().Config)
	ui.Column(c.Key("update")).Fill().Background(theme.MutedBackground).Children(func() {
		v.Host.Frame.Build(c, theme)
		ui.Scroll(c.Key("scroll")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("content")).FillWidth().MaxWidth(768+2*28).Margin(0, ui.Auto, 0, ui.Auto).Gap(16).Padding(24, 28).Children(func() {
				ui.Row(c.Key("header")).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
					tile := ui.Box(c.Key("icon")).Size(52, 52).Shrink(0).Radius(12).Center().Background(theme.Elevated).Border(1, theme.Border).
						Shadow(0, 14, 30, -26, ui.RGBA(15, 23, 42, 0.35)).Clip()
					tile.Children(func() {
						ui.Box(c.Key("glow")).Absolute().Left(6).Right(6).Bottom(4).Height(4).Radius(2).Background(theme.Primary.Alpha(0.08))
						if icon := appIcon(); icon != nil {
							ui.Image(c.Key("image"), icon).Size(32, 32).Fit(ui.Contain)
						}
					})
					ui.Column(c.Key("heading")).Grow(1).Basis(0).MinWidth(0).Gap(4).Padding(2, 0, 0, 0).Children(func() {
						ui.Text(c, strings.ToUpper(locales.UpdaterEyebrow())).FontSize(apptheme.TextSize(c, 12)).FontWeight(600).LetterSpacing(1.7).TextColor(theme.Muted)
						ui.Row(c.Key("title")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
							ui.Text(c, locales.UpdaterWindowTitle()).FontSize(apptheme.TextSize(c, 20)).Bold().LetterSpacing(-0.4).TextColor(theme.Text)
							nui.Badge(c.Key("version"), theme, "v"+updater.Snapshot.CurrentVersion, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Subtle, Size: nui.SM})
						})
					})
				})
				v.steps(c, theme)
				v.statusCard(c, theme)
				if updater.Snapshot.Release != nil && !updater.SelfUpdate() {
					nui.Alert(c.Key("manual"), theme, nui.AlertOptions{Title: locales.UpdaterManualNoticeTitle(), Description: locales.UpdaterManualNoticeDescription(), Color: nui.Info, Variant: nui.Subtle, Icon: lucide.ExternalLink()})
				}
				if v.blocked {
					nui.Alert(c.Key("blocked"), theme, nui.AlertOptions{Title: locales.UpdaterCloseBlockedTitle(), Description: locales.UpdaterCloseBlockedDescription(), Color: nui.Warning, Variant: nui.Subtle, Icon: lucide.LockKeyhole(), Actions: func() {
						nui.Button(c.Key("understood"), theme, locales.UpdaterActionUnderstood(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Warning, Size: nui.SM}).OnClick(func() { v.blocked = false })
					}})
				}
				v.notes(c, theme)
			})
		})
		v.footer(c, theme)
	})
	v.Notifier.Show(c, theme)
	widgets.ToastViewport(c, theme)
	v.closeDialog(c, theme)
}
