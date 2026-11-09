package store

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/josexy/flowlens/backend/pkg/database"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/backend/services/shortcut_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
)

type harness struct {
	store   *SettingsStore
	service *settingservice.SettingService
	window  *runtimetest.Window
	rt      Runtime
}

func (h *harness) settle() {
	h.rt.Jobs.Wait()
	h.window.Drain()
}

type fakeShortcuts struct {
	applied bool
	err     error
	calls   int
}

func (f *fakeShortcuts) ApplyShortcutConfig(config *settingservice.ShortcutConfig) (*shortcutservice.ShortcutApplyResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &shortcutservice.ShortcutApplyResult{Applied: f.applied, Config: &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{}}}, nil
}

func (f *fakeShortcuts) GetShortcutRuntimeState() shortcutservice.ShortcutRuntimeState {
	return shortcutservice.ShortcutRuntimeState{}
}

type fakeProxy struct{ calls int }

func (f *fakeProxy) ApplyCurrentProxyConfig() (proxyservice.ProxyConfigApplyResult, error) {
	f.calls++
	return proxyservice.ProxyConfigApplyResult{Applied: true}, nil
}

func newHarness(t *testing.T, shortcuts ShortcutApplier, proxy ProxyConfigApplier) *harness {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := settingservice.New(db)
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	store := NewSettingsStore(rt, SettingsDependencies{Backend: service, Shortcuts: shortcuts, Proxy: proxy})
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return &harness{store: store, service: service, window: window, rt: rt}
}

func TestDraftIsIndependentOfCurrent(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.Draft.ProxyConfig.Port = 1
	if h.store.Current.ProxyConfig.Port == 1 {
		t.Fatal("editing the draft changed the saved settings")
	}
}

func TestNewerPreferencesSurviveASettingsSave(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()

	// Other surfaces write narrow preferences after the Settings window opened.
	if err := h.service.SaveTrafficDetailConfig(&settingservice.TrafficDetailConfig{Layout: settingservice.TrafficDetailLayoutHorizontal}); err != nil {
		t.Fatal(err)
	}
	python := settingservice.PythonPluginConfig{Enabled: true, InterpreterPath: "/usr/bin/python3", HookTimeoutMs: 1234}
	if err := h.service.SavePythonPluginConfig(&python); err != nil {
		t.Fatal(err)
	}
	shortcuts := &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		"app.save": {Binding: nil, Scope: settingservice.ShortcutScopeApplication},
	}}
	if err := h.service.SaveShortcutConfig(shortcuts); err != nil {
		t.Fatal(err)
	}

	h.store.Draft.CommonConfig.AppFontSize = 18
	h.store.MarkDirty(SectionCommon)
	var saveErr error
	if !h.store.Save(func(_ SaveResult, err error) { saveErr = err }) {
		t.Fatal("Save refused")
	}
	h.settle()
	if saveErr != nil {
		t.Fatal(saveErr)
	}

	got, err := h.service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.CommonConfig.AppFontSize != 18 {
		t.Fatalf("saved font size = %d", got.CommonConfig.AppFontSize)
	}
	if got.TrafficDetailConfig.Layout != settingservice.TrafficDetailLayoutHorizontal {
		t.Fatalf("detail layout = %q", got.TrafficDetailConfig.Layout)
	}
	if got.PythonPluginConfig.InterpreterPath != "/usr/bin/python3" || got.PythonPluginConfig.HookTimeoutMs != 1234 {
		t.Fatalf("python = %+v", got.PythonPluginConfig)
	}
	if _, ok := got.Shortcuts.Overrides["app.save"]; !ok {
		t.Fatalf("shortcuts = %+v", got.Shortcuts)
	}
	if h.store.Current.TrafficDetailConfig.Layout != settingservice.TrafficDetailLayoutHorizontal {
		t.Fatal("the window's saved state lost the newer layout")
	}
}

func TestSavingCommonKeepsLatestModeAndLanguageButTakesColors(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	if err := h.service.SetThemeMode("dark"); err != nil {
		t.Fatal(err)
	}
	if err := h.service.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	h.store.Draft.CommonConfig.ThemePrimaryColor = "rose"
	h.store.Draft.CommonConfig.ThemeNeutralColor = "zinc"
	h.store.MarkDirty(SectionCommon)
	h.store.Save(nil)
	h.settle()
	got, _ := h.service.Snapshot()
	common := got.CommonConfig
	if common.ThemeMode != "dark" || common.Language != "en" || common.ThemePrimaryColor != "rose" || common.ThemeNeutralColor != "zinc" {
		t.Fatalf("common = %+v", common)
	}
}

func TestOnlyDirtySectionsAreWritten(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.Draft.ProxyConfig.Port = 9000
	h.store.Draft.CommonConfig.AppFontSize = 20
	h.store.MarkDirty(SectionCommon)
	h.store.Save(nil)
	h.settle()
	got, _ := h.service.Snapshot()
	if got.ProxyConfig.Port == 9000 || got.CommonConfig.AppFontSize != 20 {
		t.Fatalf("port %d, font %d", got.ProxyConfig.Port, got.CommonConfig.AppFontSize)
	}
}

func TestWindowSectionKeepsCurrentGeometry(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	if err := h.service.UpdateWindowConfig(&settingservice.WindowConfig{Width: 1500, Height: 900}); err != nil {
		t.Fatal(err)
	}
	h.store.Draft.WindowConfig.Width = 1
	h.store.Draft.WindowConfig.FrameMode = settingservice.WindowFrameModeSystem
	h.store.Draft.WindowConfig.MainWindowCloseBehavior = settingservice.MainWindowCloseBehaviorQuit
	h.store.MarkDirty(SectionWindow)
	h.store.Save(nil)
	h.settle()
	got, _ := h.service.Snapshot()
	if got.WindowConfig.Width != 1500 || got.WindowConfig.FrameMode != settingservice.WindowFrameModeSystem || got.WindowConfig.MainWindowCloseBehavior != settingservice.MainWindowCloseBehaviorQuit {
		t.Fatalf("window = %+v", got.WindowConfig)
	}
}

func TestDuplicateSavesAreIgnoredWhileBusy(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.MarkDirty(SectionCommon)
	count := 0
	if !h.store.Save(func(SaveResult, error) { count++ }) {
		t.Fatal("first Save refused")
	}
	if h.store.Save(func(SaveResult, error) { count++ }) {
		t.Fatal("second Save accepted")
	}
	if !h.store.Saving {
		t.Fatal("store not busy during save")
	}
	h.settle()
	if h.store.Saving || count != 1 || h.store.Dirty() {
		t.Fatalf("saving=%v count=%d dirty=%v", h.store.Saving, count, h.store.Dirty())
	}
}

func TestPreviewAppliesEverywhereAndDiscardRestoresSavedPalette(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.Draft.CommonConfig.ThemePrimaryColor = "rose"
	h.store.PreviewDraftAppearance()
	if h.store.Theme().PrimaryColor != "rose" {
		t.Fatalf("preview = %q", h.store.Theme().PrimaryColor)
	}
	if h.window.Invalidated() == 0 {
		t.Fatal("preview did not redraw windows")
	}
	h.store.EndEdit()
	if h.store.Theme().PrimaryColor != "blue" {
		t.Fatalf("after discard = %q", h.store.Theme().PrimaryColor)
	}
}

func TestSaveKeepsPreviewedPaletteAsTheNewBaseline(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.Draft.CommonConfig.ThemePrimaryColor = "rose"
	h.store.MarkDirty(SectionCommon)
	h.store.PreviewDraftAppearance()
	h.store.Save(nil)
	h.settle()
	h.store.EndEdit()
	if h.store.Theme().PrimaryColor != "rose" {
		t.Fatalf("after save and close = %q", h.store.Theme().PrimaryColor)
	}
}

func TestFontSizesPreviewAndSaveIndependently(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.BeginEdit()
	h.store.Draft.CommonConfig.AppFontSize = 20
	h.store.PreviewDraftAppearance()
	if got := h.store.Theme(); got.AppFontSize != 20 || got.CodeFontSize != 13 {
		t.Fatalf("preview: %d/%d", got.AppFontSize, got.CodeFontSize)
	}
	h.store.EndEdit()
	if got := h.store.Theme(); got.AppFontSize != 16 {
		t.Fatalf("after discard: %d", got.AppFontSize)
	}
}

func TestInvalidFontSizesAreNormalized(t *testing.T) {
	settings := &settingservice.Settings{CommonConfig: &settingservice.CommonConfig{AppFontSize: 99, CodeFontSize: 1}}
	NormalizeSettings(settings)
	if settings.CommonConfig.AppFontSize != 16 || settings.CommonConfig.CodeFontSize != 13 {
		t.Fatalf("common = %+v", settings.CommonConfig)
	}
}

func TestNormalizeFillsMissingSections(t *testing.T) {
	settings := &settingservice.Settings{}
	NormalizeSettings(settings)
	if settings.ProxyConfig.Port != 8080 || !settings.ProxyConfig.UseBuiltinCA || settings.CacheConfig.MaxWsMessages != 1000 ||
		settings.HistoryRetentionConfig.Value != 7 || !settings.ProcessAttributionConfig.Enabled || settings.PythonPluginConfig.HookTimeoutMs != 5000 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestUpstreamModeIsInferredFromOlderSettings(t *testing.T) {
	for _, test := range []struct {
		config settingservice.ProxyConfig
		want   settingservice.UpstreamProxyMode
	}{
		{settingservice.ProxyConfig{UpstreamProxy: "http://p"}, settingservice.UpstreamProxyModeCustom},
		{settingservice.ProxyConfig{DisableProxy: true}, settingservice.UpstreamProxyModeNone},
		{settingservice.ProxyConfig{}, settingservice.UpstreamProxyModeSystem},
		{settingservice.ProxyConfig{UpstreamMode: settingservice.UpstreamProxyModeNone, UpstreamProxy: "x"}, settingservice.UpstreamProxyModeNone},
	} {
		config := test.config
		if got := resolveUpstreamMode(&config); got != test.want {
			t.Errorf("%+v: got %q, want %q", test.config, got, test.want)
		}
	}
}

func TestFailedShortcutApplyKeepsDraftDirty(t *testing.T) {
	boom := errors.New("registration failed")
	h := newHarness(t, &fakeShortcuts{err: boom}, nil)
	h.store.BeginEdit()
	h.store.Draft.Shortcuts.Overrides["app.save"] = settingservice.ShortcutOverride{Scope: settingservice.ShortcutScopeApplication}
	h.store.MarkDirty(SectionShortcuts)
	var got error
	h.store.Save(func(_ SaveResult, err error) { got = err })
	h.settle()
	if !errors.Is(got, boom) {
		t.Fatalf("error = %v", got)
	}
	if !h.store.Dirty() {
		t.Fatal("failed save cleared the dirty state")
	}
	if _, ok := h.store.Draft.Shortcuts.Overrides["app.save"]; !ok {
		t.Fatal("failed save lost the edit")
	}
	if _, ok := h.store.Current.Shortcuts.Overrides["app.save"]; ok {
		t.Fatal("failed save changed the saved settings")
	}
}

func TestPartiallyAppliedShortcutsStayDirty(t *testing.T) {
	h := newHarness(t, &fakeShortcuts{applied: false}, nil)
	h.store.BeginEdit()
	h.store.MarkDirty(SectionShortcuts)
	var result SaveResult
	h.store.Save(func(r SaveResult, _ error) { result = r })
	h.settle()
	if result.Complete || !h.store.Dirty() {
		t.Fatalf("complete=%v dirty=%v", result.Complete, h.store.Dirty())
	}
}

func TestProxyConfigIsAppliedOnlyWhenProxySectionChanged(t *testing.T) {
	proxy := &fakeProxy{}
	h := newHarness(t, nil, proxy)
	h.store.BeginEdit()
	h.store.MarkDirty(SectionCommon)
	h.store.Save(nil)
	h.settle()
	if proxy.calls != 0 {
		t.Fatalf("applied %d times", proxy.calls)
	}
	h.store.MarkDirty(SectionProxy)
	h.store.Save(nil)
	h.settle()
	if proxy.calls != 1 || h.store.LastProxyApplyResult == nil {
		t.Fatalf("calls=%d result=%v", proxy.calls, h.store.LastProxyApplyResult)
	}
}

func TestThemeModeAndLanguageUpdateCurrentAndDraft(t *testing.T) {
	h := newHarness(t, nil, nil)
	var language string
	h.store.deps.ApplyLocale = func(value string) { language = value }
	h.store.BeginEdit()
	if err := h.store.SetThemeMode("dark"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if h.store.Current.CommonConfig.ThemeMode != "dark" || h.store.Draft.CommonConfig.Language != "en" || language != "en" || h.store.Language() != "en" {
		t.Fatalf("current=%+v draft=%+v language=%q", h.store.Current.CommonConfig, h.store.Draft.CommonConfig, language)
	}
	if h.store.Theme().Mode != "dark" {
		t.Fatalf("theme = %q", h.store.Theme().Mode)
	}
}

func TestTrafficDetailLayoutToggleSaves(t *testing.T) {
	h := newHarness(t, nil, nil)
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutVertical {
		t.Fatal("default layout")
	}
	h.store.ToggleTrafficDetailLayout(nil)
	h.settle()
	got, _ := h.service.Snapshot()
	if got.TrafficDetailConfig.Layout != settingservice.TrafficDetailLayoutHorizontal || h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutHorizontal {
		t.Fatalf("layout = %q", got.TrafficDetailConfig.Layout)
	}
}

func TestLastVisibleColumnCannotBeHidden(t *testing.T) {
	h := newHarness(t, nil, nil)
	for _, key := range logic.TrafficColumnKeys[1:] {
		if !h.store.SetTrafficColumnVisible(key, false, nil) {
			t.Fatalf("hide %q refused", key)
		}
		h.settle()
	}
	if h.store.SetTrafficColumnVisible(logic.TrafficColumnKeys[0], false, nil) {
		t.Fatal("last visible column was hidden")
	}
	h.store.ShowAllTrafficColumns(nil)
	h.settle()
	if hidden := h.store.HiddenTrafficColumns(); len(hidden) != 0 {
		t.Fatalf("hidden = %v", hidden)
	}
	got, _ := h.service.Snapshot()
	if !slices.Equal(got.TrafficTableConfig.HiddenColumns, []string{}) && got.TrafficTableConfig.HiddenColumns != nil {
		t.Fatalf("saved hidden = %v", got.TrafficTableConfig.HiddenColumns)
	}
}

func TestDetailLayoutTogglesAndReloads(t *testing.T) {
	h := newHarness(t, nil, nil)
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutVertical {
		t.Fatal(h.store.TrafficDetailLayout())
	}
	var saved error
	if !h.store.ToggleTrafficDetailLayout(func(err error) { saved = err }) {
		t.Fatal("toggle")
	}
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutHorizontal || !h.store.SavingTrafficDetailConfig {
		t.Fatal("optimistic layout")
	}
	if h.store.ToggleTrafficDetailLayout(nil) {
		t.Fatal("overlapping toggle")
	}
	h.settle()
	if saved != nil || h.store.SavingTrafficDetailConfig {
		t.Fatal(saved)
	}
	if err := h.store.Load(); err != nil {
		t.Fatal(err)
	}
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutHorizontal {
		t.Fatal("reload")
	}
}

type failingDetailSave struct {
	SettingsBackend
	err error
}

func (f failingDetailSave) SaveTrafficDetailConfig(*settingservice.TrafficDetailConfig) error {
	return f.err
}

func TestFailedDetailLayoutSaveRestoresAndKeepsDraft(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.deps.Backend = failingDetailSave{SettingsBackend: h.service, err: errors.New("database unavailable")}
	h.store.BeginEdit()
	h.store.Draft.CommonConfig.LogLevel = "debug"
	h.store.MarkDirty(SectionCommon)
	var saved error
	h.store.ToggleTrafficDetailLayout(func(err error) { saved = err })
	h.settle()
	if saved == nil || h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutVertical || !h.store.Dirty() || h.store.Draft.CommonConfig.LogLevel != "debug" {
		t.Fatalf("%v %s %v", saved, h.store.TrafficDetailLayout(), h.store.Dirty())
	}
}

func TestInvalidDetailLayoutUsesVertical(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.Current.TrafficDetailConfig = &settingservice.TrafficDetailConfig{Layout: "diagonal"}
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutVertical {
		t.Fatal(h.store.TrafficDetailLayout())
	}
	h.store.Current.TrafficDetailConfig = nil
	if h.store.TrafficDetailLayout() != settingservice.TrafficDetailLayoutVertical {
		t.Fatal("missing")
	}
}

func TestDetailSplitsSurviveLayoutToggle(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.store.SetTrafficDetailSplit("requestResponse:vertical", 62)
	h.store.ToggleTrafficDetailLayout(nil)
	h.settle()
	if got := h.store.TrafficDetailSplit("requestResponse:vertical", 0); got != 62 {
		t.Fatal(got)
	}
	snapshot, err := h.service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TrafficDetailConfig.Splits["requestResponse:vertical"] != 62 {
		t.Fatalf("%+v", snapshot.TrafficDetailConfig)
	}
}
