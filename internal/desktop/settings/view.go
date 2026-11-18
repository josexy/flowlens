// Package settings builds the Settings window.
package settings

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/logger"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// FilePicker describes a file dialog: its title, the message the platform
// shows where it has one (macOS), and the files it offers by extension
// under FilterName, besides all files.
type FilePicker struct {
	Title, Message, FilterName string
	Patterns                   []string
}

// Host is what the window needs from the services.
type Host struct {
	ListFonts   func() []settingservice.FontOption
	CAInfo      func() (*settingservice.CACertificateInfo, error)
	CATrust     func() (*settingservice.CACertificateTrustStatus, error)
	GenerateCA  func(overwrite bool) error
	InstallCA   func(fingerprint string) error
	UninstallCA func(fingerprint string) error
	PickFile    func(FilePicker) (string, error)
	PickFiles   func(FilePicker) ([]string, error)
	// RestartProxy restarts a running proxy so that saved changes take effect.
	RestartProxy func()

	LogStatus  func() (*logger.LogStatus, error)
	SetLog     func(enabled bool) (*logger.LogStatus, error)
	SetLevel   func(level logger.LogLevel) (*logger.LogStatus, error)
	ClearLogs  func() error
	OpenLogDir func() error

	DataSize     func() (proxyservice.LocalDataSize, error)
	ClearCache   func() error
	ClearAllData func() error

	PythonStatus func() (*pythonpluginservice.RuntimeStatus, error)
	TestPython   func(path string) (*pythonpluginservice.RuntimeStatus, error)
	FindPython   func(path string) ([]pythonpluginservice.InterpreterCandidate, error)

	// Frame is the window's title bar in custom title bar mode.
	Frame widgets.WindowFrame
	// Close closes the window.
	Close func()
	// DirtyChanged hears whether unsaved changes exist.
	DirtyChanged func(dirty bool)
}

// Sections of the window.
const (
	tabGeneral   = "general"
	tabProxy     = "proxy"
	tabPython    = "pythonPlugins"
	tabShortcuts = "shortcuts"
	tabLogs      = "logs"
	tabStorage   = "storage"
)

const saveNoticeDuration = 5 * time.Second

// View is the content of the Settings window.
type View struct {
	Data       *store.Store
	Host       Host
	Dispatcher *shortcuts.Dispatcher
	Notifier   *widgets.Notifier
	Confirm    *widgets.ConfirmDialog
	// sections counts the sections built on the page so far.
	sections int
	MacOS    bool

	tab        string
	committing bool
	notice     string
	noticeKind nui.Color
	noticeEnds time.Time
	showNotice bool
	clipboard  *string

	general   generalState
	proxy     proxyState
	python    pythonState
	shortcuts shortcutState
	logs      logState
	storage   storageState
	started   bool
}

// New returns the window's content.
func New(data *store.Store, host Host, dispatcher *shortcuts.Dispatcher, notifier *widgets.Notifier, confirm *widgets.ConfirmDialog) *View {
	return &View{Data: data, Host: host, Dispatcher: dispatcher, Notifier: notifier, Confirm: confirm, tab: tabGeneral, MacOS: shortcuts.IsMacOS()}
}

// Begin starts editing a private copy of the settings.
func (v *View) Begin() {
	v.Data.Settings.BeginEdit()
	v.tab = tabGeneral
	v.started = true
	v.showNotice = false
}

// Dirty reports whether the window holds unsaved changes.
func (v *View) Dirty() bool { return v.started && v.Data.Settings.Dirty() }

func message(key string, args ...any) string {
	if len(args) == 0 {
		return locales.Catalog.Format(i18n.Locale(), key, nil)
	}
	values := map[string]any{}
	for index := 0; index+1 < len(args); index += 2 {
		values[fmt.Sprint(args[index])] = args[index+1]
	}
	return locales.Catalog.Format(i18n.Locale(), key, values)
}

// camel turns snake_case into the suffix of a catalog key.
func camel(value string) string {
	var out strings.Builder
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		out.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return out.String()
}

// syncDirty updates each section's unsaved state and previews the draft.
func (v *View) syncDirty() {
	settings := v.Data.Settings
	if settings.Draft == nil || v.committing {
		return
	}
	settings.SyncDirty()
	settings.PreviewDraftAppearance()
}

// validate returns why the draft cannot be saved.
func (v *View) validate() string {
	proxy := v.Data.Settings.Draft.ProxyConfig
	if proxy.UpstreamMode == settingservice.UpstreamProxyModeCustom && !validUpstream(proxy.UpstreamProxy) {
		return locales.SettingsUpstreamProxyCustomRequired()
	}
	if !proxy.UseBuiltinCA && (strings.TrimSpace(proxy.CACertPath) == "" || strings.TrimSpace(proxy.CAKeyPath) == "") {
		return locales.SettingsCaTrustErrorsCaCustomPathsRequired()
	}
	return ""
}

func validUpstream(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return true
	}
	return false
}

var caErrorCode = regexp.MustCompile(`\bca_(?:trust|builtin|custom)_[a-z_]+\b`)

// caErrorMessage translates the error codes of the certificate operations.
func caErrorMessage(err error) string {
	detail := err.Error()
	if code := caErrorCode.FindString(detail); code != "" {
		key := "settingsCaTrustErrors" + camel(code)
		if text := message(key); text != "" && text != key {
			return text
		}
	}
	return detail
}

func (v *View) save() {
	settings := v.Data.Settings
	if v.committing || settings.Saving || settings.Draft == nil {
		return
	}
	if problem := v.validate(); problem != "" {
		v.Notifier.Error(problem)
		return
	}
	v.committing = true
	started := settings.Save(func(result store.SaveResult, err error) {
		v.committing = false
		if err != nil {
			v.Notifier.Error(locales.SettingsSaveFailed(caErrorMessage(err)))
			return
		}
		v.setSavedNotice()
		if v.tab == tabProxy {
			v.loadCA()
		}
	})
	if !started {
		v.committing = false
	}
}

func (v *View) setSavedNotice() {
	settings := v.Data.Settings
	v.showNotice = true
	v.noticeEnds = time.Time{}
	switch shortcut, proxy := settings.LastShortcutResult, settings.LastProxyApplyResult; {
	case shortcut != nil && !shortcut.Applied:
		code := shortcut.ErrorCode
		if code == "" {
			code = "unknown"
		}
		v.notice, v.noticeKind = message("shortcutsApplyErrors"+camel(code)), nui.Error
	case proxy != nil && proxy.Applied && !proxy.RestartRequired:
		v.notice, v.noticeKind = locales.SettingsSavedAppliedNotice(), nui.Success
	case proxy != nil && proxy.RestartRequired:
		reasons := make([]string, len(proxy.RestartReasons))
		for index, reason := range proxy.RestartReasons {
			reasons[index] = message("settingsRestartReason" + camel(reason))
		}
		v.notice, v.noticeKind = locales.SettingsSavedPartialRestartNotice(strings.Join(reasons, ", ")), nui.Warning
	default:
		v.notice, v.noticeKind = locales.SettingsSavedNotice(), nui.Success
	}
}

func (v *View) reset() {
	settings := v.Data.Settings
	if settings.Draft == nil {
		return
	}
	ids := make([]string, len(shortcuts.Catalog))
	for index, command := range shortcuts.Catalog {
		ids[index] = command.ID
	}
	settings.ResetDraftToDefaults(ids)
	settings.PreviewDraftAppearance()
}

// CloseRequested decides whether the window may close, asking first when
// changes would be lost.
func (v *View) CloseRequested() bool {
	if !v.Dirty() {
		v.finish()
		return true
	}
	v.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.SettingsQuitDirtyTitle(), Message: locales.SettingsQuitDirtyMessage(), CancelLabel: locales.SettingsQuitDirtyCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.SettingsQuitDirtyConfirm(), Color: nui.Warning, Run: func() {
			v.finish()
			if v.Host.Close != nil {
				v.Host.Close()
			}
		}},
	})
	return false
}

func (v *View) finish() {
	v.Data.Settings.EndEdit()
	v.started = false
}

// AskDiscardBeforeQuit asks whether to lose unsaved changes before quitting
// or updating.
func (v *View) AskDiscardBeforeQuit(update bool, proceed, cancel func()) {
	title, text, confirm := locales.SettingsQuitDirtyTitle(), locales.SettingsQuitDirtyMessage(), locales.SettingsQuitDirtyConfirm()
	if update {
		text, confirm = locales.SettingsUpdateDirtyMessage(), locales.SettingsUpdateDirtyConfirm()
	}
	v.Confirm.Ask(widgets.ConfirmRequest{
		Title: title, Message: text, CancelLabel: locales.SettingsQuitDirtyCancel(), OnCancel: cancel,
		Confirm: widgets.ConfirmAction{Label: confirm, Color: nui.Warning, Run: func() {
			v.finish()
			if v.Host.Close != nil {
				v.Host.Close()
			}
			proceed()
		}},
	})
}

func (v *View) declareShortcuts() {
	settings := v.Data.Settings
	v.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.AppSave, Priority: 10,
		When: func() bool { return settings.Dirty() && !settings.Saving && !v.committing },
		Run:  v.save,
	})
}

type navItem struct {
	key   string
	label string
	icon  *ui.SVG
}

func navItems() []navItem {
	return []navItem{
		{tabGeneral, locales.SettingsTabGeneral(), lucide.Settings()},
		{tabProxy, locales.SettingsTabProxy(), lucide.Server()},
		{tabPython, locales.SettingsTabPythonPlugins(), lucide.FileCode()},
		{tabShortcuts, locales.SettingsTabShortcuts(), lucide.Keyboard()},
		{tabLogs, locales.SettingsTabLogs(), lucide.FileText()},
		{tabStorage, locales.SettingsTabStorage(), lucide.Archive()},
	}
}

func (v *View) selectTab(key string) {
	if key == v.tab {
		return
	}
	if v.tab == tabShortcuts {
		v.shortcuts.stop()
	}
	v.tab = key
	switch key {
	case tabStorage:
		v.loadDataSize()
	case tabProxy:
		v.loadCA()
	case tabLogs:
		v.loadLogs()
	case tabPython:
		v.refreshPython()
	}
}

// Build shows the window.
func (v *View) Build(c *ui.Context) {
	settings := v.Data.Settings
	if settings.Draft != nil {
		v.syncDirty()
	}
	theme := apptheme.Begin(c, settings.Theme().Config)
	v.Dispatcher.Begin()
	if v.clipboard != nil {
		c.WriteClipboard(*v.clipboard)
		v.clipboard = nil
	}
	v.declareShortcuts()
	ui.Column(c.Key("settings-window")).Fill().Background(theme.MutedBackground).Children(func() {
		v.Host.Frame.Build(c, theme)
		ui.Row(c.Key("settings-body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			v.nav(c, theme)
			ui.Column(c.Key("settings-main")).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
				v.page(c, theme)
				v.footer(c, theme)
			})
		})
	})
	v.Notifier.Show(c, theme)
	widgets.ToastViewport(c, theme)
	if v.Confirm.Open() {
		v.Dispatcher.NoteModal()
	}
	v.Confirm.Build(c, theme)
	v.Dispatcher.Install(c)
	if v.Host.DirtyChanged != nil {
		v.Host.DirtyChanged(v.Dirty())
	}
}

func (v *View) nav(c *ui.Context, theme nui.Theme) {
	ui.Column(c.Key("settings-nav")).Width(160).Shrink(0).FillHeight().Gap(2).Padding(16, 8).Background(theme.Background).BorderWidth(0, 1, 0, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c, strings.ToUpper(locales.MenuSettings())).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).LetterSpacing(0.84).TextColor(theme.Muted).Padding(0, 8, 10, 8)
		for _, item := range navItems() {
			active := v.tab == item.key
			row := ui.ButtonBase(c.Key(item.key)).FillWidth().Justify(ui.Start).Gap(8).Padding(7, 10).Radius(6).FocusRing(true).AccessSelected(active).Label(item.label)
			color, weight := theme.Toned, 400
			switch {
			case active:
				row.Background(theme.Primary.Alpha(0.12))
				color, weight = theme.Primary, 600
			case row.Hovered():
				row.Background(theme.Elevated)
				color = theme.Text
			}
			key := item.key
			row.OnClick(func() { v.selectTab(key) })
			row.Children(func() {
				if active {
					ui.Box(c.Key("bar")).Absolute().Left(0).Top(6).Bottom(6).Width(3).Radius(1.5).Background(theme.Primary).IgnoreInput(true)
				}
				ui.Icon(c, item.icon).Size(15, 15).Shrink(0).TextColor(color)
				ui.Text(c, item.label).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).FontWeight(weight).TextColor(color)
			})
		}
	})
}

func (v *View) tabTitle() string {
	for _, item := range navItems() {
		if item.key == v.tab {
			return item.label
		}
	}
	return locales.MenuSettings()
}

func (v *View) page(c *ui.Context, theme nui.Theme) {
	draft := v.Data.Settings.Draft
	v.sections = 0
	ui.Scroll(c.Key(v.tab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		ui.Column(c.Key("page")).FillWidth().MaxWidth(1040+2*28).Padding(0, 28, 20, 28).Children(func() {
			title := ui.Row(c.Key("title")).FillWidth().Padding(20, 0, 16, 0).Margin(0, 0, 24, 0).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
			title.Children(func() {
				ui.Text(c, v.tabTitle()).FontSize(apptheme.TextSize(c, 18)).LineHeight(1.35).Bold().TextColor(theme.Text)
			})
			if draft == nil {
				ui.Row(c.Key("loading")).FillWidth().MinHeight(160).Center().Children(func() { widgets.Spinner(c, theme) })
				return
			}
			ui.Column(c.Key("content")).FillWidth().Children(func() {
				switch v.tab {
				case tabProxy:
					v.proxyPage(c, theme, draft)
				case tabPython:
					v.pythonPage(c, theme, draft)
				case tabShortcuts:
					v.shortcutsPage(c, theme, draft)
				case tabLogs:
					v.logsPage(c, theme)
				case tabStorage:
					v.storagePage(c, theme, draft)
				default:
					v.generalPage(c, theme, draft)
				}
			})
		})
	})
}

func (v *View) footer(c *ui.Context, theme nui.Theme) {
	settings := v.Data.Settings
	if v.showNotice {
		now := c.Now()
		if v.noticeEnds.IsZero() {
			v.noticeEnds = now.Add(saveNoticeDuration)
		}
		if settings.Dirty() || !now.Before(v.noticeEnds) {
			v.showNotice = false
		} else {
			c.After(v.noticeEnds.Sub(now))
			ui.Column(c.Key("notice")).FillWidth().Shrink(0).Gap(6).Padding(10, 32).Background(theme.Elevated).Children(func() {
				nui.Alert(c, theme, nui.AlertOptions{Title: v.notice, Color: v.noticeKind, Variant: nui.Subtle})
				if proxy := settings.LastProxyApplyResult; proxy != nil && proxy.RestartRequired && v.Host.RestartProxy != nil {
					nui.Button(c.Key("restart"), theme, locales.CaptureRestart(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Warning}).OnClick(v.Host.RestartProxy)
				}
			})
		}
	}
	if v.tab == tabLogs && !settings.Dirty() && !settings.Saving {
		return
	}
	ui.Row(c.Key("footer")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.End).Gap(10).Padding(10, 28).Background(theme.Elevated).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		if v.tab == tabLogs && settings.Dirty() {
			ui.Text(c, locales.SettingsOtherUnsavedNotice()).Grow(1).Basis(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.6).TextColor(theme.Muted)
		}
		reset := nui.Button(c.Key("reset"), theme, locales.SettingsReset(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.RefreshCw(), Disabled: v.committing}).DimDisabled(false).MinWidth(88).Shrink(0)
		if !v.committing {
			reset.OnClick(v.reset)
		}
		disabled := !settings.Dirty()
		save := nui.Button(c.Key("save"), theme, locales.SettingsSave(), nui.ButtonOptions{Leading: lucide.Save(), Disabled: disabled, Loading: settings.Saving || v.committing}).DimDisabled(disabled).MinWidth(88).Shrink(0)
		if !disabled {
			save.OnClick(v.save)
		}
	})
}

// section is a titled group of a settings page. Every section after the first
// of a page starts below a rule.
func (v *View) section(c *ui.Context, theme nui.Theme, key, title string, danger bool, actions func(), body func()) {
	v.sections++
	group := ui.Column(c.Key(key)).FillWidth()
	if v.sections > 1 {
		group.Margin(24, 0, 0, 0).Padding(20, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border)
	}
	group.Children(func() {
		if title != "" || actions != nil {
			ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(16).Margin(0, 0, 12, 0).Children(func() {
				color := theme.Text
				if danger {
					color = theme.Error
				}
				ui.Text(c, title).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.4).Bold().TextColor(color)
				if actions != nil {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(actions)
				}
			})
		}
		ui.Column(c.Key("body")).FillWidth().Gap(8).Children(body)
	})
}

func row(c *ui.Context, theme nui.Theme, key, label, hint string, body func()) {
	line := ui.Row(c.Key(key)).FillWidth().Gap(12).MinHeight(38).PaddingY(2)
	if hint != "" {
		line.AlignItems(ui.Start)
	} else {
		line.AlignItems(ui.Center)
	}
	line.Children(func() {
		name := ui.Text(c, label).Width(170).Shrink(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.4).FontWeight(600).TextColor(theme.Toned)
		if hint != "" {
			name.Margin(6, 0, 0, 0)
		}
		ui.Column(c.Key("control")).Grow(1).Basis(0).MinWidth(0).Gap(6).Children(func() {
			body()
			if hint != "" {
				ui.Text(c.Key("hint"), hint).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.6).TextColor(theme.Muted).FillWidth()
			}
		})
	})
}

func option(value, label string) nui.Option[string] {
	return nui.Option[string]{Value: value, Label: label}
}

// run performs work off the main thread.
func run[T any](v *View, job string, work func() (T, error), done func(T, error)) {
	runtime.Run(v.Data.Runtime.Jobs, "settings:"+job, func(context.Context) (T, error) { return work() }, done)
}
