package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/locale"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/theme"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
)

// SettingsSection names a part of the settings the user can change separately.
type SettingsSection string

// Settings sections.
const (
	SectionCommon             SettingsSection = "common"
	SectionProxy              SettingsSection = "proxy"
	SectionProcessAttribution SettingsSection = "processAttribution"
	SectionWindow             SettingsSection = "window"
	SectionCache              SettingsSection = "cache"
	SectionHistoryRetention   SettingsSection = "historyRetention"
	SectionTrafficTable       SettingsSection = "trafficTable"
	SectionPythonPlugins      SettingsSection = "pythonPlugins"
	SectionShortcuts          SettingsSection = "shortcuts"
)

// editableSections are the sections the Settings window edits.
var editableSections = []SettingsSection{
	SectionCommon, SectionProxy, SectionProcessAttribution, SectionWindow, SectionCache,
	SectionHistoryRetention, SectionTrafficTable, SectionPythonPlugins, SectionShortcuts,
}

// sectionField returns the address of the field of settings that holds
// section.
func sectionField(settings *settingservice.Settings, section SettingsSection) any {
	switch section {
	case SectionCommon:
		return &settings.CommonConfig
	case SectionProxy:
		return &settings.ProxyConfig
	case SectionProcessAttribution:
		return &settings.ProcessAttributionConfig
	case SectionWindow:
		return &settings.WindowConfig
	case SectionCache:
		return &settings.CacheConfig
	case SectionHistoryRetention:
		return &settings.HistoryRetentionConfig
	case SectionTrafficTable:
		return &settings.TrafficTableConfig
	case SectionPythonPlugins:
		return &settings.PythonPluginConfig
	case SectionShortcuts:
		return &settings.Shortcuts
	}
	panic(fmt.Sprintf("unknown settings section %q", section))
}

const (
	settingsSaveJob     = "settings/save"
	trafficTableJob     = "settings/traffic-table"
	trafficDetailJob    = "settings/traffic-detail"
	detailSaveDelay     = 400 * time.Millisecond
	defaultHookTimeout  = 5000
	defaultLogLevel     = "info"
	defaultWSMessages   = 1000
	defaultBodyCache    = 10 * 1024
	defaultRetention    = 7
	minRetention        = 1
	maxRetention        = 9999
	defaultProxyDNSTTL  = 60
	defaultProxyPort    = 8080
	defaultProxyAddress = "127.0.0.1"
)

// SettingsBackend is the settings service.
type SettingsBackend interface {
	Snapshot() (*settingservice.Settings, error)
	Save() error
	UpdatePreservingShortcuts(*settingservice.Settings) error
	SaveTrafficTableConfig(*settingservice.TrafficTableConfig) error
	SaveTrafficDetailConfig(*settingservice.TrafficDetailConfig) error
	SetThemeMode(string) error
	SetLanguage(string) error
	GetActiveWindowFrameMode() settingservice.WindowFrameMode
}

// ShortcutApplier registers shortcut configuration with the system.
type ShortcutApplier interface {
	ApplyShortcutConfig(*settingservice.ShortcutConfig) (*shortcutservice.ShortcutApplyResult, error)
	GetShortcutRuntimeState() shortcutservice.ShortcutRuntimeState
}

// ProxyConfigApplier applies saved proxy settings to the running proxy.
type ProxyConfigApplier interface {
	ApplyCurrentProxyConfig() (proxyservice.ProxyConfigApplyResult, error)
}

// PythonRuntimeConfigurer configures the Python runtime.
type PythonRuntimeConfigurer interface {
	ConfigureRuntime(settingservice.PythonPluginConfig) (*pythonpluginservice.RuntimeStatus, error)
	GetRuntimeStatus() (*pythonpluginservice.RuntimeStatus, error)
}

// SettingsDependencies are the services the settings store uses.
type SettingsDependencies struct {
	Backend   SettingsBackend
	Shortcuts ShortcutApplier
	Proxy     ProxyConfigApplier
	Python    PythonRuntimeConfigurer
	// ApplyLocale shows the interface in a saved language.
	ApplyLocale func(language string)
	// ApplyThemeMode updates the native appearance to match the interface.
	ApplyThemeMode func(mode string)
}

// SaveResult reports a finished save.
type SaveResult struct {
	// Complete is false when shortcuts could not all be applied.
	Complete bool
}

// SettingsStore holds saved settings and the Settings window's draft.
type SettingsStore struct {
	rt   Runtime
	deps SettingsDependencies

	// Current is the saved configuration every window follows.
	Current *settingservice.Settings
	// Draft is what the Settings window edits. It is nil while the window is
	// closed.
	Draft *settingservice.Settings
	// baseline is the saved state Draft started from. Changes that reach
	// Draft from outside the Settings window move it along, so only the
	// user's edits count as unsaved.
	baseline *settingservice.Settings

	dirtySections  map[SettingsSection]bool
	clock          Clock
	stopDetailSave func()
	// columnLayoutDirty asks for another save of the column layout after the
	// one running.
	columnLayoutDirty bool

	Saving                    bool
	SavingTrafficTableConfig  bool
	SavingTrafficDetailConfig bool

	ActiveFrameMode      settingservice.WindowFrameMode
	LastProxyApplyResult *proxyservice.ProxyConfigApplyResult
	LastShortcutResult   *shortcutservice.ShortcutApplyResult
	PythonStatus         *pythonpluginservice.RuntimeStatus
	ShortcutRuntime      shortcutservice.ShortcutRuntimeState

	preview  *theme.Appearance
	resolved theme.Resolved
}

// NewSettingsStore returns an unloaded settings store.
func NewSettingsStore(rt Runtime, deps SettingsDependencies) *SettingsStore {
	return &SettingsStore{rt: rt, deps: deps, dirtySections: map[SettingsSection]bool{}, clock: dispatcherClock{dispatcher: rt.Dispatcher}}
}

// Load reads the saved settings. It runs before the first window opens.
func (s *SettingsStore) Load() error {
	current, err := s.deps.Backend.Snapshot()
	if err != nil {
		return err
	}
	NormalizeSettings(current)
	s.Current = current
	s.ActiveFrameMode = sanitizeFrameMode(s.deps.Backend.GetActiveWindowFrameMode())
	if s.deps.Shortcuts != nil {
		s.ShortcutRuntime = s.deps.Shortcuts.GetShortcutRuntimeState()
	}
	if s.deps.Python != nil {
		if status, err := s.deps.Python.GetRuntimeStatus(); err == nil {
			s.PythonStatus = status
		}
	}
	s.refreshTheme()
	return nil
}

// Theme returns the appearance every window currently shows.
func (s *SettingsStore) Theme() theme.Resolved { return s.resolved }

// CodeFont is the current preview or saved code font, including its size.
func (s *SettingsStore) CodeFont() ui.Font {
	resolved := theme.Resolve(theme.Appearance{})
	if s != nil {
		resolved = s.resolved
	}
	return ui.Font{Family: resolved.CodeFont, Size: float32(theme.NormalizeCodeFontSize(resolved.CodeFontSize))}
}

func (s *SettingsStore) refreshTheme() {
	appearance := theme.Appearance{}
	if s.Current != nil {
		appearance = theme.AppearanceFromSettings(s.Current.CommonConfig)
	}
	if s.preview != nil {
		appearance = *s.preview
	}
	previousMode := s.resolved.Mode
	s.resolved = theme.Resolve(appearance)
	if s.resolved.Mode != previousMode && s.deps.ApplyThemeMode != nil {
		s.deps.ApplyThemeMode(s.resolved.Mode)
	}
}

// Language returns the saved language.
func (s *SettingsStore) Language() string {
	if s.Current == nil || s.Current.CommonConfig == nil {
		return locale.Chinese
	}
	return locale.Normalize(s.Current.CommonConfig.Language)
}

// UsesCustomFrame reports whether the running windows draw their own title bar.
func (s *SettingsStore) UsesCustomFrame() bool {
	return s.ActiveFrameMode == settingservice.WindowFrameModeCustom
}

// TrafficDetailLayout returns the saved detail panel layout.
func (s *SettingsStore) TrafficDetailLayout() settingservice.TrafficDetailLayout {
	if s.Current == nil || s.Current.TrafficDetailConfig == nil {
		return settingservice.TrafficDetailLayoutVertical
	}
	return normalizeDetailLayout(s.Current.TrafficDetailConfig.Layout)
}

// HiddenTrafficColumns returns the saved hidden column keys.
func (s *SettingsStore) HiddenTrafficColumns() []string {
	if s.Current == nil || s.Current.TrafficTableConfig == nil {
		return nil
	}
	return logic.NormalizeHiddenTrafficColumns(s.Current.TrafficTableConfig.HiddenColumns)
}

// BeginEdit gives the Settings window a private copy of the saved settings.
func (s *SettingsStore) BeginEdit() {
	s.Draft = CloneSettings(s.Current)
	if s.Draft != nil {
		NormalizeSettings(s.Draft)
	}
	s.baseline = CloneSettings(s.Draft)
	s.dirtySections = map[SettingsSection]bool{}
	s.LastProxyApplyResult = nil
	s.LastShortcutResult = nil
}

// EndEdit discards the draft and any appearance preview.
func (s *SettingsStore) EndEdit() {
	s.Draft = nil
	s.baseline = nil
	s.dirtySections = map[SettingsSection]bool{}
	s.EndPreview()
}

// SyncDirty marks each section whose draft differs from the saved state it
// started from, and clears the others.
func (s *SettingsStore) SyncDirty() {
	if s.Draft == nil || s.baseline == nil {
		return
	}
	for _, section := range editableSections {
		s.SetSectionDirty(section, !reflect.DeepEqual(sectionField(s.Draft, section), sectionField(s.baseline, section)))
	}
}

// unedited reports whether the user has left section of the draft as it
// started.
func (s *SettingsStore) unedited(section SettingsSection) bool {
	return s.Draft != nil && s.baseline != nil && !s.dirtySections[section] &&
		reflect.DeepEqual(sectionField(s.Draft, section), sectionField(s.baseline, section))
}

// replaceDraft installs a draft that a save refreshed from the saved
// settings. Sections the user has not edited take their new values into the
// baseline as well.
func (s *SettingsStore) replaceDraft(next *settingservice.Settings) {
	if s.baseline != nil {
		source := CloneSettings(next)
		for _, section := range editableSections {
			if s.unedited(section) {
				field := reflect.ValueOf(sectionField(s.baseline, section)).Elem()
				field.Set(reflect.ValueOf(sectionField(source, section)).Elem())
			}
		}
	}
	s.Draft = next
}

// MarkDirty records that the draft changed in section.
func (s *SettingsStore) MarkDirty(section SettingsSection) { s.SetSectionDirty(section, true) }

// SetSectionDirty records whether the draft has unsaved changes in section.
func (s *SettingsStore) SetSectionDirty(section SettingsSection, dirty bool) {
	if dirty {
		s.dirtySections[section] = true
	} else {
		delete(s.dirtySections, section)
	}
}

// Dirty reports whether the draft has unsaved changes.
func (s *SettingsStore) Dirty() bool { return len(s.dirtySections) > 0 }

// DirtySections returns the sections with unsaved changes in a stable order.
func (s *SettingsStore) DirtySections() []SettingsSection {
	sections := make([]SettingsSection, 0, len(s.dirtySections))
	for section := range s.dirtySections {
		sections = append(sections, section)
	}
	slices.Sort(sections)
	return sections
}

// PreviewDraftAppearance shows the draft's appearance in every window without
// saving it.
func (s *SettingsStore) PreviewDraftAppearance() {
	if s.Draft == nil || s.Draft.CommonConfig == nil {
		return
	}
	appearance := theme.AppearanceFromSettings(s.Draft.CommonConfig)
	if s.preview != nil && *s.preview == appearance {
		return
	}
	if s.preview == nil && s.Current != nil && appearance == theme.AppearanceFromSettings(s.Current.CommonConfig) {
		return
	}
	s.preview = &appearance
	s.refreshTheme()
	s.rt.Dispatcher.InvalidateAll()
}

// EndPreview returns every window to the saved appearance.
func (s *SettingsStore) EndPreview() {
	if s.preview == nil {
		return
	}
	s.preview = nil
	s.refreshTheme()
	s.rt.Dispatcher.InvalidateAll()
}

type saveOutcome struct {
	draft          *settingservice.Settings
	persisted      *settingservice.Settings
	complete       bool
	shortcutResult *shortcutservice.ShortcutApplyResult
	proxyResult    *proxyservice.ProxyConfigApplyResult
	pythonStatus   *pythonpluginservice.RuntimeStatus
	pythonUpdated  bool
}

// Save writes the draft's dirty sections. It reports false when a save is
// already running or there is nothing to save; done is then not called.
func (s *SettingsStore) Save(done func(SaveResult, error)) bool {
	if s.Saving || s.Draft == nil {
		return false
	}
	s.Saving = true
	draft := CloneSettings(s.Draft)
	sections := s.DirtySections()
	runtime.Run(s.rt.Jobs, settingsSaveJob, func(context.Context) (saveOutcome, error) {
		return s.persist(draft, sections)
	}, func(outcome saveOutcome, err error) {
		s.Saving = false
		s.applySaveOutcome(outcome, err)
		if done != nil {
			done(SaveResult{Complete: outcome.complete}, err)
		}
	})
	return true
}

func (s *SettingsStore) persist(draft *settingservice.Settings, sections []SettingsSection) (saveOutcome, error) {
	outcome := saveOutcome{draft: draft}
	var ordinary []SettingsSection
	for _, section := range sections {
		if section != SectionShortcuts && section != SectionPythonPlugins {
			ordinary = append(ordinary, section)
		}
	}
	var shortcutCandidate *settingservice.ShortcutConfig
	if slices.Contains(sections, SectionShortcuts) && draft.Shortcuts != nil {
		shortcutCandidate = cloneShortcuts(draft.Shortcuts)
	}
	var pythonCandidate *settingservice.PythonPluginConfig
	if slices.Contains(sections, SectionPythonPlugins) && draft.PythonPluginConfig != nil {
		copied := *draft.PythonPluginConfig
		pythonCandidate = &copied
	}

	latest, err := s.deps.Backend.Snapshot()
	if err != nil {
		return s.failSave(outcome, shortcutCandidate, err)
	}
	merged := MergeSettingsForSave(draft, latest, ordinary)
	outcome.draft = merged

	if len(ordinary) > 0 {
		if err := s.deps.Backend.UpdatePreservingShortcuts(CloneSettings(merged)); err != nil {
			return s.failSave(outcome, shortcutCandidate, err)
		}
		if err := s.deps.Backend.Save(); err != nil {
			return s.failSave(outcome, shortcutCandidate, err)
		}
		persisted, err := s.deps.Backend.Snapshot()
		if err != nil {
			return s.failSave(outcome, shortcutCandidate, err)
		}
		NormalizeSettings(persisted)
		outcome.draft = persisted
	}

	if pythonCandidate != nil {
		if err := s.configurePython(&outcome, *pythonCandidate); err != nil {
			return s.failSave(outcome, shortcutCandidate, err)
		}
	}

	outcome.complete = true
	outcome.persisted = CloneSettings(outcome.draft)
	if shortcutCandidate != nil {
		result, err := s.deps.Shortcuts.ApplyShortcutConfig(cloneShortcuts(shortcutCandidate))
		if err == nil && (result == nil || result.Config == nil) {
			err = errors.New("shortcut apply result is not available")
		}
		if err != nil {
			outcome.complete = false
			return s.failSave(outcome, shortcutCandidate, err)
		}
		outcome.shortcutResult = result
		outcome.persisted.Shortcuts = cloneShortcuts(result.Config)
		if result.Applied {
			outcome.draft.Shortcuts = cloneShortcuts(result.Config)
		} else {
			outcome.draft.Shortcuts = shortcutCandidate
			outcome.complete = false
		}
	}

	if slices.Contains(sections, SectionProxy) && s.deps.Proxy != nil {
		result, err := s.deps.Proxy.ApplyCurrentProxyConfig()
		if err != nil {
			outcome.complete = false
			return s.failSave(outcome, shortcutCandidate, err)
		}
		outcome.proxyResult = &result
	}
	return outcome, nil
}

func (s *SettingsStore) configurePython(outcome *saveOutcome, candidate settingservice.PythonPluginConfig) error {
	status, err := s.deps.Python.ConfigureRuntime(candidate)
	if err == nil && status == nil {
		err = errors.New("python runtime status is not available")
	}
	outcome.pythonUpdated = true
	if err != nil {
		enabled := false
		if refreshed, refreshErr := s.deps.Backend.Snapshot(); refreshErr == nil && refreshed.PythonPluginConfig != nil {
			enabled = refreshed.PythonPluginConfig.Enabled
		}
		failed := candidate
		failed.Enabled = enabled
		outcome.draft.PythonPluginConfig = &failed
		outcome.pythonStatus, _ = s.deps.Python.GetRuntimeStatus()
		return err
	}
	outcome.pythonStatus = status
	if refreshed, refreshErr := s.deps.Backend.Snapshot(); refreshErr == nil && refreshed.PythonPluginConfig != nil {
		outcome.draft.PythonPluginConfig = refreshed.PythonPluginConfig
	} else {
		applied := candidate
		outcome.draft.PythonPluginConfig = &applied
	}
	return nil
}

func (s *SettingsStore) failSave(outcome saveOutcome, shortcuts *settingservice.ShortcutConfig, err error) (saveOutcome, error) {
	if shortcuts != nil && outcome.draft != nil {
		outcome.draft.Shortcuts = shortcuts
	}
	return outcome, err
}

func (s *SettingsStore) applySaveOutcome(outcome saveOutcome, err error) {
	if outcome.pythonUpdated {
		s.PythonStatus = outcome.pythonStatus
	}
	if outcome.shortcutResult != nil {
		s.LastShortcutResult = outcome.shortcutResult
		if outcome.shortcutResult.Applied {
			s.ShortcutRuntime = outcome.shortcutResult.RuntimeState
		}
	}
	if s.Draft != nil && outcome.draft != nil {
		s.replaceDraft(outcome.draft)
	}
	if err != nil {
		return
	}
	s.LastProxyApplyResult = outcome.proxyResult
	if s.Draft != nil && outcome.complete {
		s.dirtySections = map[SettingsSection]bool{}
	} else if s.Draft != nil {
		s.dirtySections = map[SettingsSection]bool{SectionShortcuts: true}
	}
	s.Current = outcome.persisted
	if s.Draft != nil {
		NormalizeSettings(s.Draft)
		s.baseline = CloneSettings(s.Current)
		NormalizeSettings(s.baseline)
	}
	s.preview = nil
	s.refreshTheme()
	s.rt.Dispatcher.InvalidateAll()
}

// SetThemeMode saves a theme mode chosen outside the Settings window.
func (s *SettingsStore) SetThemeMode(mode string) error {
	if err := s.deps.Backend.SetThemeMode(mode); err != nil {
		return err
	}
	for _, settings := range []*settingservice.Settings{s.Current, s.Draft, s.baseline} {
		if settings != nil && settings.CommonConfig != nil {
			settings.CommonConfig.ThemeMode = mode
		}
	}
	if s.preview != nil {
		s.preview.Mode = mode
	}
	s.refreshTheme()
	s.rt.Dispatcher.InvalidateAll()
	return nil
}

// SetLanguage saves a language chosen outside the Settings window and shows
// the interface in it.
func (s *SettingsStore) SetLanguage(language string) error {
	if err := s.deps.Backend.SetLanguage(language); err != nil {
		return err
	}
	for _, settings := range []*settingservice.Settings{s.Current, s.Draft, s.baseline} {
		if settings != nil && settings.CommonConfig != nil {
			settings.CommonConfig.Language = language
		}
	}
	if s.deps.ApplyLocale != nil {
		s.deps.ApplyLocale(language)
	}
	s.rt.Dispatcher.InvalidateAll()
	return nil
}

// ToggleTrafficDetailLayout switches the detail panel layout and saves it. On
// failure the previous layout returns and done receives the error.
func (s *SettingsStore) ToggleTrafficDetailLayout(done func(error)) bool {
	if s.Current == nil || s.SavingTrafficDetailConfig {
		return false
	}
	previous := s.TrafficDetailLayout()
	next := settingservice.TrafficDetailLayoutVertical
	if previous == settingservice.TrafficDetailLayoutVertical {
		next = settingservice.TrafficDetailLayoutHorizontal
	}
	splits := s.trafficDetailSplits()
	s.Current.TrafficDetailConfig = &settingservice.TrafficDetailConfig{Layout: next, Splits: splits}
	s.SavingTrafficDetailConfig = true
	runtime.Run(s.rt.Jobs, trafficDetailJob, func(context.Context) (struct{}, error) {
		return struct{}{}, s.deps.Backend.SaveTrafficDetailConfig(&settingservice.TrafficDetailConfig{Layout: next, Splits: splits})
	}, func(_ struct{}, err error) {
		s.SavingTrafficDetailConfig = false
		if err != nil {
			s.Current.TrafficDetailConfig = &settingservice.TrafficDetailConfig{Layout: previous, Splits: splits}
		}
		if done != nil {
			done(err)
		}
	})
	return true
}

// SetTrafficColumnVisible shows or hides a column and saves the choice. The
// last visible column cannot be hidden.
func (s *SettingsStore) SetTrafficColumnVisible(key logic.TrafficColumnKey, visible bool, done func(error)) bool {
	hidden := s.HiddenTrafficColumns()
	name := string(key)
	if visible {
		hidden = slices.DeleteFunc(hidden, func(candidate string) bool { return candidate == name })
	} else {
		if !slices.Contains(hidden, name) && len(logic.TrafficColumnKeys)-len(hidden) <= 1 {
			return false
		}
		hidden = append(hidden, name)
	}
	return s.replaceHiddenColumns(hidden, done)
}

// ShowAllTrafficColumns shows every column and saves the choice.
func (s *SettingsStore) ShowAllTrafficColumns(done func(error)) bool {
	return s.replaceHiddenColumns(nil, done)
}

func (s *SettingsStore) replaceHiddenColumns(values []string, done func(error)) bool {
	if s.Current == nil || s.SavingTrafficTableConfig {
		return false
	}
	previous := s.HiddenTrafficColumns()
	next := logic.NormalizeHiddenTrafficColumns(values)
	if slices.Equal(previous, next) {
		return true
	}
	order, widths := s.TrafficColumnLayout()
	s.Current.TrafficTableConfig = &settingservice.TrafficTableConfig{HiddenColumns: next, ColumnOrder: order, ColumnWidths: widths}
	s.saveTrafficTableConfig(func(err error) {
		if err != nil {
			s.Current.TrafficTableConfig = &settingservice.TrafficTableConfig{HiddenColumns: previous, ColumnOrder: order, ColumnWidths: widths}
		}
		if done != nil {
			done(err)
		}
	})
	return true
}

// TrafficColumnLayout returns the saved column order and widths.
func (s *SettingsStore) TrafficColumnLayout() ([]string, map[string]float32) {
	if s.Current == nil || s.Current.TrafficTableConfig == nil {
		return nil, nil
	}
	config := s.Current.TrafficTableConfig
	return slices.Clone(config.ColumnOrder), maps.Clone(config.ColumnWidths)
}

// SetTrafficColumnLayout saves the column order and widths. A save running
// already is followed by another with the latest layout, so a drag produces
// few writes.
func (s *SettingsStore) SetTrafficColumnLayout(order []string, widths map[string]float32) {
	if s.Current == nil {
		return
	}
	current, currentWidths := s.TrafficColumnLayout()
	if slices.Equal(current, order) && maps.Equal(currentWidths, widths) {
		return
	}
	s.Current.TrafficTableConfig = &settingservice.TrafficTableConfig{
		HiddenColumns: s.HiddenTrafficColumns(), ColumnOrder: slices.Clone(order), ColumnWidths: maps.Clone(widths),
	}
	if s.SavingTrafficTableConfig {
		s.columnLayoutDirty = true
		return
	}
	s.saveTrafficTableConfig(nil)
}

func (s *SettingsStore) saveTrafficTableConfig(done func(error)) {
	config := *s.Current.TrafficTableConfig
	config.HiddenColumns = slices.Clone(config.HiddenColumns)
	config.ColumnOrder = slices.Clone(config.ColumnOrder)
	config.ColumnWidths = maps.Clone(config.ColumnWidths)
	s.SavingTrafficTableConfig = true
	s.columnLayoutDirty = false
	runtime.Run(s.rt.Jobs, trafficTableJob, func(context.Context) (struct{}, error) {
		return struct{}{}, s.deps.Backend.SaveTrafficTableConfig(&config)
	}, func(_ struct{}, err error) {
		s.SavingTrafficTableConfig = false
		if done != nil {
			done(err)
		}
		if s.columnLayoutDirty && s.Current != nil {
			s.saveTrafficTableConfig(nil)
		}
	})
}

// UpdateProxyConfig changes the saved proxy configuration from outside the
// Settings window and applies it to the running proxy. It reports false when
// a save is running; done is then not called.
func (s *SettingsStore) UpdateProxyConfig(mutate func(*settingservice.ProxyConfig), done func(*proxyservice.ProxyConfigApplyResult, error)) bool {
	if s.Current == nil || s.Saving {
		return false
	}
	draft := CloneSettings(s.Current)
	NormalizeSettings(draft)
	mutate(draft.ProxyConfig)
	s.Saving = true
	runtime.Run(s.rt.Jobs, settingsSaveJob, func(context.Context) (saveOutcome, error) {
		return s.persist(draft, []SettingsSection{SectionProxy})
	}, func(outcome saveOutcome, err error) {
		s.Saving = false
		if err == nil {
			s.LastProxyApplyResult = outcome.proxyResult
			s.Current = outcome.persisted
			if s.unedited(SectionProxy) {
				s.Draft.ProxyConfig = CloneSettings(outcome.persisted).ProxyConfig
				s.baseline.ProxyConfig = CloneSettings(outcome.persisted).ProxyConfig
			}
			s.rt.Dispatcher.InvalidateAll()
		}
		if done != nil {
			done(outcome.proxyResult, err)
		}
	})
	return true
}

func (s *SettingsStore) trafficDetailSplits() map[string]float32 {
	if s.Current == nil || s.Current.TrafficDetailConfig == nil {
		return nil
	}
	return maps.Clone(s.Current.TrafficDetailConfig.Splits)
}

// TrafficDetailSplit returns a saved split percentage, or fallback.
func (s *SettingsStore) TrafficDetailSplit(name string, fallback float32) float32 {
	if s.Current != nil && s.Current.TrafficDetailConfig != nil {
		if percent, ok := s.Current.TrafficDetailConfig.Splits[name]; ok {
			return percent
		}
	}
	return fallback
}

// SetTrafficDetailSplit records a split percentage and saves it shortly after
// the last change, so dragging a divider writes once.
func (s *SettingsStore) SetTrafficDetailSplit(name string, percent float32) {
	if s.Current == nil {
		return
	}
	splits := s.trafficDetailSplits()
	if splits == nil {
		splits = map[string]float32{}
	}
	if splits[name] == percent {
		return
	}
	splits[name] = percent
	s.Current.TrafficDetailConfig = &settingservice.TrafficDetailConfig{Layout: s.TrafficDetailLayout(), Splits: splits}
	s.scheduleDetailSave()
}

func (s *SettingsStore) scheduleDetailSave() {
	if s.stopDetailSave != nil {
		s.stopDetailSave()
	}
	s.stopDetailSave = s.clock.After(detailSaveDelay, func() {
		s.stopDetailSave = nil
		if s.SavingTrafficDetailConfig || s.Current == nil {
			s.scheduleDetailSave()
			return
		}
		config := settingservice.TrafficDetailConfig{Layout: s.TrafficDetailLayout(), Splits: s.trafficDetailSplits()}
		s.SavingTrafficDetailConfig = true
		runtime.Run(s.rt.Jobs, trafficDetailJob, func(context.Context) (struct{}, error) {
			return struct{}{}, s.deps.Backend.SaveTrafficDetailConfig(&config)
		}, func(_ struct{}, _ error) { s.SavingTrafficDetailConfig = false })
	})
}

// CloneSettings returns an independent copy.
func CloneSettings(settings *settingservice.Settings) *settingservice.Settings {
	if settings == nil {
		return nil
	}
	data, err := json.Marshal(settings)
	if err != nil {
		panic(fmt.Sprintf("copy settings: %v", err))
	}
	copied := new(settingservice.Settings)
	if err := json.Unmarshal(data, copied); err != nil {
		panic(fmt.Sprintf("copy settings: %v", err))
	}
	return copied
}

func cloneShortcuts(config *settingservice.ShortcutConfig) *settingservice.ShortcutConfig {
	data, err := json.Marshal(config)
	if err != nil {
		panic(fmt.Sprintf("copy shortcuts: %v", err))
	}
	copied := new(settingservice.ShortcutConfig)
	if err := json.Unmarshal(data, copied); err != nil {
		panic(fmt.Sprintf("copy shortcuts: %v", err))
	}
	return copied
}

// MergeSettingsForSave starts from the latest saved settings and takes only
// the draft's dirty sections, so preferences saved elsewhere are not lost.
func MergeSettingsForSave(draft, latest *settingservice.Settings, sections []SettingsSection) *settingservice.Settings {
	if latest == nil {
		return draft
	}
	NormalizeSettings(draft)
	NormalizeSettings(latest)
	merged := CloneSettings(latest)
	copied := CloneSettings(draft)
	has := func(section SettingsSection) bool { return slices.Contains(sections, section) }

	if has(SectionCommon) {
		merged.CommonConfig = copied.CommonConfig
		merged.CommonConfig.ThemeMode = latest.CommonConfig.ThemeMode
		merged.CommonConfig.Language = latest.CommonConfig.Language
	}
	if has(SectionProxy) {
		merged.ProxyConfig = copied.ProxyConfig
	}
	if has(SectionCache) {
		merged.CacheConfig = copied.CacheConfig
	}
	if has(SectionHistoryRetention) {
		merged.HistoryRetentionConfig = copied.HistoryRetentionConfig
	}
	if has(SectionProcessAttribution) {
		merged.ProcessAttributionConfig = copied.ProcessAttributionConfig
	}
	if has(SectionWindow) {
		merged.WindowConfig.FrameMode = copied.WindowConfig.FrameMode
		merged.WindowConfig.MainWindowCloseBehavior = copied.WindowConfig.MainWindowCloseBehavior
	}
	if has(SectionTrafficTable) {
		merged.TrafficTableConfig = copied.TrafficTableConfig
	}
	if has(SectionShortcuts) {
		merged.Shortcuts = copied.Shortcuts
	}
	return merged
}

func sanitizeFrameMode(mode settingservice.WindowFrameMode) settingservice.WindowFrameMode {
	if mode == settingservice.WindowFrameModeSystem {
		return mode
	}
	return settingservice.WindowFrameModeCustom
}

func normalizeDetailLayout(layout settingservice.TrafficDetailLayout) settingservice.TrafficDetailLayout {
	if layout == settingservice.TrafficDetailLayoutHorizontal {
		return layout
	}
	return settingservice.TrafficDetailLayoutVertical
}

// NormalizeSettings fills missing sections and replaces invalid values with
// their defaults.
func NormalizeSettings(s *settingservice.Settings) {
	if s.CommonConfig == nil {
		s.CommonConfig = &settingservice.CommonConfig{
			LogLevel: defaultLogLevel, ThemeMode: theme.ModeLight, Language: locale.Chinese,
		}
	}
	common := s.CommonConfig
	if common.LogLevel == "" {
		common.LogLevel = defaultLogLevel
	}
	common.AppFontSize = theme.NormalizeAppFontSize(common.AppFontSize)
	common.CodeFontSize = theme.NormalizeCodeFontSize(common.CodeFontSize)
	common.ThemePrimaryColor = theme.NormalizePrimaryColor(common.ThemePrimaryColor)
	common.ThemeNeutralColor = theme.NormalizeNeutralColor(common.ThemeNeutralColor)

	if s.WindowConfig == nil {
		s.WindowConfig = &settingservice.WindowConfig{}
	}
	s.WindowConfig.FrameMode = sanitizeFrameMode(s.WindowConfig.FrameMode)
	if s.WindowConfig.MainWindowCloseBehavior != settingservice.MainWindowCloseBehaviorQuit {
		s.WindowConfig.MainWindowCloseBehavior = settingservice.MainWindowCloseBehaviorHideToTray
	}

	if s.ProxyConfig == nil {
		s.ProxyConfig = &settingservice.ProxyConfig{
			Mode: settingservice.ProxyModeHTTP, Host: defaultProxyAddress, Port: defaultProxyPort,
			UseBuiltinCA: true, UpstreamMode: settingservice.UpstreamProxyModeSystem,
			DNSCacheTTLSeconds: defaultProxyDNSTTL,
		}
	}
	proxy := s.ProxyConfig
	proxy.UpstreamMode = resolveUpstreamMode(proxy)
	if proxy.IncludeHosts == nil {
		proxy.IncludeHosts = []string{}
	}
	if proxy.ExcludeHosts == nil {
		proxy.ExcludeHosts = []string{}
	}
	if proxy.RootCAPaths == nil {
		proxy.RootCAPaths = []string{}
	}
	if proxy.ClientCerts == nil {
		proxy.ClientCerts = []settingservice.ClientCertConfig{}
	}

	if s.CacheConfig == nil {
		s.CacheConfig = &settingservice.CacheConfig{
			BodyCacheThresholdBytes: defaultBodyCache, MaxWsMessages: defaultWSMessages,
		}
	}

	if s.HistoryRetentionConfig == nil {
		s.HistoryRetentionConfig = &settingservice.HistoryRetentionConfig{
			Value: defaultRetention, Unit: settingservice.HistoryRetentionUnitDay,
		}
	}
	retention := s.HistoryRetentionConfig
	if retention.Value < minRetention || retention.Value > maxRetention {
		retention.Value = defaultRetention
	}
	switch retention.Unit {
	case settingservice.HistoryRetentionUnitHour, settingservice.HistoryRetentionUnitDay,
		settingservice.HistoryRetentionUnitWeek, settingservice.HistoryRetentionUnitMonth,
		settingservice.HistoryRetentionUnitYear:
	default:
		retention.Unit = settingservice.HistoryRetentionUnitDay
	}

	if s.ProcessAttributionConfig == nil {
		s.ProcessAttributionConfig = &settingservice.ProcessAttributionConfig{Enabled: true}
	}
	if s.TrafficTableConfig == nil {
		s.TrafficTableConfig = &settingservice.TrafficTableConfig{}
	}
	s.TrafficTableConfig.HiddenColumns = logic.NormalizeHiddenTrafficColumns(s.TrafficTableConfig.HiddenColumns)
	if s.TrafficDetailConfig == nil {
		s.TrafficDetailConfig = &settingservice.TrafficDetailConfig{}
	}
	s.TrafficDetailConfig.Layout = normalizeDetailLayout(s.TrafficDetailConfig.Layout)
	if s.PythonPluginConfig == nil {
		s.PythonPluginConfig = &settingservice.PythonPluginConfig{HookTimeoutMs: defaultHookTimeout}
	}
	if s.Shortcuts == nil {
		s.Shortcuts = &settingservice.ShortcutConfig{}
	}
	if s.Shortcuts.Overrides == nil {
		s.Shortcuts.Overrides = map[string]settingservice.ShortcutOverride{}
	}
}

func resolveUpstreamMode(config *settingservice.ProxyConfig) settingservice.UpstreamProxyMode {
	switch config.UpstreamMode {
	case settingservice.UpstreamProxyModeNone, settingservice.UpstreamProxyModeSystem, settingservice.UpstreamProxyModeCustom:
		return config.UpstreamMode
	}
	if config.UpstreamProxy != "" {
		return settingservice.UpstreamProxyModeCustom
	}
	if config.DisableProxy {
		return settingservice.UpstreamProxyModeNone
	}
	return settingservice.UpstreamProxyModeSystem
}

// ShortcutsChanged records system-wide shortcut registration results.
func (s *SettingsStore) ShortcutsChanged(result shortcutservice.ShortcutApplyResult) {
	s.ShortcutRuntime = result.RuntimeState
	if result.Config != nil && s.Current != nil {
		s.Current.Shortcuts = cloneShortcuts(result.Config)
	}
}

// ResetDraftToDefaults fills the draft with the default values. Saved
// preferences outside the editable sections stay as they are.
func (s *SettingsStore) ResetDraftToDefaults(shortcutIDs []string) {
	draft := s.Draft
	if draft == nil {
		return
	}
	NormalizeSettings(draft)
	common := draft.CommonConfig
	common.DisableAnimations = false
	common.LogLevel, common.LogDisabled, common.AppFontFamily, common.CodeFontFamily = defaultLogLevel, false, "", ""
	common.AppFontSize, common.CodeFontSize = theme.DefaultAppFontSize, theme.DefaultCodeFontSize
	common.ThemePrimaryColor, common.ThemeNeutralColor = theme.DefaultPrimaryColor, theme.DefaultNeutralColor
	draft.WindowConfig.FrameMode = sanitizeFrameMode("")
	draft.WindowConfig.MainWindowCloseBehavior = settingservice.MainWindowCloseBehaviorHideToTray
	proxy := draft.ProxyConfig
	proxy.Mode, proxy.Host, proxy.Port = settingservice.ProxyModeHTTP, defaultProxyAddress, defaultProxyPort
	proxy.UseBuiltinCA, proxy.CACertPath, proxy.CAKeyPath = true, "", ""
	proxy.UpstreamMode, proxy.UpstreamProxy, proxy.DisableProxy = settingservice.UpstreamProxyModeSystem, "", false
	proxy.DisableHTTP2, proxy.SkipVerifyTLS, proxy.AntiCache, proxy.AntiComp = false, false, false, false
	proxy.IncludeHosts, proxy.ExcludeHosts, proxy.RootCAPaths, proxy.ClientCerts = []string{}, []string{}, []string{}, []settingservice.ClientCertConfig{}
	draft.CacheConfig.BodyCacheThresholdBytes, draft.CacheConfig.MaxWsMessages = defaultBodyCache, defaultWSMessages
	draft.HistoryRetentionConfig.Enabled, draft.HistoryRetentionConfig.Value, draft.HistoryRetentionConfig.Unit = false, defaultRetention, settingservice.HistoryRetentionUnitDay
	draft.ProcessAttributionConfig.Enabled = true
	draft.TrafficTableConfig.HiddenColumns = []string{}
	draft.PythonPluginConfig.Enabled, draft.PythonPluginConfig.InterpreterPath, draft.PythonPluginConfig.HookTimeoutMs = false, "", defaultHookTimeout
	for _, id := range shortcutIDs {
		delete(draft.Shortcuts.Overrides, id)
	}
	for _, section := range editableSections {
		s.dirtySections[section] = true
	}
	s.LastProxyApplyResult, s.LastShortcutResult = nil, nil
}
