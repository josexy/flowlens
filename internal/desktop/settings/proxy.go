package settings

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
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

func (s *proxyState) hasTrustEntry() bool {
	return s.trust != nil && (s.trust.Present || s.trust.Installed)
}

// CA actions target the saved capture certificate configuration.
func (v *View) caConfigDirty() bool {
	settings := v.Data.Settings
	if settings.Draft == nil || settings.Current == nil {
		return false
	}
	draft, saved := settings.Draft.ProxyConfig, settings.Current.ProxyConfig
	return draft.UseBuiltinCA != saved.UseBuiltinCA || draft.CACertPath != saved.CACertPath || draft.CAKeyPath != saved.CAKeyPath
}

// caState derives the certificate panel's state for the draft's CA mode.
func (v *View) caState(builtin, caDirty bool) logic.CATrustState {
	s := &v.proxy
	return logic.DeriveCATrustState(logic.CATrustInput{
		UseBuiltinCA:     builtin,
		Info:             s.info,
		Trust:            s.trust,
		TrustLoading:     s.loading,
		TrustLoadFailed:  s.trustFailed,
		ChangingTrust:    s.changing,
		SettingsDirty:    caDirty,
		HasExistingFiles: s.hasExistingFiles(),
		Generating:       s.generating,
	})
}

func caActionHintText(hint logic.CAActionHint, action string) string {
	switch hint {
	case logic.CAActionHintLoading:
		return locales.SettingsCaTrustLoading()
	case logic.CAActionHintSaveFirst:
		return locales.SettingsCaActionsSaveFirst()
	case logic.CAActionHintUnavailable:
		return locales.SettingsCaTrustStatusUnavailable()
	default:
		return action
	}
}

func caGenerationHintText(hint logic.CAGenerationHint) string {
	switch hint {
	case logic.CAHintCustomDisabled:
		return locales.SettingsCaTrustErrorsCaCustomGenerationDisabled()
	case logic.CAHintSaveFirst:
		return locales.SettingsCaActionsSaveFirst()
	case logic.CAHintLoading:
		return locales.SettingsCaTrustLoading()
	case logic.CAHintUninstallFirst:
		return locales.SettingsCaTrustUninstallFirst()
	case logic.CAHintStatusUnavailable:
		return locales.SettingsCaTrustStatusUnavailable()
	case logic.CAHintRegenerate:
		return locales.SettingsRegenerateCa()
	default:
		return locales.SettingsGenerateCa()
	}
}

func caTrustSummary(summary logic.CATrustSummary) (string, *ui.SVG) {
	switch summary {
	case logic.CATrustLoading:
		return locales.SettingsCaTrustLoading(), lucide.LoaderCircle()
	case logic.CATrustUnavailable:
		return locales.SettingsCaTrustStatusUnavailable(), lucide.ShieldQuestionMark()
	case logic.CATrustPartial:
		return locales.SettingsCaTrustPartial(), lucide.ShieldAlert()
	case logic.CATrustInstalled:
		return locales.SettingsCaTrustInstalled(), lucide.ShieldCheck()
	default:
		return locales.SettingsCaTrustNotInstalled(), lucide.ShieldMinus()
	}
}

func caStatusText(status logic.CAStatus) string {
	switch status {
	case logic.CAStatusReady:
		return locales.SettingsCaStatusReady()
	case logic.CAStatusBuiltinUnavailable:
		return locales.SettingsCaTrustErrorsCaBuiltinUnavailable()
	case logic.CAStatusFilesMissing:
		return locales.SettingsCaFilesMissing()
	case logic.CAStatusNotCA:
		return locales.SettingsCaNotCa()
	case logic.CAStatusIncomplete:
		return locales.SettingsCaStatusIncomplete()
	default:
		return locales.SettingsCaStatusUnavailable()
	}
}

func caPanelHintText(hint logic.CAPanelHint) string {
	switch hint {
	case logic.CAPanelHintSaveFirst:
		return locales.SettingsCaActionsSaveFirst()
	case logic.CAPanelHintUninstallFirst:
		return locales.SettingsCaTrustUninstallFirst()
	default:
		return ""
	}
}

func (v *View) changeTrust() {
	state := &v.proxy
	if v.committing || state.changing || state.generating || state.loading {
		return
	}
	if v.caConfigDirty() {
		v.Notifier.Warning(locales.SettingsCaActionsSaveFirst())
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
	case v.committing || state.generating || state.changing || state.loading:
		return
	case v.caConfigDirty():
		v.Notifier.Warning(locales.SettingsCaActionsSaveFirst())
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

func (v *View) pickPath(job string, picker FilePicker, done func(string)) {
	run(v, "pick:"+job, func() (string, error) { return v.Host.PickFile(picker) }, func(path string, err error) {
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

// certificatePicker and keyPicker describe the dialogs that choose
// certificate and private key files, as 1.0.5's.
func certificatePicker(title, message string) FilePicker {
	return FilePicker{Title: title, Message: message, FilterName: locales.SettingsCertificateFileFilter(), Patterns: certificatePatterns}
}

func keyPicker(title, message string) FilePicker {
	return FilePicker{Title: title, Message: message, FilterName: locales.SettingsPrivateKeyFileFilter(), Patterns: keyPatterns}
}

// fileInput is a path field with a browse button. buttonTitle names the
// button; "" uses the generic Select File, as 1.0.5 did for root CA and
// client certificate paths. picker describes the file dialog.
func (v *View) fileInput(c *ui.Context, palette nui.Theme, key, label, placeholder, buttonTitle string, picker FilePicker, target *string) {
	if buttonTitle == "" {
		buttonTitle = locales.SettingsSelectFile()
	}
	ui.Row(c.Key(key)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c.Key("input")).Grow(1).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c, palette, target, nui.InputOptions{Label: label, Placeholder: placeholder}).FillWidth()
		})
		browse := nui.Button(c.Key("browse"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Square: true, Leading: lucide.FolderOpen(), AccessibleLabel: picker.Title})
		browse.OnClick(func() { v.pickPath(key, picker, func(path string) { *target = path }) })
		nui.Tooltip(c, palette, browse, buttonTitle, nui.TooltipOptions{})
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
			}, nui.SelectOptions{Label: locales.ToolbarUpstreamProxy()})
		})
		if next := settingservice.UpstreamProxyMode(value); next != proxy.UpstreamMode {
			proxy.UpstreamMode = next
			if next != settingservice.UpstreamProxyModeCustom {
				proxy.UpstreamProxy = ""
			}
		}
		if proxy.UpstreamMode == settingservice.UpstreamProxyModeCustom {
			nui.Input(c.Key("url"), palette, &proxy.UpstreamProxy, nui.InputOptions{Label: locales.SettingsUpstreamProxyModeCustom(), Placeholder: locales.SettingsUpstreamProxyCustomPlaceholder(), Invalid: !validUpstream(proxy.UpstreamProxy)}).FillWidth()
		}
	})
}

func (v *View) certificateActions(c *ui.Context, palette nui.Theme, ca logic.CATrustState) {
	state := &v.proxy
	if ca.TrustActions {
		label, icon := locales.SettingsCaTrustInstall(), lucide.ShieldPlus()
		if ca.Action == logic.CATrustUninstall {
			label, icon = locales.SettingsCaTrustUninstall(), lucide.ShieldMinus()
		}
		disabled := v.committing || ca.ActionDisabled
		button := nui.Button(c.Key("trust"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled, Loading: state.changing})
		if !disabled {
			button.OnClick(v.changeTrust)
		}
		nui.Tooltip(c, palette, button, caActionHintText(ca.ActionHint, label), nui.TooltipOptions{})
	}
	disabled := v.committing || ca.GenerationDisabled
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
	nui.Tooltip(c, palette, button, caGenerationHintText(ca.GenerationHint), nui.TooltipOptions{})
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
	nui.Button(c.Key("details"), palette, label, nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Leading: icon}).AlignSelf(ui.Start).OnClick(func() { state.details = !state.details })
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
				ui.Text(c, item.label).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
				ui.Text(c, text).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(color)
			})
		}
	})
	detail := func(key, label, value string) {
		if value == "" {
			return
		}
		ui.Column(c.Key(key)).FillWidth().Gap(2).Children(func() {
			ui.Text(c, label).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
			ui.Text(c, value).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text).Selectable().FillWidth()
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
	ca := v.caState(proxy.UseBuiltinCA, v.caConfigDirty())
	v.section(c, palette, "certificate", locales.SettingsSectionCertificate(), false, func() { v.certificateActions(c, palette, ca) }, func() {
		color := palette.Warning
		icon := lucide.Info()
		switch ca.Status {
		case logic.CAStatusReady:
			color, icon = palette.Success, lucide.CircleCheck()
		case logic.CAStatusUnavailable:
			color = palette.Muted
		}
		ui.Row(c.Key("summary")).FillWidth().Wrap().Gap(12).AlignItems(ui.Center).Children(func() {
			status := func(key, text string, icon *ui.SVG, color ui.Color) {
				ui.Row(c.Key(key)).Gap(6).MinWidth(0).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(color)
					ui.Text(c, text).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(color)
				})
			}
			status("files", caStatusText(ca.Status), icon, color)
			if ca.ShowSummary {
				text, trustIcon := caTrustSummary(ca.Summary)
				toneColor := palette.Muted
				switch ca.SummaryTone {
				case logic.CAToneWarning:
					toneColor = palette.Warning
				case logic.CAToneSuccess:
					toneColor = palette.Success
				}
				status("trust", text, trustIcon, toneColor)
			}
		})
		if hint := caPanelHintText(ca.PanelHint); hint != "" {
			ui.Text(c.Key("action-hint"), hint).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted).FillWidth()
		}
		if !proxy.UseBuiltinCA && state.info != nil && state.info.CertExists && state.info.KeyExists && state.info.Error != "" {
			ui.Text(c.Key("error"), state.info.Error).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted).FillWidth()
		}
		if state.generatedAt {
			nui.Alert(c.Key("generated"), palette, nui.AlertOptions{Title: locales.SettingsCaGeneratedRestartNotice(), Color: nui.Success, Variant: nui.Soft})
		}
		row(c, palette, "builtin", locales.SettingsCaUseBuiltin(), "", func() {
			nui.Switch(c.Key("switch"), palette, &proxy.UseBuiltinCA, nui.SwitchOptions{Disabled: state.generating || state.changing || state.loading}).DimDisabled(false).Label(locales.SettingsCaUseBuiltin())
		})
		if !proxy.UseBuiltinCA {
			row(c, palette, "ca-cert", locales.ToolbarCaCertPath(), "", func() {
				v.fileInput(c, palette, "ca-cert-file", locales.ToolbarCaCertPath(), "", locales.SettingsSelectCaCertFile(), certificatePicker(locales.SettingsSelectCaCertFile(), locales.SettingsSelectCaCertFileMessage()), &proxy.CACertPath)
			})
			row(c, palette, "ca-key", locales.ToolbarCaKeyPath(), "", func() {
				v.fileInput(c, palette, "ca-key-file", locales.ToolbarCaKeyPath(), "", locales.SettingsSelectCaKeyFile(), keyPicker(locales.SettingsSelectCaKeyFile(), locales.SettingsSelectCaKeyFileMessage()), &proxy.CAKeyPath)
			})
		}
		v.certificateDetails(c, palette)
	})
}

func collapsibleHead(c *ui.Context, palette nui.Theme, key, title, summary string, summaryColor ui.Color, open *bool, actions func()) {
	ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		toggle := ui.CollapsibleBase(c.Key("toggle"), open).Trigger
		toggle.AlignItems(ui.Center).Gap(6).FocusRing(true).Label(title).Description(summary)
		toggle.Children(func() {
			icon := lucide.ChevronRight()
			if *open {
				icon = lucide.ChevronDown()
			}
			ui.Icon(c, icon).Size(14, 14).TextColor(palette.Muted)
			ui.Text(c, title).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text)
			ui.Text(c, summary).FontSize(apptheme.TextSize(c, 14)).TextColor(summaryColor)
		})
		actions()
	})
}

func (v *View) rootCAs(c *ui.Context, palette nui.Theme, proxy *settingservice.ProxyConfig) {
	state := &v.proxy
	summary := locales.SettingsNoRootCas()
	if count := len(proxy.RootCAPaths); count > 0 {
		summary = locales.SettingsRootCasCount(strconv.Itoa(count))
	}
	collapsibleHead(c, palette, "roots-head", locales.SettingsSectionRootCas(), summary, palette.Muted, &state.rootsOpen, func() {
		anchor := nui.Button(c.Key("add"), palette, locales.SettingsAddRootCa(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.Plus(), Trailing: lucide.ChevronDown(), AccessibleLabel: locales.SettingsAddRootCaAction()})
		nui.DropdownMenu(c.Key("add-menu"), palette, anchor, &state.rootMenu, []nui.MenuItem{
			{ID: "files", Label: locales.SettingsSelectFiles(), Icon: lucide.FolderOpen(), OnSelect: func() { v.pickRoots(proxy) }},
			{ID: "manual", Label: locales.SettingsAddPathManually(), Icon: lucide.Pencil(), OnSelect: func() {
				proxy.RootCAPaths = append(proxy.RootCAPaths, "")
				state.rootsOpen = true
			}},
		}, nui.MenuOptions{Label: locales.SettingsAddRootCaAction(), Placement: nui.Set(nui.BottomEnd)})
	})
	if !state.rootsOpen {
		return
	}
	if len(proxy.RootCAPaths) == 0 {
		ui.Text(c.Key("roots-empty"), locales.SettingsNoRootCasDesc()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted).FillWidth()
		return
	}
	for index := range proxy.RootCAPaths {
		number := strconv.Itoa(index + 1)
		ui.Row(c.Key("root-" + number)).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c.Key("input")).Grow(1).Basis(0).MinWidth(0).Children(func() {
				v.fileInput(c, palette, "file", locales.SettingsRootCaPathPlaceholder()+" "+number, locales.SettingsRootCaPathPlaceholder(), "", certificatePicker(locales.SettingsSelectRootCaFile(), locales.SettingsSelectRootCaFileMessage()), &proxy.RootCAPaths[index])
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
		return v.Host.PickFiles(certificatePicker(locales.SettingsSelectRootCaFiles(), locales.SettingsSelectRootCaFilesMessage()))
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
	summary, color := locales.SettingsNoClientCerts(), palette.Muted
	if count := len(proxy.ClientCerts); count > 0 {
		if incomplete {
			summary, color = locales.SettingsClientCertsIncomplete(strconv.Itoa(count)), palette.Warning
		} else {
			summary = locales.SettingsClientCertsCount(strconv.Itoa(count))
		}
	}
	collapsibleHead(c, palette, "certs-head", locales.SettingsSectionClientCerts(), summary, color, &state.certsOpen, func() {
		nui.Button(c.Key("add"), palette, locales.SettingsAddClientCert(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.Plus()}).OnClick(func() {
			proxy.ClientCerts = append(proxy.ClientCerts, settingservice.ClientCertConfig{Enabled: true})
			state.certsOpen = true
		})
	})
	if !state.certsOpen {
		return
	}
	if len(proxy.ClientCerts) == 0 {
		ui.Text(c.Key("certs-empty"), locales.SettingsNoClientCertsDesc()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted).FillWidth()
		return
	}
	for index := range proxy.ClientCerts {
		cert := &proxy.ClientCerts[index]
		position := index
		number := strconv.Itoa(index + 1)
		nui.Card(c.Key("cert-"+number), palette, nui.CardOptions{
			Decoration: nui.Decoration{UI: nui.Styles{
				nui.Body: {PaddingX: nui.Set(float32(10)), PaddingY: nui.Set(float32(10)), Gap: nui.Set(float32(8))},
			}},
		}, func() {
			title := strings.TrimSpace(cert.Hostname)
			if title == "" {
				title = locales.SettingsClientCertNewTitle(number)
			}
			ui.Row(c.Key("head")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				nui.Switch(c.Key("switch"), palette, &cert.Enabled, nui.SwitchOptions{}).Label(title)
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, title).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text)
					state := locales.SettingsClientCertDisabled()
					if cert.Enabled {
						state = locales.SettingsClientCertEnabled()
					}
					ui.Text(c, state).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
				})
				nui.Button(c.Key("remove"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Trash(), AccessibleLabel: locales.SettingsRemoveClientCert() + " " + number}).OnClick(func() {
					next := append([]settingservice.ClientCertConfig(nil), proxy.ClientCerts[:position]...)
					proxy.ClientCerts = append(next, proxy.ClientCerts[position+1:]...)
				})
			})
			nui.FormField(c.Key("host"), palette, nui.FormFieldOptions{Label: locales.SettingsClientCertTargetHost(), Help: locales.SettingsClientCertTargetHostHint()}, func(field nui.Theme) {
				nui.Input(c.Key("input"), field, &cert.Hostname, nui.InputOptions{Label: locales.SettingsClientCertTargetHost() + " " + number, Placeholder: locales.SettingsClientCertTargetHostPlaceholder()}).FillWidth()
			})
			nui.FormField(c.Key("cert"), palette, nui.FormFieldOptions{Label: locales.SettingsClientCertFile()}, func(field nui.Theme) {
				v.fileInput(c, field, "file", locales.SettingsClientCertFile()+" "+number, locales.SettingsClientCertPathPlaceholder(), "", certificatePicker(locales.SettingsSelectClientCertFile(), locales.SettingsSelectClientCertFileMessage()), &cert.CertPath)
			})
			nui.FormField(c.Key("key"), palette, nui.FormFieldOptions{Label: locales.SettingsClientKeyFile()}, func(field nui.Theme) {
				v.fileInput(c, field, "file", locales.SettingsClientKeyFile()+" "+number, locales.SettingsClientKeyPathPlaceholder(), "", keyPicker(locales.SettingsSelectClientKeyFile(), locales.SettingsSelectClientKeyFileMessage()), &cert.KeyPath)
			})
			if warning := certificateWarning(cert); warning != "" {
				ui.Row(c.Key("warning")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, lucide.Info()).Size(14, 14).Shrink(0).TextColor(palette.Warning)
					ui.Text(c, warning).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Warning).FillWidth()
				})
			}
		}).FillWidth()
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
