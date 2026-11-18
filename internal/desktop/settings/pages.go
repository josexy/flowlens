package settings

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/logger"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func confirmRequest(title, text, confirm string, color nui.Color, run func()) widgets.ConfirmRequest {
	return widgets.ConfirmRequest{Title: title, Message: text, CancelLabel: locales.HistoryCancel(), Confirm: widgets.ConfirmAction{Label: confirm, Color: color, Run: run}}
}

type pythonState struct {
	picking, finding, testing bool
	accepted                  bool
	status                    *pythonpluginservice.RuntimeStatus
	testError                 string
	seenPath                  string
	candidates                []pythonpluginservice.InterpreterCandidate
	discovery                 bool
	selected                  string
	timeout                   integerField
}

func (v *View) refreshPython() {
	if v.Host.PythonStatus == nil {
		return
	}
	run(v, "python-status", v.Host.PythonStatus, func(status *pythonpluginservice.RuntimeStatus, err error) {
		if err == nil {
			v.Data.Settings.PythonStatus = status
		}
	})
}

func runtimeVersion(status *pythonpluginservice.RuntimeStatus) string {
	if status == nil || !status.Ready {
		return ""
	}
	return fmt.Sprintf("%s %d.%d.%d", status.Implementation, status.PythonMajor, status.PythonMinor, status.PythonPatch)
}

func (v *View) runtimeBadge() (string, nui.Color) {
	status := v.Data.Settings.PythonStatus
	switch {
	case status == nil:
		return locales.SettingsPythonRuntimeUnknown(), nui.Neutral
	case !status.Enabled:
		return locales.SettingsPythonRuntimeDisabled(), nui.Neutral
	case status.Error != "":
		return locales.SettingsPythonRuntimeError(), nui.Error
	case status.Ready:
		return locales.SettingsPythonRuntimeReady(), nui.Success
	default:
		return locales.SettingsPythonRuntimeIdle(), nui.Warning
	}
}

func (v *View) requestPython(config *settingservice.PythonPluginConfig, enabled bool) {
	if !enabled || v.python.accepted {
		config.Enabled = enabled
		return
	}
	v.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.SettingsPythonTrustedCodeTitle(), Message: locales.SettingsPythonTrustedCodeConfirmMessage(), CancelLabel: locales.SettingsPythonTrustedCodeCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.SettingsPythonTrustedCodeConfirm(), Color: nui.Warning, Run: func() {
			v.python.accepted = true
			config.Enabled = true
		}},
	})
}

func (v *View) testPython(path string) {
	state := &v.python
	state.testing, state.status, state.testError = true, nil, ""
	run(v, "python-test", func() (*pythonpluginservice.RuntimeStatus, error) { return v.Host.TestPython(path) }, func(status *pythonpluginservice.RuntimeStatus, err error) {
		state.testing = false
		if err != nil {
			state.testError = err.Error()
			return
		}
		state.status = status
	})
}

func (v *View) useInterpreter(config *settingservice.PythonPluginConfig, path string) {
	if path == "" {
		return
	}
	v.python.discovery = false
	config.InterpreterPath = path
	v.python.seenPath = path
	v.testPython(path)
}

func (v *View) detectPython(config *settingservice.PythonPluginConfig) {
	state := &v.python
	state.finding = true
	current := config.InterpreterPath
	run(v, "python-find", func() ([]pythonpluginservice.InterpreterCandidate, error) { return v.Host.FindPython(current) }, func(candidates []pythonpluginservice.InterpreterCandidate, err error) {
		state.finding = false
		if err != nil {
			v.Notifier.Error(locales.SettingsPythonInterpreterDetectFailed(err.Error()))
			return
		}
		state.candidates = candidates
		switch len(candidates) {
		case 0:
			v.Notifier.Warning(locales.SettingsPythonInterpreterDetectNone())
		case 1:
			v.useInterpreter(config, candidates[0].InterpreterPath)
		default:
			state.selected = candidates[0].InterpreterPath
			for _, candidate := range candidates {
				if candidate.Current {
					state.selected = candidate.InterpreterPath
				}
			}
			state.discovery = true
		}
	})
}

func (v *View) pythonPage(c *ui.Context, palette nui.Theme, draft *settingservice.Settings) {
	config := draft.PythonPluginConfig
	state := &v.python
	if config.InterpreterPath != state.seenPath {
		state.seenPath, state.status, state.testError = config.InterpreterPath, nil, ""
	}
	v.section(c, palette, "python", locales.SettingsSectionPythonPlugins(), false, nil, func() {
		row(c, palette, "enabled", locales.SettingsPythonPluginsEnabledLabel(), "", func() {
			ui.Row(c.Key("line")).Gap(8).AlignItems(ui.Center).Children(func() {
				enabled := config.Enabled
				nui.Switch(c.Key("switch"), palette, &enabled, nui.SwitchOptions{}).Label(locales.SettingsPythonPluginsEnabledLabel())
				if enabled != config.Enabled {
					v.requestPython(config, enabled)
				}
				label, color := v.runtimeBadge()
				nui.Badge(c.Key("status"), palette, label, nui.BadgeOptions{Color: color, Variant: nui.Soft})
			})
		})
		row(c, palette, "interpreter", locales.SettingsPythonInterpreterLabel(), "", func() {
			ui.Row(c.Key("path")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c.Key("input")).Grow(1).Basis(0).MinWidth(0).Children(func() {
					nui.Input(c, palette, &config.InterpreterPath, nui.InputOptions{Label: locales.SettingsPythonInterpreterLabel(), Placeholder: locales.SettingsPythonInterpreterPlaceholder()}).FillWidth()
				})
				blocked := state.finding || state.testing
				browse := nui.Button(c.Key("browse"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Square: true, Leading: lucide.FolderOpen(), AccessibleLabel: locales.SettingsPythonInterpreterBrowse(), Disabled: blocked, Loading: state.picking})
				if !blocked {
					browse.OnClick(func() {
						state.picking = true
						run(v, "python-pick", func() (string, error) {
							return v.Host.PickFile(FilePicker{Title: locales.SettingsPythonInterpreterPickerTitle(), Message: locales.SettingsPythonInterpreterPickerMessage()})
						}, func(path string, err error) {
							state.picking = false
							if err != nil {
								v.Notifier.Error(locales.SettingsPythonInterpreterPickerFailed(err.Error()))
								return
							}
							if trimmed := strings.TrimSpace(path); trimmed != "" {
								config.InterpreterPath = trimmed
							}
						})
					})
				}
				nui.Tooltip(c, palette, browse, locales.SettingsPythonInterpreterBrowse(), nui.TooltipOptions{})
			})
			ui.Row(c.Key("actions")).Gap(6).Children(func() {
				detect := nui.Button(c.Key("detect"), palette, locales.SettingsPythonInterpreterDetect(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.ScanSearch(), Disabled: state.picking || state.testing, Loading: state.finding})
				if !state.picking && !state.testing {
					detect.OnClick(func() { v.detectPython(config) })
				}
				disabled := strings.TrimSpace(config.InterpreterPath) == "" || state.finding
				test := nui.Button(c.Key("test"), palette, locales.SettingsPythonInterpreterTest(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.FlaskConical(), Disabled: disabled, Loading: state.testing})
				if !disabled {
					test.OnClick(func() { v.testPython(config.InterpreterPath) })
				}
			})
			version := runtimeVersion(state.status)
			switch {
			case state.status != nil && state.status.Ready:
				nui.Alert(c.Key("test-result"), palette, nui.AlertOptions{Description: locales.SettingsPythonInterpreterTestSuccess(version), Color: nui.Success, Variant: nui.Soft, Icon: lucide.CircleCheck()})
			case state.testError != "":
				nui.Alert(c.Key("test-result"), palette, nui.AlertOptions{Description: locales.SettingsPythonInterpreterTestFailed(state.testError), Color: nui.Error, Variant: nui.Soft, Icon: lucide.CircleX()})
			}
		})
		row(c, palette, "timeout", locales.SettingsPythonHookTimeoutLabel(), "", func() {
			ui.Row(c.Key("line")).Gap(6).AlignItems(ui.Center).Children(func() {
				state.timeout.build(c, palette, "number", locales.SettingsPythonHookTimeoutAccessibleLabel(), &config.HookTimeoutMs, 100, 60000, 100, false)
				ui.Text(c, locales.SettingsUnitMilliseconds()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted)
			})
		})
		runtime := v.Data.Settings.PythonStatus
		if runtime != nil && runtime.Error != "" {
			nui.Alert(c.Key("runtime-error"), palette, nui.AlertOptions{Title: locales.SettingsPythonRuntimeError(), Description: runtime.Error, Color: nui.Error, Variant: nui.Soft})
		} else if version := runtimeVersion(runtime); version != "" && (state.status == nil || !state.status.Ready) {
			ui.Text(c.Key("runtime-version"), locales.SettingsPythonRuntimeVersion(version)).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
		}
	})
	v.discoveryDialog(c, palette, config)
}

func (v *View) discoveryDialog(c *ui.Context, palette nui.Theme, config *settingservice.PythonPluginConfig) {
	state := &v.python
	if !state.discovery {
		return
	}
	v.Dispatcher.NoteModal()
	nui.ModalWithOptions(c.Key("python-detect"), palette, &state.discovery, nui.ModalOptions{Title: locales.SettingsPythonInterpreterDetectTitle(), Width: nui.Set[float32](560), Footer: func(c *ui.Context, theme nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), theme, locales.SettingsPythonInterpreterDetectCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).OnClick(func() { state.discovery = false })
			nui.Button(c.Key("use"), theme, locales.SettingsPythonInterpreterDetectUse(), nui.ButtonOptions{Disabled: state.selected == ""}).OnClick(func() { v.useInterpreter(config, state.selected) })
		})
	}}, func(theme nui.Theme) {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, locales.SettingsPythonInterpreterDetectDescription()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted).FillWidth()
			highest := state.candidates[0]
			for index, candidate := range state.candidates {
				selected := state.selected == candidate.InterpreterPath
				version := fmt.Sprintf("%s %d.%d.%d", candidate.Implementation, candidate.PythonMajor, candidate.PythonMinor, candidate.PythonPatch)
				card := ui.Column(c.Key(index)).FillWidth().Gap(2).Padding(8).Border(1, theme.Border).Radius(theme.Radius).Role(ui.RoleButton).Label(candidate.InterpreterPath)
				if selected {
					card.Border(1, theme.Primary).Background(theme.Primary.Alpha(0.08))
				}
				path := candidate.InterpreterPath
				card.OnClick(func() { state.selected = path })
				card.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, version).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
						if candidate.PythonMajor == highest.PythonMajor && candidate.PythonMinor == highest.PythonMinor && candidate.PythonPatch == highest.PythonPatch {
							nui.Badge(c.Key("latest"), theme, locales.SettingsPythonInterpreterDetectLatest(), nui.BadgeOptions{Color: nui.Success, Variant: nui.Soft, Size: nui.SM})
						}
						if candidate.Current {
							nui.Badge(c.Key("current"), theme, locales.SettingsPythonInterpreterDetectCurrent(), nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM})
						}
					})
					ui.Text(c, candidate.InterpreterPath).FontSize(apptheme.TextSize(c, 12)).Font(theme.MonoFont).TextColor(theme.Muted)
				})
			}
		})
	})
}

type logState struct {
	status                            *logger.LogStatus
	loaded, updatingEnabled, updating bool
	enabledTarget                     bool
	level                             string
	opening, clearing                 bool
}

func (v *View) loadLogs() {
	state := &v.logs
	if state.loaded || v.Host.LogStatus == nil {
		return
	}
	state.loaded = true
	run(v, "logs", v.Host.LogStatus, func(status *logger.LogStatus, err error) {
		if err != nil {
			state.loaded = false
			v.Notifier.Error(locales.SettingsLogErrorLoad(err.Error()))
			return
		}
		state.status, state.level = status, string(status.Level)
	})
}

func logLevelOptions() []nui.Option[string] {
	return []nui.Option[string]{
		option(string(logger.LogLevelTrace), locales.SettingsLogLevelTrace()), option(string(logger.LogLevelDebug), locales.SettingsLogLevelDebug()),
		option(string(logger.LogLevelInfo), locales.SettingsLogLevelInfo()), option(string(logger.LogLevelWarn), locales.SettingsLogLevelWarn()),
		option(string(logger.LogLevelError), locales.SettingsLogLevelError()),
	}
}

func (v *View) logsPage(c *ui.Context, palette nui.Theme) {
	state := &v.logs
	ui.Text(c.Key("immediate"), locales.SettingsLogImmediateNotice()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted).FillWidth()
	if state.status == nil {
		v.loadLogs()
		return
	}
	v.section(c, palette, "logging", "", false, nil, func() {
		row(c, palette, "enabled", locales.SettingsLogEnabledLabel(), "", func() {
			enabled := state.status.Enabled
			if state.updatingEnabled {
				enabled = state.enabledTarget
			}
			before := enabled
			nui.Switch(c.Key("switch"), palette, &enabled, nui.SwitchOptions{Disabled: state.updatingEnabled, Loading: state.updatingEnabled}).DimDisabled(false).Label(locales.SettingsLogEnabledLabel())
			if enabled != before {
				state.enabledTarget = enabled
				state.updatingEnabled = true
				run(v, "log-enabled", func() (*logger.LogStatus, error) { return v.Host.SetLog(enabled) }, func(status *logger.LogStatus, err error) {
					state.updatingEnabled = false
					if err != nil {
						v.Notifier.Error(locales.SettingsLogErrorToggle(err.Error()))
						return
					}
					state.status, state.level = status, string(status.Level)
				})
			}
		})
		row(c, palette, "level", locales.SettingsLogLevelLabel(), "", func() {
			ui.Box(c.Key("menu")).Width(240).Children(func() {
				nui.Select(c.Key("select"), palette, &state.level, logLevelOptions(), nui.SelectOptions{Label: locales.SettingsLogLevelLabel(), Disabled: state.updating}).DimDisabled(false)
			})
			if state.level != string(state.status.Level) && !state.updating {
				level := logger.LogLevel(state.level)
				previous := string(state.status.Level)
				state.updating = true
				run(v, "log-level", func() (*logger.LogStatus, error) { return v.Host.SetLevel(level) }, func(status *logger.LogStatus, err error) {
					state.updating = false
					if err != nil {
						state.level = previous
						v.Notifier.Error(locales.SettingsLogErrorLevel(err.Error()))
						return
					}
					state.status, state.level = status, string(status.Level)
				})
			}
		})
	})
	v.section(c, palette, "maintenance", locales.SettingsLogMaintenanceLabel(), false, nil, func() {
		ui.Row(c.Key("actions")).Gap(8).Children(func() {
			nui.Button(c.Key("open"), palette, locales.SettingsLogOpenDir(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.FolderOpen(), Loading: state.opening}).OnClick(func() {
				state.opening = true
				run(v, "log-open", func() (struct{}, error) { return struct{}{}, v.Host.OpenLogDir() }, func(_ struct{}, err error) {
					state.opening = false
					if err != nil {
						v.Notifier.Error(locales.SettingsLogErrorOpenDir(err.Error()))
					}
				})
			})
			nui.Button(c.Key("clear"), palette, locales.SettingsLogClearLogs(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Warning, Leading: lucide.Trash(), Loading: state.clearing}).OnClick(func() {
				v.Confirm.Ask(confirmRequest(locales.SettingsLogClearLogs(), locales.SettingsLogClearLogsConfirm(), locales.SettingsLogClearLogs(), nui.Warning, func() {
					state.clearing = true
					run(v, "log-clear", func() (struct{}, error) { return struct{}{}, v.Host.ClearLogs() }, func(_ struct{}, err error) {
						state.clearing = false
						if err != nil {
							v.Notifier.Error(locales.SettingsLogErrorClearLogs(err.Error()))
							return
						}
						v.Notifier.Success(locales.SettingsLogSuccessClearedLogs())
					})
				}))
			})
		})
	})
}

type storageState struct {
	size                   proxyservice.LocalDataSize
	sequence               int
	pending                bool
	cacheDone, allDone     bool
	threshold, maxMessages integerField
	retention              integerField
}

func (v *View) loadDataSize() {
	if v.Host.DataSize == nil {
		return
	}
	state := &v.storage
	state.sequence++
	sequence := state.sequence
	run(v, "data-size", v.Host.DataSize, func(size proxyservice.LocalDataSize, err error) {
		if err == nil && sequence == state.sequence {
			state.size = size
		}
	})
}

func (v *View) clearData(work func() error, done *bool) {
	state := &v.storage
	if state.pending {
		return
	}
	state.pending, state.cacheDone, state.allDone = true, false, false
	run(v, "data-clear", func() (struct{}, error) { return struct{}{}, work() }, func(_ struct{}, err error) {
		state.pending = false
		if err != nil {
			v.Notifier.Error(err.Error())
		} else {
			*done = true
		}
		v.loadDataSize()
	})
}

func formatSize(bytes int64) string {
	return logic.FormatFileSizeWith(bytes, logic.FileSizeOptions{Precision: 1})
}

func (v *View) dangerRow(c *ui.Context, palette nui.Theme, key, title, description, label string, color nui.Color, variant nui.Variant, disabled, loading bool, run func()) {
	border := palette.Border
	if color == nui.Error {
		border = palette.Error.Alpha(0.35).Over(palette.Border)
	}
	nui.Card(c.Key(key), palette, nui.CardOptions{
		Variant: nui.Subtle,
		Decoration: nui.Decoration{UI: nui.Styles{
			nui.Root: {BorderColor: nui.Set(border)},
			nui.Body: {PaddingX: nui.Set(float32(12)), PaddingY: nui.Set(float32(12))},
		}},
	}, func() {
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(palette.Text)
				ui.Text(c, description).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.5).TextColor(palette.Muted).FillWidth()
			})
			button := nui.Button(c.Key("action"), palette, label, nui.ButtonOptions{Color: color, Variant: variant, Leading: lucide.Trash(), Disabled: disabled, Loading: loading, AccessibleLabel: title}).Shrink(0)
			if !disabled {
				button.OnClick(run)
			}
		})
	}).FillWidth()
}

func (v *View) storagePage(c *ui.Context, palette nui.Theme, draft *settingservice.Settings) {
	state := &v.storage
	cache, retention := draft.CacheConfig, draft.HistoryRetentionConfig
	v.section(c, palette, "cache", locales.SettingsSectionCache(), false, nil, func() {
		row(c, palette, "threshold", locales.SettingsCacheThresholdLabel(), locales.SettingsCacheThresholdHint(), func() {
			threshold := int(cache.BodyCacheThresholdBytes)
			ui.Row(c.Key("line")).Gap(6).AlignItems(ui.Center).Children(func() {
				state.threshold.build(c, palette, "number", locales.SettingsCacheThresholdAccessibleLabel(), &threshold, 1, 1<<40, 1, false)
				ui.Text(c, locales.SettingsUnitBytes()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted)
			})
			cache.BodyCacheThresholdBytes = int64(threshold)
		})
		row(c, palette, "messages", locales.SettingsWsMaxMessagesLabel(), locales.SettingsWsMaxMessagesHint(), func() {
			state.maxMessages.build(c, palette, "number", locales.SettingsWsMaxMessagesLabel(), &cache.MaxWsMessages, 1, 1<<30, 1, false)
		})
	})
	v.section(c, palette, "retention", locales.SettingsSectionHistoryRetention(), false, nil, func() {
		row(c, palette, "enabled", locales.SettingsHistoryRetentionEnabledLabel(), locales.SettingsHistoryRetentionEnabledHint(), func() {
			nui.Switch(c.Key("switch"), palette, &retention.Enabled, nui.SwitchOptions{}).Label(locales.SettingsHistoryRetentionEnabledLabel())
		})
		row(c, palette, "period", locales.SettingsHistoryRetentionPeriodLabel(), locales.SettingsHistoryRetentionPeriodHint(), func() {
			ui.Row(c.Key("line")).Gap(6).AlignItems(ui.Center).Children(func() {
				state.retention.build(c, palette, "number", locales.SettingsHistoryRetentionPeriodLabel(), &retention.Value, 1, 9999, 1, !retention.Enabled)
				unit := string(retention.Unit)
				ui.Box(c.Key("unit")).Width(140).Children(func() {
					nui.Select(c.Key("select"), palette, &unit, []nui.Option[string]{
						option(string(settingservice.HistoryRetentionUnitHour), locales.SettingsHistoryRetentionUnitHour()), option(string(settingservice.HistoryRetentionUnitDay), locales.SettingsHistoryRetentionUnitDay()),
						option(string(settingservice.HistoryRetentionUnitWeek), locales.SettingsHistoryRetentionUnitWeek()), option(string(settingservice.HistoryRetentionUnitMonth), locales.SettingsHistoryRetentionUnitMonth()),
						option(string(settingservice.HistoryRetentionUnitYear), locales.SettingsHistoryRetentionUnitYear()),
					}, nui.SelectOptions{Label: locales.SettingsHistoryRetentionUnitLabel(), Disabled: !retention.Enabled})
				})
				retention.Unit = settingservice.HistoryRetentionUnit(unit)
			})
		})
	})
	v.section(c, palette, "disk", locales.SettingsSectionDiskUsage(), false, nil, func() {
		// Compact tiles, each as wide as its content and at least 118 wide.
		ui.Row(c.Key("grid")).FillWidth().Wrap().Gap(8).Children(func() {
			for index, item := range []struct {
				label string
				value int64
			}{{locales.SettingsDiskUsageCache(), state.size.CacheBytes}, {locales.SettingsDiskUsageHistory(), state.size.HistoryBytes}, {locales.SettingsDiskUsageTotal(), state.size.CacheBytes + state.size.HistoryBytes}} {
				nui.Card(c.Key(index), palette, nui.CardOptions{
					Variant: nui.Subtle,
					Decoration: nui.Decoration{UI: nui.Styles{
						nui.Body: {PaddingX: nui.Set(float32(10)), PaddingY: nui.Set(float32(9)), Gap: nui.Set(float32(4))},
					}},
				}, func() {
					ui.Text(c, item.label).SingleLine().FontSize(apptheme.TextSize(c, 14)).LineHeight(1.4).FontWeight(600).TextColor(palette.Muted)
					ui.Text(c, formatSize(item.value)).FontSize(apptheme.TextSize(c, 14)).LineHeight(1.35).FontWeight(600).TextColor(palette.Toned)
				}).MinWidth(118)
			}
		})
	})
	v.section(c, palette, "danger", locales.SettingsSectionDanger(), true, nil, func() {
		if state.cacheDone {
			nui.Alert(c.Key("cache-done"), palette, nui.AlertOptions{Title: locales.SettingsClearCacheSuccess(), Color: nui.Success, Variant: nui.Subtle})
		}
		if state.allDone {
			nui.Alert(c.Key("all-done"), palette, nui.AlertOptions{Title: locales.SettingsClearAllDataSuccess(), Color: nui.Success, Variant: nui.Subtle})
		}
		blocked := state.pending
		v.dangerRow(c, palette, "clear-cache", locales.SettingsClearCacheFiles(), locales.SettingsClearCacheFilesDesc(), locales.SettingsClearAction(), nui.Neutral, nui.Outline, blocked, false, func() {
			v.Confirm.Ask(confirmRequest(locales.SettingsClearCacheFiles(), locales.SettingsClearCacheFilesConfirm(), locales.SettingsClearAction(), nui.Warning, func() { v.clearData(v.Host.ClearCache, &state.cacheDone) }))
		})
		v.dangerRow(c, palette, "clear-all", locales.SettingsClearAllData(), locales.SettingsClearAllDataDesc(), locales.SettingsClearAction(), nui.Error, nui.Solid, blocked, false, func() {
			v.Confirm.Ask(confirmRequest(locales.SettingsClearAllData(), locales.SettingsClearAllDataConfirm(), locales.SettingsClearAction(), nui.Error, func() { v.clearData(v.Host.ClearAllData, &state.allDone) }))
		})
	})
}

type shortcutState struct {
	search    string
	recording string
	held      []ui.Key
	settled   bool
}

func (s *shortcutState) stop() { s.recording, s.held, s.settled = "", nil, false }

func (v *View) keyLabels(binding *shortcuts.Binding) string {
	return strings.Join(shortcuts.KbdKeys(binding, v.MacOS), "+")
}

func (v *View) applyOverride(config *settingservice.ShortcutConfig, command shortcuts.Command, binding *shortcuts.Binding, scope settingservice.ShortcutScope) {
	if config.Overrides == nil {
		config.Overrides = map[string]settingservice.ShortcutOverride{}
	}
	if command.ID == shortcuts.AppShowMainWindow {
		scope = settingservice.ShortcutScopeGlobal
	}
	config.Overrides[command.ID] = settingservice.ShortcutOverride{Binding: binding, Scope: scope}
}

func scopeOf(config *settingservice.ShortcutConfig, command shortcuts.Command) settingservice.ShortcutScope {
	if command.ID == shortcuts.AppShowMainWindow {
		return settingservice.ShortcutScopeGlobal
	}
	if command.ID == shortcuts.CaptureToggleProxy && config.Overrides != nil && config.Overrides[command.ID].Scope == settingservice.ShortcutScopeGlobal {
		return settingservice.ShortcutScopeGlobal
	}
	return settingservice.ShortcutScopeApplication
}

func (v *View) requestBinding(config *settingservice.ShortcutConfig, command shortcuts.Command, binding *shortcuts.Binding) {
	conflicts := shortcuts.Conflicts(command.ID, binding, config, v.MacOS)
	if len(conflicts) == 0 {
		v.applyOverride(config, command, binding, scopeOf(config, command))
		return
	}
	names := make([]string, len(conflicts))
	for index, conflict := range conflicts {
		names[index] = conflict.Label()
	}
	v.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.ShortcutsConflictTitle(), Message: locales.ShortcutsConflictMessage(v.keyLabels(binding), command.Label(), strings.Join(names, locales.SettingsFieldSeparator())), CancelLabel: locales.ShortcutsCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.ShortcutsReplace(), Color: nui.Warning, Run: func() {
			v.applyOverride(config, command, binding, scopeOf(config, command))
			for _, conflict := range conflicts {
				v.applyOverride(config, conflict, nil, scopeOf(config, conflict))
			}
		}},
	})
}

// watchRecording turns the next key press into a binding.
func (v *View) watchRecording(c *ui.Context, config *settingservice.ShortcutConfig, command shortcuts.Command) {
	state := &v.shortcuts
	v.Dispatcher.Suspend()
	held := c.KeysDown()
	if !state.settled {
		state.held, state.settled = slices.Clone(held), true
		return
	}
	var pressed ui.Key
	found := false
	for _, key := range held {
		if !slices.Contains(state.held, key) {
			pressed, found = key, true
			break
		}
	}
	state.held = slices.DeleteFunc(state.held, func(key ui.Key) bool { return !slices.Contains(held, key) })
	if !found {
		return
	}
	outcome, binding := shortcuts.Record(pressed, c.Modifiers(), v.MacOS)
	if outcome == shortcuts.RecordContinue {
		state.held = append(state.held, pressed)
		return
	}
	state.stop()
	switch outcome {
	case shortcuts.RecordClear:
		v.applyOverride(config, command, nil, scopeOf(config, command))
	case shortcuts.RecordBinding:
		v.requestBinding(config, command, binding)
	}
}

func runtimeStatusColor(status shortcutservice.ShortcutRuntimeStatus) nui.Color {
	switch status {
	case shortcutservice.ShortcutStatusActive:
		return nui.Success
	case shortcutservice.ShortcutStatusConflict:
		return nui.Error
	case shortcutservice.ShortcutStatusPortalPending:
		return nui.Warning
	default:
		return nui.Neutral
	}
}

func (v *View) shortcutsPage(c *ui.Context, palette nui.Theme, draft *settingservice.Settings) {
	state := &v.shortcuts
	config := draft.Shortcuts
	settings := v.Data.Settings
	runtimeState := settings.ShortcutRuntime
	warnings := append([]string(nil), runtimeState.Warnings...)
	if result := settings.LastShortcutResult; result != nil {
		if !result.Applied {
			runtimeState = result.RuntimeState
			warnings = append([]string(nil), runtimeState.Warnings...)
		}
		warnings = append(warnings, result.Warnings...)
	}
	slices.Sort(warnings)
	warnings = slices.Compact(warnings)
	ui.Row(c.Key("toolbar")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c.Key("search")).Grow(1).Basis(0).MinWidth(0).Children(func() {
			nui.Input(c, palette, &state.search, nui.InputOptions{Label: locales.ShortcutsSearchPlaceholder(), Placeholder: locales.ShortcutsSearchPlaceholder(), Leading: lucide.Search()}).FillWidth()
		})
		nui.Button(c.Key("reset-all"), palette, locales.ShortcutsResetAll(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.RotateCcw()}).OnClick(func() {
			v.Confirm.Ask(widgets.ConfirmRequest{Title: locales.ShortcutsResetAll(), Message: locales.ShortcutsResetAllConfirm(), CancelLabel: locales.ShortcutsCancel(), Confirm: widgets.ConfirmAction{Label: locales.ShortcutsResetAll(), Color: nui.Warning, Run: func() {
				for _, command := range shortcuts.Catalog {
					delete(config.Overrides, command.ID)
				}
			}}})
		})
	})
	ui.Text(c.Key("note"), locales.ShortcutsGlobalNote()).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted).FillWidth()
	for index, warning := range warnings {
		nui.Alert(c.Key(fmt.Sprintf("warning-%d", index)), palette, nui.AlertOptions{Title: message("shortcutsWarnings" + camel(warning)), Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()})
	}
	query := strings.ToLower(strings.TrimSpace(state.search))
	type group struct {
		label    string
		commands []shortcuts.Command
	}
	var groups []*group
	for _, command := range shortcuts.Catalog {
		category, label := command.Category(), command.Label()
		if query != "" && !strings.Contains(strings.ToLower(category), query) && !strings.Contains(strings.ToLower(label), query) && !strings.Contains(strings.ToLower(command.ID), query) {
			continue
		}
		var target *group
		for _, existing := range groups {
			if existing.label == category {
				target = existing
			}
		}
		if target == nil {
			target = &group{label: category}
			groups = append(groups, target)
		}
		target.commands = append(target.commands, command)
	}
	if len(groups) == 0 {
		ui.Text(c.Key("empty"), locales.ShortcutsEmptySearch()).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted)
		return
	}
	for index, current := range groups {
		ui.Column(c.Key(fmt.Sprintf("group-%d", index))).FillWidth().Gap(10).Margin(20, 0, 0, 0).Children(func() {
			ui.Text(c, current.label).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(palette.Highlighted)
			ui.Column(c.Key("commands")).FillWidth().Radius(8).Border(1, palette.Border).Background(palette.Background).Clip().Children(func() {
				for index, command := range current.commands {
					if index > 0 {
						ui.Box(c.Key("separator-" + command.ID)).FillWidth().Height(1).Background(palette.Border)
					}
					v.commandRow(c, palette, config, command, runtimeState)
				}
			})
		})
	}
}

func (v *View) commandRow(c *ui.Context, palette nui.Theme, config *settingservice.ShortcutConfig, command shortcuts.Command, runtimeState shortcutservice.ShortcutRuntimeState) {
	state := &v.shortcuts
	resolved := shortcuts.ResolveCommand(command, config)
	recording := state.recording == command.ID
	if recording {
		v.watchRecording(c, config, command)
	}
	label := command.Label()
	line := ui.Row(c.Key(command.ID)).FillWidth().MinHeight(52).AlignItems(ui.Center).Gap(8).Padding(10, 12)
	if recording {
		line.Background(palette.Primary.Alpha(0.1))
	}
	line.Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Text)
			scope := scopeOf(config, command)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				switch command.ID {
				case shortcuts.CaptureToggleProxy:
					global := scope == settingservice.ShortcutScopeGlobal
					ui.Text(c, locales.ShortcutsScopeGlobal()).FontSize(apptheme.TextSize(c, 12)).TextColor(palette.Muted)
					before := global
					nui.Switch(c.Key("scope"), palette, &global, nui.SwitchOptions{Size: nui.SM}).Label(locales.ShortcutsScopeLabel())
					if global != before {
						next := settingservice.ShortcutScopeApplication
						if global {
							next = settingservice.ShortcutScopeGlobal
						}
						v.applyOverride(config, command, resolved.Binding, next)
					}
				case shortcuts.AppShowMainWindow:
					nui.Badge(c.Key("global"), palette, locales.ShortcutsScopeGlobal(), nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft, Size: nui.SM})
				}
				if scope == settingservice.ShortcutScopeGlobal && resolved.Binding != nil {
					if runtime, ok := runtimeState.Commands[command.ID]; ok {
						nui.Badge(c.Key("runtime"), palette, message("shortcutsRuntime"+camel(string(runtime.Status))), nui.BadgeOptions{Color: runtimeStatusColor(runtime.Status), Variant: nui.Soft, Size: nui.SM})
					}
				}
			})
		})
		text := locales.ShortcutsNotSet()
		if recording {
			text = locales.ShortcutsRecording()
		} else if resolved.Binding != nil {
			text = v.keyLabels(resolved.Binding)
		}
		record := nui.Button(c.Key("record"), palette, "", nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, AccessibleLabel: locales.ShortcutsRecordFor(label)}).Width(160).MinHeight(32).Shrink(0).Center()
		record.Children(func() {
			if recording || resolved.Binding == nil {
				ui.Text(c, text).SingleLine().MinWidth(0)
			} else {
				platform := "windows"
				if v.MacOS {
					platform = "darwin"
				}
				nui.Kbd(c, palette, shortcuts.KbdKeys(resolved.Binding, v.MacOS), nui.KbdOptions{Size: nui.XS, Platform: platform}).IgnoreInput(true)
			}
		})
		if !recording {
			record.OnClick(func() { *state = shortcutState{search: state.search, recording: command.ID} })
		}
		clear := nui.Button(c.Key("clear"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Eraser(), AccessibleLabel: locales.ShortcutsClearFor(label), Disabled: resolved.Binding == nil})
		if resolved.Binding != nil {
			clear.OnClick(func() { v.applyOverride(config, command, nil, scopeOf(config, command)) })
		}
		nui.Tooltip(c, palette, clear, locales.ShortcutsClearFor(label), nui.TooltipOptions{})
		_, overridden := config.Overrides[command.ID]
		restore := nui.Button(c.Key("restore"), palette, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.RotateCcw(), AccessibleLabel: locales.ShortcutsResetFor(label), Disabled: !overridden})
		if overridden {
			restore.OnClick(func() { delete(config.Overrides, command.ID) })
		}
		nui.Tooltip(c, palette, restore, locales.ShortcutsResetFor(label), nui.TooltipOptions{})
	})
}
