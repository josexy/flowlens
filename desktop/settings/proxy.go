package settings

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

type proxyState struct {
	info         *settingservice.CACertificateInfo
	trust        *settingservice.CACertificateTrustStatus
	trustFailed  bool
	loading      bool
	changing     bool
	generating   bool
	generatedAt  bool
	sequence     int
	details      bool
	rootsOpen    bool
	certsOpen    bool
	rootMenu     nui.MenuState
	regenerating bool
}

func (v *View) loadCA() {
	state := &v.proxy
	if v.Host.CAInfo == nil {
		return
	}
	state.sequence++
	sequence := state.sequence
	state.loading = true
	type result struct {
		info     *settingservice.CACertificateInfo
		infoErr  error
		trust    *settingservice.CACertificateTrustStatus
		trustErr error
	}
	run(v, "ca", func() (result, error) {
		var out result
		out.info, out.infoErr = v.Host.CAInfo()
		out.trust, out.trustErr = v.Host.CATrust()
		return out, nil
	}, func(out result, _ error) {
		if sequence != state.sequence {
			return
		}
		state.loading = false
		if out.infoErr != nil {
			state.info = nil
			v.Notifier.Error(locales.SettingsErrorLoadCaInfo())
		} else {
			state.info = out.info
		}
		state.trustFailed = out.trustErr != nil || out.trust == nil
		if state.trustFailed {
			v.Notifier.Error(locales.SettingsCaTrustStatusUnavailable())
		} else {
			state.trust = out.trust
		}
	})
}

func (s *proxyState) hasExistingFiles() bool {
	return s.info != nil && (s.info.CertExists || s.info.KeyExists)
}

func (s *proxyState) ready() bool {
	info := s.info
	return info != nil && info.CertExists && info.KeyExists && info.ValidPair && info.IsCA && info.Error == ""
}

func (s *proxyState) trustUnknown() bool {
	return s.loading || s.trustFailed || (s.trust != nil && s.trust.Error != "")
}

func (s *proxyState) hasTrustEntry() bool {
	return s.trust != nil && (s.trust.Present || s.trust.Installed)
}

func (s *proxyState) generationDisabled(builtin, dirty bool) bool {
	return !builtin || dirty || s.generating || s.loading || s.trustFailed || s.changing ||
		(s.trust != nil && s.trust.Supported && (s.hasTrustEntry() || (s.trust.Error != "" && s.trust.SHA256Fingerprint != "")))
}

func (s *proxyState) generationHint(builtin, dirty bool) string {
	switch {
	case !builtin:
		return locales.SettingsCaTrustErrorsCaCustomGenerationDisabled()
	case dirty:
		return locales.SettingsCaSaveFirst()
	case s.loading:
		return locales.SettingsCaTrustLoading()
	case s.trust != nil && s.trust.Supported && s.hasTrustEntry():
		return locales.SettingsCaTrustUninstallFirst()
	case s.generationDisabled(builtin, dirty) && s.trustUnknown():
		return locales.SettingsCaTrustStatusUnavailable()
	case s.hasExistingFiles():
		return locales.SettingsRegenerateCa()
	default:
		return locales.SettingsGenerateCa()
	}
}

func (s *proxyState) trustSummary() (string, *ui.SVG, nui.Color) {
	switch {
	case s.loading:
		return locales.SettingsCaTrustLoading(), lucide.LoaderCircle(), nui.Neutral
	case s.trustUnknown():
		return locales.SettingsCaTrustStatusUnavailable(), lucide.ShieldQuestionMark(), nui.Warning
	case s.hasTrustEntry() && !s.trust.Installed:
		return locales.SettingsCaTrustPartial(), lucide.ShieldAlert(), nui.Warning
	case s.trust != nil && s.trust.Installed:
		return locales.SettingsCaTrustInstalled(), lucide.ShieldCheck(), nui.Success
	default:
		return locales.SettingsCaTrustNotInstalled(), lucide.ShieldMinus(), nui.Neutral
	}
}

func (s *proxyState) statusSummary(builtin bool) string {
	info := s.info
	switch {
	case info == nil:
		return locales.SettingsCaStatusUnavailable()
	case s.ready():
		return locales.SettingsCaStatusReady()
	case builtin:
		return locales.SettingsCaTrustErrorsCaBuiltinUnavailable()
	case !info.CertExists || !info.KeyExists:
		return locales.SettingsCaFilesMissing()
	case info.Error == "" && !info.IsCA:
		return locales.SettingsCaNotCa()
	default:
		return locales.SettingsCaStatusIncomplete()
	}
}

func (v *View) changeTrust() {
	state := &v.proxy
	if state.changing || state.generating || state.loading {
		return
	}
	if v.Data.Settings.Dirty() {
		v.Notifier.Warning(locales.SettingsCaTrustSaveFirst())
		return
	}
	status := state.trust
	if status == nil || !status.Supported || status.Error != "" || state.trustFailed || status.SHA256Fingerprint == "" {
		return
	}
	install := !status.Installed && !status.Present
	change := v.Host.UninstallCA
	if install {
		change = v.Host.InstallCA
	}
	fingerprint := status.SHA256Fingerprint
	state.sequence++
	state.changing = true
	run(v, "ca-trust", func() (struct{}, error) { return struct{}{}, change(fingerprint) }, func(_ struct{}, err error) {
		switch {
		case err == nil && install:
			v.Notifier.Success(locales.SettingsCaTrustInstallSuccess())
		case err == nil:
			v.Notifier.Success(locales.SettingsCaTrustUninstallSuccess())
		case strings.Contains(err.Error(), "ca_trust_canceled"):
		case install:
			v.Notifier.Error(locales.SettingsCaTrustInstallFailed(caErrorMessage(err)))
		default:
			v.Notifier.Error(locales.SettingsCaTrustUninstallFailed(caErrorMessage(err)))
		}
		state.changing = false
		v.loadCA()
	})
}

func (v *View) generateCA(overwrite bool) {
	state := &v.proxy
	draft := v.Data.Settings.Draft
	switch {
	case !draft.ProxyConfig.UseBuiltinCA:
		v.Notifier.Warning(locales.SettingsCaTrustErrorsCaCustomGenerationDisabled())
		return
	case state.generating || state.changing || state.loading:
		return
	case v.Data.Settings.Dirty():
		v.Notifier.Warning(locales.SettingsCaSaveFirst())
		return
	case state.trust != nil && state.trust.Supported && state.hasTrustEntry():
		v.Notifier.Warning(locales.SettingsCaTrustUninstallFirst())
		return
	case state.trustFailed || (state.trust != nil && state.trust.Error != "" && state.trust.SHA256Fingerprint != ""):
		v.Notifier.Warning(locales.SettingsCaTrustStatusUnavailable())
		return
	}
	state.sequence++
	state.generating = true
	run(v, "ca-generate", func() (struct{}, error) { return struct{}{}, v.Host.GenerateCA(overwrite) }, func(_ struct{}, err error) {
		if err != nil {
			v.Notifier.Error(locales.SettingsErrorGenerateCa() + " " + caErrorMessage(err))
		} else {
			state.generatedAt = true
		}
		state.generating = false
		v.loadCA()
	})
}

func (v *View) pickPath(job, title string, patterns []string, done func(string)) {
	run(v, "pick:"+job, func() (string, error) { return v.Host.PickFile(title, patterns...) }, func(path string, err error) {
		if err != nil {
			v.Notifier.Error(locales.SettingsErrorSelectCertificateFile(err.Error()))
			return
		}
		if strings.TrimSpace(path) != "" {
			done(strings.TrimSpace(path))
		}
	})
}

var (
	certificatePatterns = []string{"crt", "cer", "pem"}
	keyPatterns         = []string{"key", "pem"}
)

func (v *View) fileInput(c *ui.Context, palette nui.Theme, key, label, placeholder, title string, patterns []string, target *string) {
	ui.Row(c.Key(key)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c.Key("input")).Grow(1).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c, palette, target, nui.InputOptions{Label: label, Placeholder: placeholder, Size: nui.SM}).FillWidth()
		})
		browse := nui.Button(c.Key("browse"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.FolderOpen(), AccessibleLabel: title})
		browse.OnClick(func() { v.pickPath(key, title, patterns, func(path string) { *target = path }) })
		nui.Tooltip(c, palette, browse, title, nui.TooltipOptions{})
	})
}

func (v *View) upstream(c *ui.Context, palette nui.Theme, proxy *settingservice.ProxyConfig) {
	row(c, palette, "upstream", locales.ToolbarUpstreamProxy(), "", func() {
		value := string(proxy.UpstreamMode)
		ui.Box(c.Key("mode")).Width(320).Children(func() {
			nui.Select(c.Key("select"), palette, &value, []nui.Option[string]{
				option(string(settingservice.UpstreamProxyModeNone), locales.SettingsUpstreamProxyModeNone()),
				option(string(settingservice.UpstreamProxyModeSystem), locales.SettingsUpstreamProxyModeSystem()),
				option(string(settingservice.UpstreamProxyModeCustom), locales.SettingsUpstreamProxyModeCustom()),
			}, nui.SelectOptions{Label: locales.ToolbarUpstreamProxy(), Size: nui.SM})
		})
		if next := settingservice.UpstreamProxyMode(value); next != proxy.UpstreamMode {
			proxy.UpstreamMode = next
			if next != settingservice.UpstreamProxyModeCustom {
				proxy.UpstreamProxy = ""
			}
		}
		if proxy.UpstreamMode == settingservice.UpstreamProxyModeCustom {
			nui.Input(c.Key("url"), palette, &proxy.UpstreamProxy, nui.InputOptions{Label: locales.SettingsUpstreamProxyModeCustom(), Placeholder: locales.SettingsUpstreamProxyCustomPlaceholder(), Size: nui.SM, Invalid: !validUpstream(proxy.UpstreamProxy)}).FillWidth()
		}
	})
}

func (v *View) certificateActions(c *ui.Context, palette nui.Theme, builtin bool) {
	state := &v.proxy
	dirty := v.Data.Settings.Dirty()
	if state.trust != nil && state.trust.Supported {
		hasEntry := state.hasTrustEntry()
		label, icon := locales.SettingsCaTrustInstall(), lucide.ShieldPlus()
		if hasEntry {
			label, icon = locales.SettingsCaTrustUninstall(), lucide.ShieldMinus()
		}
		hint := label
		switch {
		case state.loading:
			hint = locales.SettingsCaTrustLoading()
		case dirty:
			hint = locales.SettingsCaTrustSaveFirst()
		case state.trustUnknown():
			hint = locales.SettingsCaTrustStatusUnavailable()
		}
		disabled := state.trustUnknown() || state.generating || state.changing || dirty || state.trust.SHA256Fingerprint == "" || (!hasEntry && !state.ready())
		button := nui.Button(c.Key("trust"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled, Loading: state.changing})
		if !disabled {
			button.OnClick(v.changeTrust)
		}
		nui.Tooltip(c, palette, button, hint, nui.TooltipOptions{})
	}
	disabled := state.generationDisabled(builtin, dirty)
	label, icon := locales.SettingsGenerateCa(), lucide.FilePlus()
	if state.hasExistingFiles() {
		label, icon = locales.SettingsRegenerateCa(), lucide.RefreshCw()
	}
	button := nui.Button(c.Key("generate"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled, Loading: state.generating})
	if !disabled {
		button.OnClick(func() {
			if state.hasExistingFiles() {
				state.regenerating = true
				v.Confirm.Ask(confirmRequest(locales.SettingsRegenerateCa(), locales.SettingsRegenerateCaConfirm(), locales.SettingsRegenerateCa(), nui.Warning, func() { v.generateCA(true) }))
				return
			}
			v.generateCA(false)
		})
	}
	nui.Tooltip(c, palette, button, state.generationHint(builtin, dirty), nui.TooltipOptions{})
}

func (v *View) certificateDetails(c *ui.Context, palette nui.Theme) {
	state := &v.proxy
	info := state.info
	if info == nil {
		return
	}
	icon, label := lucide.ChevronRight(), locales.SettingsCaShowDetails()
	if state.details {
		icon, label = lucide.ChevronDown(), locales.SettingsCaHideDetails()
	}
	nui.Button(c.Key("details"), palette, label, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Leading: icon}).OnClick(func() { state.details = !state.details })
	if !state.details {
		return
	}
	yesNo := func(value bool) (string, ui.Color) {
		if value {
			return locales.SettingsYes(), palette.Success
		}
		return locales.SettingsNo(), palette.Warning
	}
	ui.Row(c.Key("status-items")).FillWidth().Gap(16).Wrap().Children(func() {
		for index, item := range []struct {
			label string
			value bool
		}{{locales.SettingsCaCertExists(), info.CertExists}, {locales.SettingsCaKeyExists(), info.KeyExists}, {locales.SettingsCaValidPair(), info.ValidPair}, {locales.SettingsCaIsCa(), info.IsCA}} {
			text, color := yesNo(item.value)
			ui.Column(c.Key(index)).Gap(2).Children(func() {
				ui.Text(c, item.label).FontSize(12).TextColor(palette.Muted)
				ui.Text(c, text).FontSize(14).Bold().TextColor(color)
			})
		}
	})
	detail := func(key, label, value string) {
		if value == "" {
			return
		}
		ui.Column(c.Key(key)).FillWidth().Gap(2).Children(func() {
			ui.Text(c, label).FontSize(12).TextColor(palette.Muted)
			ui.Text(c, value).FontSize(14).TextColor(palette.Text).Selectable().FillWidth()
		})
	}
	micros := func(value int64) string {
		if value == 0 {
			return ""
		}
		return logic.FormatUnixMicrosLocal(value)
	}
	detail("subject", locales.SettingsCaSubject(), info.Subject)
	detail("issuer", locales.SettingsCaIssuer(), info.Issuer)
	detail("from", locales.SettingsCaValidFrom(), micros(info.NotBeforeMicros))
	detail("to", locales.SettingsCaValidTo(), micros(info.NotAfterMicros))
	detail("fingerprint", locales.SettingsCaFingerprint(), info.SHA256Fingerprint)
}

func (v *View) certificateSection(c *ui.Context, palette nui.Theme, proxy *settingservice.ProxyConfig) {
	state := &v.proxy
	v.section(c, palette, "certificate", locales.SettingsSectionCertificate(), false, func() { v.certificateActions(c, palette, proxy.UseBuiltinCA) }, func() {
		color := palette.Warning
		icon := lucide.Info()
		if state.ready() {
			color, icon = palette.Success, lucide.CircleCheck()
		} else if state.info == nil {
			color = palette.Muted
		}
		ui.Row(c.Key("summary")).FillWidth().Gap(8).Children(func() {
			ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(color)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, state.statusSummary(proxy.UseBuiltinCA)).FontSize(14).TextColor(color).FillWidth()
				if !proxy.UseBuiltinCA && state.info != nil && state.info.CertExists && state.info.KeyExists && state.info.Error != "" {
					ui.Text(c, state.info.Error).FontSize(12).TextColor(palette.Muted).FillWidth()
				}
				if (state.trust != nil && state.trust.Supported) || state.trustFailed {
					text, trustIcon, tone := state.trustSummary()
					toneColor := palette.Muted
					switch tone {
					case nui.Warning:
						toneColor = palette.Warning
					case nui.Success:
						toneColor = palette.Success
					}
					ui.Row(c.Key("trust")).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, trustIcon).Size(14, 14).TextColor(toneColor)
						ui.Text(c, text).FontSize(14).TextColor(toneColor)
					})
				}
			})
		})
		if state.generatedAt {
			nui.Alert(c.Key("generated"), palette, nui.AlertOptions{Title: locales.SettingsCaGeneratedRestartNotice(), Color: nui.Success, Variant: nui.Soft})
		}
		row(c, palette, "builtin", locales.SettingsCaUseBuiltin(), "", func() {
			nui.Switch(c.Key("switch"), palette, &proxy.UseBuiltinCA, nui.SwitchOptions{Disabled: state.generating || state.changing || state.loading}).Label(locales.SettingsCaUseBuiltin())
		})
		if !proxy.UseBuiltinCA {
			row(c, palette, "ca-cert", locales.ToolbarCaCertPath(), "", func() {
				v.fileInput(c, palette, "ca-cert-file", locales.ToolbarCaCertPath(), "", locales.SettingsSelectCaCertFile(), certificatePatterns, &proxy.CACertPath)
			})
			row(c, palette, "ca-key", locales.ToolbarCaKeyPath(), "", func() {
				v.fileInput(c, palette, "ca-key-file", locales.ToolbarCaKeyPath(), "", locales.SettingsSelectCaKeyFile(), keyPatterns, &proxy.CAKeyPath)
			})
		}
		v.certificateDetails(c, palette)
	})
}

func collapsibleHead(c *ui.Context, palette nui.Theme, key, title, summary string, summaryColor ui.Color, open *bool, actions func()) {
	ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		toggle := ui.Row(c.Key("toggle")).AlignItems(ui.Center).Gap(6).Role(ui.RoleButton).Label(title)
		toggle.OnClick(func() { *open = !*open })
		toggle.Children(func() {
			icon := lucide.ChevronRight()
			if *open {
				icon = lucide.ChevronDown()
			}
			ui.Icon(c, icon).Size(14, 14).TextColor(palette.Muted)
			ui.Text(c, title).FontSize(14).TextColor(palette.Text)
			ui.Text(c, summary).FontSize(14).TextColor(summaryColor)
		})
		actions()
	})
}

func (v *View) rootCAs(c *ui.Context, palette nui.Theme, proxy *settingservice.ProxyConfig) {
	state := &v.proxy
	for _, path := range proxy.RootCAPaths {
		if strings.TrimSpace(path) == "" {
			state.rootsOpen = true
		}
	}
	summary := locales.SettingsNoRootCas()
	if count := len(proxy.RootCAPaths); count > 0 {
		summary = locales.SettingsRootCasCount(strconv.Itoa(count))
	}
	collapsibleHead(c, palette, "roots-head", locales.SettingsSectionRootCas(), summary, palette.Muted, &state.rootsOpen, func() {
		anchor := nui.Button(c.Key("add"), palette, locales.SettingsAddRootCa(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Leading: lucide.Plus(), Trailing: lucide.ChevronDown(), AccessibleLabel: locales.SettingsAddRootCaAction()})
		anchor.OnClick(func() { state.rootMenu.Open = !state.rootMenu.Open })
		nui.DropdownMenu(c.Key("add-menu"), palette, anchor, &state.rootMenu, []nui.MenuItem{
			{ID: "files", Label: locales.SettingsSelectFiles(), OnSelect: func() { v.pickRoots(proxy) }},
			{ID: "manual", Label: locales.SettingsAddPathManually(), OnSelect: func() {
				proxy.RootCAPaths = append(proxy.RootCAPaths, "")
				state.rootsOpen = true
			}},
		}, nui.MenuOptions{Label: locales.SettingsAddRootCaAction()})
	})
	if !state.rootsOpen {
		return
	}
	if len(proxy.RootCAPaths) == 0 {
		ui.Text(c.Key("roots-empty"), locales.SettingsNoRootCasDesc()).FontSize(14).TextColor(palette.Muted).FillWidth()
		return
	}
	for index := range proxy.RootCAPaths {
		number := strconv.Itoa(index + 1)
		ui.Row(c.Key("root-" + number)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, number).Width(20).Shrink(0).FontSize(14).TextColor(palette.Muted)
			ui.Box(c.Key("input")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				v.fileInput(c, palette, "file", locales.SettingsRootCaPathPlaceholder()+" "+number, locales.SettingsRootCaPathPlaceholder(), locales.SettingsSelectRootCaFile(), certificatePatterns, &proxy.RootCAPaths[index])
			})
			position := index
			nui.Button(c.Key("remove"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.SettingsRemoveRootCa() + " " + number}).OnClick(func() {
				next := append([]string(nil), proxy.RootCAPaths[:position]...)
				proxy.RootCAPaths = append(next, proxy.RootCAPaths[position+1:]...)
			})
		})
	}
}

func (v *View) pickRoots(proxy *settingservice.ProxyConfig) {
	run(v, "pick-roots", func() ([]string, error) {
		return v.Host.PickFiles(locales.SettingsSelectRootCaFiles(), certificatePatterns...)
	}, func(paths []string, err error) {
		if err != nil {
			v.Notifier.Error(locales.SettingsErrorSelectCertificateFile(err.Error()))
			return
		}
		existing := map[string]bool{}
		for _, path := range proxy.RootCAPaths {
			if trimmed := strings.TrimSpace(path); trimmed != "" {
				existing[trimmed] = true
			}
		}
		for _, path := range paths {
			if trimmed := strings.TrimSpace(path); trimmed != "" && !existing[trimmed] {
				proxy.RootCAPaths = append(proxy.RootCAPaths, trimmed)
				existing[trimmed] = true
				v.proxy.rootsOpen = true
			}
		}
	})
}

func certificateWarning(cert *settingservice.ClientCertConfig) string {
	if !cert.Enabled {
		return ""
	}
	var missing []string
	if strings.TrimSpace(cert.Hostname) == "" {
		missing = append(missing, locales.SettingsClientCertTargetHost())
	}
	if strings.TrimSpace(cert.CertPath) == "" {
		missing = append(missing, locales.SettingsClientCertFile())
	}
	if strings.TrimSpace(cert.KeyPath) == "" {
		missing = append(missing, locales.SettingsClientKeyFile())
	}
	if len(missing) == 0 {
		return ""
	}
	return locales.SettingsClientCertMissingFields(strings.Join(missing, locales.SettingsFieldSeparator()))
}

func (v *View) clientCerts(c *ui.Context, palette nui.Theme, proxy *settingservice.ProxyConfig) {
	state := &v.proxy
	incomplete := false
	for index := range proxy.ClientCerts {
		incomplete = incomplete || certificateWarning(&proxy.ClientCerts[index]) != ""
	}
	if incomplete {
		state.certsOpen = true
	}
	summary, color := locales.SettingsNoClientCerts(), palette.Muted
	if count := len(proxy.ClientCerts); count > 0 {
		if incomplete {
			summary, color = locales.SettingsClientCertsIncomplete(strconv.Itoa(count)), palette.Warning
		} else {
			summary = locales.SettingsClientCertsCount(strconv.Itoa(count))
		}
	}
	collapsibleHead(c, palette, "certs-head", locales.SettingsSectionClientCerts(), summary, color, &state.certsOpen, func() {
		nui.Button(c.Key("add"), palette, locales.SettingsAddClientCert(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Leading: lucide.Plus()}).OnClick(func() {
			proxy.ClientCerts = append(proxy.ClientCerts, settingservice.ClientCertConfig{Enabled: true})
			state.certsOpen = true
		})
	})
	if !state.certsOpen {
		return
	}
	if len(proxy.ClientCerts) == 0 {
		ui.Text(c.Key("certs-empty"), locales.SettingsNoClientCertsDesc()).FontSize(14).TextColor(palette.Muted).FillWidth()
		return
	}
	for index := range proxy.ClientCerts {
		cert := &proxy.ClientCerts[index]
		position := index
		number := strconv.Itoa(index + 1)
		ui.Column(c.Key("cert-"+number)).FillWidth().Gap(8).Padding(10).Border(1, palette.Border).Radius(palette.Radius).Children(func() {
			title := strings.TrimSpace(cert.Hostname)
			if title == "" {
				title = locales.SettingsClientCertNewTitle(number)
			}
			ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				nui.Switch(c.Key("switch"), palette, &cert.Enabled, nui.SwitchOptions{}).Label(title)
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, title).SingleLine().FontSize(14).TextColor(palette.Text)
					state := locales.SettingsClientCertDisabled()
					if cert.Enabled {
						state = locales.SettingsClientCertEnabled()
					}
					ui.Text(c, state).FontSize(12).TextColor(palette.Muted)
				})
				nui.Button(c.Key("remove"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.SettingsRemoveClientCert() + " " + number}).OnClick(func() {
					next := append([]settingservice.ClientCertConfig(nil), proxy.ClientCerts[:position]...)
					proxy.ClientCerts = append(next, proxy.ClientCerts[position+1:]...)
				})
			})
			row(c, palette, "host", locales.SettingsClientCertTargetHost(), locales.SettingsClientCertTargetHostHint(), func() {
				nui.Input(c.Key("input"), palette, &cert.Hostname, nui.InputOptions{Label: locales.SettingsClientCertTargetHost() + " " + number, Placeholder: locales.SettingsClientCertTargetHostPlaceholder(), Size: nui.SM}).FillWidth()
			})
			row(c, palette, "cert", locales.SettingsClientCertFile(), "", func() {
				v.fileInput(c, palette, "file", locales.SettingsClientCertFile()+" "+number, locales.SettingsClientCertPathPlaceholder(), locales.SettingsSelectClientCertFile(), certificatePatterns, &cert.CertPath)
			})
			row(c, palette, "key", locales.SettingsClientKeyFile(), "", func() {
				v.fileInput(c, palette, "file", locales.SettingsClientKeyFile()+" "+number, locales.SettingsClientKeyPathPlaceholder(), locales.SettingsSelectClientKeyFile(), keyPatterns, &cert.KeyPath)
			})
			if warning := certificateWarning(cert); warning != "" {
				ui.Row(c.Key("warning")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, lucide.Info()).Size(14, 14).Shrink(0).TextColor(palette.Warning)
					ui.Text(c, warning).FontSize(12).TextColor(palette.Warning).FillWidth()
				})
			}
		})
	}
}

func (v *View) proxyPage(c *ui.Context, palette nui.Theme, draft *settingservice.Settings) {
	proxy := draft.ProxyConfig
	v.section(c, palette, "connection", locales.SettingsSectionConnection(), false, nil, func() {
		v.upstream(c, palette, proxy)
		row(c, palette, "attribution", locales.SettingsProcessAttributionLabel(), "", func() {
			nui.Switch(c.Key("switch"), palette, &draft.ProcessAttributionConfig.Enabled, nui.SwitchOptions{}).Label(locales.SettingsProcessAttributionLabel())
		})
	})
	v.certificateSection(c, palette, proxy)
	v.section(c, palette, "upstream-tls", locales.SettingsSectionUpstreamTls(), false, nil, func() {
		v.rootCAs(c, palette, proxy)
		v.clientCerts(c, palette, proxy)
	})
}
