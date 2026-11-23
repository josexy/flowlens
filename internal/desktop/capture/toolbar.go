package capture

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const (
	minProxyPort = 1
	maxProxyPort = 65535
	minDNSTTL    = 1
	maxDNSTTL    = 3600
	localIPsJob  = "capture/local-ips"
)

var addressSchemePattern = regexp.MustCompile(`(?i)^[a-z][a-z\d+\-.]*://`)

type toolbarState struct {
	modeMenu, hostMenu nui.MenuState
	proxyMenu          nui.MenuState
	proxyAction        ui.Handle
	focusProxyAction   bool
	hostOptions        []settingservice.LocalIPAddress

	portOpen    bool
	portWasOpen bool
	portValue   nui.Optional[float64]
	portNumber  nui.NumberState

	dnsOpen     bool
	dnsWasOpen  bool
	dnsLoaded   bool
	dnsLoading  bool
	dnsApplying bool
	dnsClearing bool
	dnsEnabled  bool
	dnsTTL      nui.Optional[float64]
	dnsNumber   nui.NumberState
	dnsError    string

	applying bool
	hosts    hostFilterDialog
}

func (w *Workspace) proxyConfig() *settingservice.ProxyConfig {
	if w.Data.Settings.Current == nil {
		return nil
	}
	return w.Data.Settings.Current.ProxyConfig
}

func proxyScheme(mode settingservice.ProxyMode) string {
	if mode == settingservice.ProxyModeSOCKS5 {
		return "socks5"
	}
	return "http"
}

// ProxyDisplayAddress returns the listening address with a scheme.
func ProxyDisplayAddress(address string, mode settingservice.ProxyMode) string {
	address = strings.TrimSpace(address)
	if address == "" || addressSchemePattern.MatchString(address) {
		return address
	}
	return proxyScheme(mode) + "://" + address
}

func validPort(value nui.Optional[float64]) bool {
	return value.Set && value.Value == float64(int(value.Value)) && value.Value >= minProxyPort && value.Value <= maxProxyPort
}

func validTTL(value nui.Optional[float64]) bool {
	return value.Set && value.Value == float64(int(value.Value)) && value.Value >= minDNSTTL && value.Value <= maxDNSTTL
}

func (w *Workspace) toggleProxy() {
	proxy := w.Data.Proxy
	wasRunning := proxy.Status.Running
	proxy.Toggle(func(_ bool, err error) {
		switch {
		case err != nil:
			w.Notifier.Error(locales.ToolbarOperationFailed(err.Error()))
		case wasRunning:
			w.Notifier.Success(locales.ToolbarStopped())
		default:
			w.Notifier.Success(locales.ToolbarStarted())
		}
	})
}

func (w *Workspace) toggleRecording() {
	proxy := w.Data.Proxy
	proxy.SetRecordingPaused(!proxy.Status.RecordingPaused, func(_ proxyservice.ProxyStatus, err error) {
		if err != nil {
			w.Notifier.Error(locales.ToolbarOperationFailed(err.Error()))
		}
	})
}

func (w *Workspace) toggleSystemProxy() {
	proxy := w.Data.Proxy
	proxy.SetSystemProxyEnabled(!proxy.SystemProxy.Active, func(status proxyservice.SystemProxyStatus, err error) {
		switch {
		case err != nil:
			w.Notifier.Error(locales.ToolbarSystemProxyFailed(err.Error()))
		case status.Active:
			w.Notifier.Success(locales.ToolbarSystemProxyEnabled(status.Address))
		default:
			w.Notifier.Success(locales.ToolbarSystemProxyRestored())
		}
	})
}

func (w *Workspace) clearTraffic() {
	w.Data.Traffic.ClearAll(func(err error) {
		if err != nil {
			w.Notifier.Error(locales.ToolbarClearFailed(err.Error()))
			return
		}
		w.Notifier.Success(locales.ToolbarCleared())
	})
}

// updateProxy saves a proxy change and restarts a running proxy if it needs
// that for the change to take effect.
func (w *Workspace) updateProxy(mutate func(*settingservice.ProxyConfig), done func(error)) {
	finish := func(err error) {
		w.toolbar.applying = false
		if err != nil {
			w.Notifier.Error(locales.ToolbarQuickSettingsFailed(err.Error()))
		}
		if done != nil {
			done(err)
		}
	}
	wasRunning := w.Data.Proxy.Status.Running
	started := w.Data.Settings.UpdateProxyConfig(mutate, func(result *proxyservice.ProxyConfigApplyResult, err error) {
		if err != nil {
			finish(err)
			return
		}
		if wasRunning && result != nil && result.RestartRequired {
			w.Data.Proxy.Restart(finish)
			return
		}
		finish(nil)
	})
	if started {
		w.toolbar.applying = true
	}
}

// Kbds returns the keys of a command for tooltips.
func (w *Workspace) Kbds(commandID string) []string { return w.kbds(commandID) }

func (w *Workspace) kbds(commandID string) []string {
	command, ok := shortcuts.CommandByID(commandID)
	if !ok {
		return nil
	}
	var config *settingservice.ShortcutConfig
	if w.Data.Settings.Current != nil {
		config = w.Data.Settings.Current.Shortcuts
	}
	resolved := shortcuts.ResolveCommand(command, config)
	if resolved.Binding == nil {
		return nil
	}
	return shortcuts.KbdKeys(resolved.Binding, shortcuts.IsMacOS())
}

type toolButton struct {
	key      string
	icon     *ui.SVG
	tooltip  string
	label    string
	active   bool
	disabled bool
	kbds     []string
	run      func()
	// quick gives proxy toolbar settings compact square padding.
	quick bool
	// color is the button's color when it is not active, Neutral by default.
	color nui.Color
}

// quickButtonPadding is the padding of the proxy toolbar's settings buttons.
var quickButtonPadding = nui.Styles{nui.Root: {PaddingX: nui.Set[float32](4), PaddingY: nui.Set[float32](4)}}

func toolbarButton(c *ui.Context, theme nui.Theme, b toolButton) ui.Element {
	color := b.color
	if color == nui.ColorDefault {
		color = nui.Neutral
	}
	if b.active {
		color = nui.Primary
	}
	label := b.label
	if label == "" {
		label = b.tooltip
	}
	options := nui.ButtonOptions{
		Variant: nui.Ghost, Color: color, Size: nui.SM, Square: true, Leading: b.icon,
		AccessibleLabel: label, Disabled: b.disabled,
	}
	if b.quick {
		options.UI = quickButtonPadding
	}
	button := nui.Button(c.Key(b.key), theme, "", options)
	if b.run != nil && !b.disabled {
		button.OnClick(b.run)
	}
	nui.Tooltip(c, theme, button, b.tooltip, nui.TooltipOptions{Kbds: b.kbds})
	return button
}

func (w *Workspace) booleanButton(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, label, on, off string, value bool, set func(*settingservice.ProxyConfig, bool)) {
	tooltip := off
	if value {
		tooltip = on
	}
	toolbarButton(c, theme, toolButton{
		key: key, icon: icon, tooltip: tooltip, label: label, active: value, quick: true,
		run: func() {
			if w.toolbar.applying {
				return
			}
			w.updateProxy(func(config *settingservice.ProxyConfig) { set(config, !value) }, nil)
		},
	})
}

func (w *Workspace) buildToolbar(c *ui.Context, theme nui.Theme) {
	config := w.proxyConfig()
	row := ui.Row(c).FillWidth().Shrink(0).MinHeight(36).AlignItems(ui.Center).Gap(8).PaddingX(8).PaddingY(4).Background(theme.MutedBackground)
	row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	row.Children(func() {
		panel := ui.Row(c).Grow(1).Basis(0).MinWidth(0).MinHeight(28).AlignItems(ui.Center).Gap(8).PaddingX(8).PaddingY(3).Background(theme.Background).Radius(4)
		panel.Border(1, theme.Border).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05)).Children(func() {
			w.statusText(c, theme, config)
			ui.Row(c).AlignItems(ui.Center).Gap(4).Shrink(0).Children(func() {
				if config != nil {
					w.quickSettings(c, theme, config)
				}
			})
		})
		w.startStopButtons(c, theme)
	})
	if config != nil {
		w.toolbar.hosts.build(c, theme, w, config)
	}
}

func (w *Workspace) statusText(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	proxy := w.Data.Proxy
	mode := settingservice.ProxyModeHTTP
	if config != nil {
		mode = config.Mode
	}
	address := ProxyDisplayAddress(proxy.Status.Address, mode)
	ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(8).Clip().Children(func() {
		color := theme.Error
		if proxy.Status.Running {
			color = theme.Success
		}
		ui.Box(c).Size(7, 7).Radius(4).Background(color).Shrink(0)
		text := locales.ToolbarProxyNotRunning()
		if proxy.Status.Running {
			text = locales.ToolbarProxyRunning(address)
		}
		ui.Text(c, text).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Toned)
		if proxy.Status.Running && address != "" {
			nui.Button(c.Key("copy-address"), theme, "", nui.ButtonOptions{
				Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true,
				Leading: lucide.Copy(), AccessibleLabel: locales.ToolbarCopied(),
			}).OnClick(func() {
				w.Table.clipboard = &address
				w.Notifier.Success(locales.ToolbarCopied())
			})
		}
	})
}

func (w *Workspace) startStopButtons(c *ui.Context, theme nui.Theme) {
	proxy := w.Data.Proxy
	ui.Row(c).AlignItems(ui.Center).Gap(8).Shrink(0).Children(func() {
		arrowWidth := apptheme.TextSize(c, 32)
		var controlWidth float32
		for index, text := range []string{locales.ToolbarStart(), locales.ToolbarPauseRecording(), locales.ToolbarResumeRecording()} {
			width, _ := c.MeasureText(0, ui.Span{Text: text, Font: theme.Font, Size: apptheme.TextSize(c, 14), Weight: 500})
			if index > 0 {
				width += arrowWidth - 1 // FieldGroup overlaps adjacent borders.
			}
			controlWidth = max(controlWidth, width)
		}
		// Match nui.MD's icon, horizontal padding and gap, including outline borders.
		density := theme.Density
		if density <= 0 {
			density = 1
		}
		controlWidth += apptheme.TextSize(c, 20) + 26*density + 2
		busy := proxy.Toggling || w.toolbar.applying
		// Quick-setting saves block conflicting actions without dimming the
		// group or its buttons; only a proxy operation changes their busy look.
		label, color, variant, icon := locales.ToolbarStart(), nui.Primary, nui.Solid, lucide.Power()
		run := w.toggleProxy
		if proxy.Status.Running {
			run = w.toggleRecording
			label, color, variant, icon = locales.ToolbarPauseRecording(), nui.Neutral, nui.Outline, lucide.Pause()
			if proxy.Status.RecordingPaused {
				label, color, variant, icon = locales.ToolbarResumeRecording(), nui.Primary, nui.Solid, lucide.Play()
			}
		} else {
			w.toolbar.proxyMenu.Open = false
		}
		var menuAnchor ui.Element
		items := []nui.FieldGroupItem{{ID: "primary", Build: func(c *ui.Context, palette nui.Theme) ui.Element {
			button := nui.Button(c, palette, label, nui.ButtonOptions{
				Color: color, Variant: variant, Size: nui.MD, Leading: icon, Loading: proxy.Toggling, Disabled: busy,
			}).DimDisabled(proxy.Toggling).Grow(1).Basis(0).MinWidth(0).Justify(ui.Center).Bind(&w.toolbar.proxyAction)
			if !busy {
				button.OnClick(run)
				if w.toolbar.focusProxyAction {
					w.toolbar.focusProxyAction = false
					w.toolbar.proxyAction.Focus(c)
				}
			}
			return button
		}}}
		if proxy.Status.Running {
			items = append(items, nui.FieldGroupItem{ID: "menu", Build: func(c *ui.Context, palette nui.Theme) ui.Element {
				menuAnchor = nui.Button(c, palette, "", nui.ButtonOptions{
					Color: color, Variant: variant, Leading: lucide.ChevronDown(), Square: true,
					AccessibleLabel: locales.ToolbarProxyActions(), Disabled: busy,
				}).DimDisabled(proxy.Toggling).Width(arrowWidth).Shrink(0)
				if variant == nui.Solid {
					menuAnchor.BorderWidth(0, 0, 0, 1).BorderColor(theme.Background.Alpha(0.3))
				}
				return menuAnchor
			}})
		}
		// Each button owns its disabled opacity; dimming the group would apply it twice.
		nui.FieldGroup(c.Key("proxy-control"), theme, nui.FieldGroupOptions{Disabled: busy}, items).
			DimDisabled(false).Width(controlWidth).Shrink(0)
		if proxy.Status.Running {
			nui.DropdownMenu(c.Key("proxy-menu"), theme, menuAnchor, &w.toolbar.proxyMenu, []nui.MenuItem{{
				ID: "stop-proxy", Label: locales.ToolbarStop(), Icon: lucide.Power(), Color: nui.Error,
				Disabled: busy,
				// The trigger disappears after stopping; restore focus to the
				// primary action without queueing focus on the hidden arrow.
				Close: nui.Set(false),
				OnSelect: func() {
					w.toolbar.proxyMenu.Open = false
					w.toolbar.focusProxyAction = true
					w.toggleProxy()
				},
			}}, nui.MenuOptions{Label: locales.ToolbarProxyActions(), Disabled: busy}).MinWidth(controlWidth)
		}
		toolbarButton(c, theme, toolButton{
			key: "clear", icon: lucide.Trash(), tooltip: locales.ToolbarClear(), color: nui.Error,
			disabled: !w.captureTabActive() || len(w.Data.Traffic.Entries) == 0, kbds: w.kbds(shortcuts.CaptureClearTraffic), run: w.clearTraffic,
		})
	})
}

// captureTabActive reports whether the live capture tab is on show.
func (w *Workspace) captureTabActive() bool {
	return w.Data.Workspace == nil || w.Data.Workspace.ActiveTab().Type == logic.TabTypeCapture
}

func (w *Workspace) quickSettings(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	proxy := w.Data.Proxy
	w.modeMenu(c, theme, config)
	w.hostMenu(c, theme, config)
	w.portPopover(c, theme, config)
	if proxy.SystemProxy.Supported {
		w.systemProxyButton(c, theme, config)
	}
	w.booleanButton(c, theme, "disable-proxy", pick(config.DisableProxy, lucide.Unplug, lucide.Plug), locales.ToolbarDisableProxy(), locales.ToolbarDisableProxyOn(), locales.ToolbarDisableProxyOff(), config.DisableProxy,
		func(target *settingservice.ProxyConfig, value bool) { target.DisableProxy = value })
	w.booleanButton(c, theme, "disable-http2", lucide.Heading2(), locales.ToolbarDisableHttp2(), locales.ToolbarDisableHttp2On(), locales.ToolbarDisableHttp2Off(), config.DisableHTTP2,
		func(target *settingservice.ProxyConfig, value bool) { target.DisableHTTP2 = value })
	w.booleanButton(c, theme, "anti-cache", pick(config.AntiCache, lucide.DatabaseZap, lucide.Database), locales.ToolbarAntiCache(), locales.ToolbarAntiCacheOn(), locales.ToolbarAntiCacheOff(), config.AntiCache,
		func(target *settingservice.ProxyConfig, value bool) { target.AntiCache = value })
	w.dnsPopover(c, theme)
	w.booleanButton(c, theme, "anti-comp", pick(config.AntiComp, lucide.File, lucide.FileArchive), locales.ToolbarAntiComp(), locales.ToolbarAntiCompOn(), locales.ToolbarAntiCompOff(), config.AntiComp,
		func(target *settingservice.ProxyConfig, value bool) { target.AntiComp = value })
	w.booleanButton(c, theme, "skip-tls", pick(config.SkipVerifyTLS, lucide.ShieldOff, lucide.ShieldCheck), locales.ToolbarSkipVerifyTls(), locales.ToolbarSkipVerifyTlsOn(), locales.ToolbarSkipVerifyTlsOff(), config.SkipVerifyTLS,
		func(target *settingservice.ProxyConfig, value bool) { target.SkipVerifyTLS = value })
	count := len(config.IncludeHosts) + len(config.ExcludeHosts)
	toolbarButton(c, theme, toolButton{
		key: "host-filter", icon: lucide.ListFilter(), tooltip: locales.ToolbarHostFilterTooltip(strconv.Itoa(count)), active: count > 0, quick: true,
		run: func() { w.toolbar.hosts.show(config) },
	})
}

func (w *Workspace) systemProxyButton(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	status := w.Data.Proxy.SystemProxy
	tooltip := locales.ToolbarSystemProxyOff()
	switch {
	case status.Active:
		tooltip = locales.ToolbarSystemProxyOn(status.Address)
	case !status.ModeSupported:
		tooltip = locales.ToolbarSystemProxyModeUnsupported(strings.ToUpper(string(config.Mode)))
	}
	toolbarButton(c, theme, toolButton{
		key: "system-proxy", icon: lucide.MonitorUp(), tooltip: tooltip, active: status.Active, quick: true,
		disabled: !status.ModeSupported && !status.Active, kbds: w.kbds(shortcuts.CaptureToggleSystemProxy), run: w.toggleSystemProxy,
	})
}

func (w *Workspace) modeMenu(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	tooltip := locales.ToolbarQuickMode(strings.ToUpper(string(config.Mode)))
	anchor := toolbarButton(c, theme, toolButton{key: "mode", icon: lucide.Network(), tooltip: tooltip, quick: true})
	item := func(label string, mode settingservice.ProxyMode) nui.MenuItem {
		return nui.MenuItem{ID: string(mode), Label: label, Radio: true, Checked: nui.Set(config.Mode == mode), OnSelect: func() {
			if mode != config.Mode {
				w.updateProxy(func(target *settingservice.ProxyConfig) { target.Mode = mode }, nil)
			}
		}}
	}
	nui.DropdownMenu(c.Key("mode-menu"), theme, anchor, &w.toolbar.modeMenu, []nui.MenuItem{
		item("HTTP", settingservice.ProxyModeHTTP), item("SOCKS5", settingservice.ProxyModeSOCKS5),
	}, nui.MenuOptions{Label: tooltip})
}

func (w *Workspace) refreshLocalIPs() {
	list := w.Host.ListLocalIPs
	jobs := w.Data.Runtime.Jobs
	if list == nil || jobs.Active(localIPsJob) {
		return
	}
	runtime.Run(jobs, localIPsJob, func(context.Context) ([]settingservice.LocalIPAddress, error) {
		return list(), nil
	}, func(options []settingservice.LocalIPAddress, err error) {
		if err == nil {
			w.toolbar.hostOptions = options
		}
	})
}

func (w *Workspace) localIPs() []settingservice.LocalIPAddress {
	if len(w.toolbar.hostOptions) > 0 {
		return w.toolbar.hostOptions
	}
	return []settingservice.LocalIPAddress{{Label: "ALL", Value: "0.0.0.0"}, {Label: "127.0.0.1", Value: "127.0.0.1"}}
}

func (w *Workspace) hostMenu(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	tooltip := locales.ToolbarQuickHost(config.Host)
	anchor := toolbarButton(c, theme, toolButton{key: "host", icon: lucide.Router(), tooltip: tooltip, quick: true})
	var items []nui.MenuItem
	if w.toolbar.hostMenu.Open {
		for _, option := range w.localIPs() {
			value := option.Value
			items = append(items, nui.MenuItem{ID: value, Label: option.Label, Radio: true, Checked: nui.Set(config.Host == value), OnSelect: func() {
				if value != config.Host {
					w.updateProxy(func(target *settingservice.ProxyConfig) { target.Host = value }, nil)
				}
			}})
		}
	}
	nui.DropdownMenu(c.Key("host-menu"), theme, anchor, &w.toolbar.hostMenu, items, nui.MenuOptions{
		Label: tooltip,
		OnOpenChange: func(open bool) {
			if open {
				w.refreshLocalIPs()
			}
		},
	})
}

func (w *Workspace) portPopover(c *ui.Context, theme nui.Theme, config *settingservice.ProxyConfig) {
	state := &w.toolbar
	saving := state.applying || w.Data.Settings.Saving
	tooltip := locales.ToolbarQuickPort(strconv.Itoa(config.Port))
	anchor := toolbarButton(c, theme, toolButton{
		key: "port", icon: lucide.Hash(), tooltip: tooltip, quick: true,
		run: func() {
			if !saving {
				state.portOpen = !state.portOpen
			}
		},
	})
	if state.portOpen && !state.portWasOpen {
		state.portValue = nui.Set(float64(config.Port))
		state.portNumber = nui.NumberState{}
	}
	state.portWasOpen = state.portOpen
	nui.Popover(c.Key("port-popover"), theme, anchor, &state.portOpen, nui.PopoverOptions{Label: locales.ToolbarPort(), Width: nui.Set[float32](280), Dismissible: nui.Set(!saving)}, func(palette nui.Theme) {
		valid := validPort(state.portValue)
		problem := ""
		if !valid {
			problem = locales.ToolbarInvalidPort()
		}
		nui.FormField(c, palette, nui.FormFieldOptions{Label: locales.ToolbarPort(), Error: problem, Disabled: saving}, func(field nui.Theme) {
			nui.InputNumber(c.Key("port-number"), field, &state.portValue, &state.portNumber, nui.InputNumberOptions{
				Label: locales.ToolbarPort(), Disabled: saving,
				Orientation: nui.Vertical,
				Step:        1, AllowInvalid: true,
			}).DimDisabled(false)
		}).DimDisabled(false)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			cancel := nui.Button(c.Key("port-cancel"), palette, locales.ToolbarCancel(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Disabled: saving}).DimDisabled(false)
			if !saving {
				cancel.OnClick(func() { state.portOpen = false })
			}
			save := nui.Button(c.Key("port-save"), palette, locales.ToolbarSave(), nui.ButtonOptions{Disabled: saving || !valid})
			if !saving && valid {
				save.OnClick(func() { w.savePort(config, int(state.portValue.Value)) })
			}
		})
	})
}

func (w *Workspace) savePort(config *settingservice.ProxyConfig, port int) {
	state := &w.toolbar
	if port == config.Port {
		state.portOpen = false
		return
	}
	w.updateProxy(func(target *settingservice.ProxyConfig) { target.Port = port }, func(error) { state.portOpen = false })
}

func (w *Workspace) dnsPopover(c *ui.Context, theme nui.Theme) {
	state := &w.toolbar
	proxy := w.Data.Proxy
	busy := state.dnsApplying || state.dnsClearing || state.dnsLoading || w.Data.Settings.Saving
	tooltip := locales.ToolbarDnsCacheOff()
	if proxy.DNSCache.Enabled {
		tooltip = locales.ToolbarDnsCacheOn()
	}
	anchor := toolbarButton(c, theme, toolButton{
		key: "dns", icon: lucide.Globe(), tooltip: tooltip, label: locales.ToolbarDnsCache(), active: proxy.DNSCache.Enabled, quick: true,
		run: func() {
			if !state.dnsApplying && !state.dnsClearing {
				state.dnsOpen = !state.dnsOpen
			}
		},
	})
	if state.dnsOpen && !state.dnsWasOpen {
		state.dnsLoading, state.dnsLoaded, state.dnsError = true, false, ""
		state.dnsNumber = nui.NumberState{}
		proxy.RefreshDNSCache(func(status proxyservice.DNSCacheStatus) {
			state.dnsLoading, state.dnsLoaded = false, true
			state.dnsEnabled, state.dnsTTL = status.Enabled, nui.Set(float64(status.TTLSeconds))
		})
	}
	state.dnsWasOpen = state.dnsOpen
	nui.Popover(c.Key("dns-popover"), theme, anchor, &state.dnsOpen, nui.PopoverOptions{Label: locales.ToolbarDnsCache(), Width: nui.Set[float32](280), Dismissible: nui.Set(!state.dnsApplying && !state.dnsClearing)}, func(palette nui.Theme) {
		valid := validTTL(state.dnsTTL)
		problem := ""
		if !valid {
			problem = locales.ToolbarDnsCacheInvalidTtl()
		}
		nui.Switch(c.Key("dns-enabled"), palette, &state.dnsEnabled, nui.SwitchOptions{Label: locales.ToolbarDnsCacheEnabled(), Disabled: busy}).DimDisabled(!state.dnsLoaded)
		nui.FormField(c, palette, nui.FormFieldOptions{Label: locales.ToolbarDnsCacheTtl(), Error: problem, Disabled: busy}, func(field nui.Theme) {
			nui.InputNumber(c.Key("dns-ttl"), field, &state.dnsTTL, &state.dnsNumber, nui.InputNumberOptions{
				Label: locales.ToolbarDnsCacheTtl(), Disabled: busy,
				Orientation: nui.Vertical,
				Step:        1, AllowInvalid: true,
			}).DimDisabled(!state.dnsLoaded)
		}).DimDisabled(!state.dnsLoaded)
		if state.dnsError != "" {
			ui.Text(c, state.dnsError).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Error)
		}
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			clear := nui.Button(c.Key("dns-clear"), palette, locales.ToolbarDnsCacheClear(), nui.ButtonOptions{Color: nui.Neutral, Variant: nui.Outline, Disabled: busy}).DimDisabled(state.dnsClearing || !state.dnsLoaded)
			if !busy {
				clear.OnClick(w.clearDNS)
			}
			canApply := !busy && state.dnsLoaded && valid
			apply := nui.Button(c.Key("dns-apply"), palette, locales.ToolbarDnsCacheApply(), nui.ButtonOptions{Disabled: !canApply}).DimDisabled(state.dnsApplying || !state.dnsLoaded || !valid)
			if canApply {
				apply.OnClick(w.applyDNS)
			}
		})
	})
}

func (w *Workspace) clearDNS() {
	state := &w.toolbar
	state.dnsClearing, state.dnsError = true, ""
	w.Data.Proxy.ClearDNSCache(func() {
		state.dnsClearing = false
		w.Notifier.Success(locales.ToolbarDnsCacheCleared())
	})
}

func (w *Workspace) applyDNS() {
	state := &w.toolbar
	enabled, ttl := state.dnsEnabled, int(state.dnsTTL.Value)
	state.dnsApplying, state.dnsError = true, ""
	if !w.Data.Settings.UpdateProxyConfig(func(target *settingservice.ProxyConfig) {
		target.DNSCacheEnabled, target.DNSCacheTTLSeconds = enabled, ttl
	}, func(_ *proxyservice.ProxyConfigApplyResult, err error) {
		state.dnsApplying = false
		if err != nil {
			// The draft stays for another try; the icon shows the backend's state.
			state.dnsError = locales.ToolbarDnsCacheFailed(err.Error())
			w.Data.Proxy.RefreshDNSCache(nil)
			return
		}
		w.Data.Proxy.RefreshDNSCache(nil)
		state.dnsOpen = false
	}) {
		state.dnsApplying = false
	}
}

// pick returns on's icon when active and off's otherwise.
func pick(active bool, on, off func() *ui.SVG) *ui.SVG {
	if active {
		return on()
	}
	return off()
}
