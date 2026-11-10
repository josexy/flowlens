package settings

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/pkg/database"
	"github.com/josexy/flowlens/backend/pkg/logger"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/backend/services/shortcut_service"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
)

type shortcutApplier struct {
	service *settingservice.SettingService
	fail    error
	applied int
}

func (s *shortcutApplier) ApplyShortcutConfig(config *settingservice.ShortcutConfig) (*shortcutservice.ShortcutApplyResult, error) {
	s.applied++
	if s.fail != nil {
		return nil, s.fail
	}
	if err := s.service.SaveShortcutConfig(config); err != nil {
		return nil, err
	}
	return &shortcutservice.ShortcutApplyResult{Applied: true, Config: config}, nil
}

func (s *shortcutApplier) GetShortcutRuntimeState() shortcutservice.ShortcutRuntimeState {
	return shortcutservice.ShortcutRuntimeState{}
}

type proxyApplier struct {
	result proxyservice.ProxyConfigApplyResult
}

func (p *proxyApplier) ApplyCurrentProxyConfig() (proxyservice.ProxyConfigApplyResult, error) {
	return p.result, nil
}

type fixture struct {
	t         *testing.T
	service   *settingservice.SettingService
	data      *store.Store
	window    *runtimetest.Window
	tester    *ui.Tester
	view      *View
	notifier  *widgets.Notifier
	confirm   *widgets.ConfirmDialog
	dispatch  *shortcuts.Dispatcher
	proxy     *proxyApplier
	shortcut  *shortcutApplier
	mu        sync.Mutex
	calls     []string
	info      *settingservice.CACertificateInfo
	trust     *settingservice.CACertificateTrustStatus
	trustErr  error
	trustFail error
	log       logger.LogStatus
	size      proxyservice.LocalDataSize
	python    *pythonpluginservice.RuntimeStatus
	testErr   error
	found     []pythonpluginservice.InterpreterCandidate
	picked    string
	closed    int
	restarts  int
}

func (f *fixture) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fixture) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, call)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
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
	if err := service.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	f := &fixture{t: t, service: service, window: window, notifier: &widgets.Notifier{}, confirm: &widgets.ConfirmDialog{}, proxy: &proxyApplier{}, shortcut: &shortcutApplier{service: service}}
	f.info = &settingservice.CACertificateInfo{CertExists: true, KeyExists: true, ValidPair: true, IsCA: true, Subject: "FlowLens CA", SHA256Fingerprint: "ab:cd"}
	f.trust = &settingservice.CACertificateTrustStatus{Supported: true, SHA256Fingerprint: "ab:cd"}
	f.log = logger.LogStatus{Enabled: true, Level: logger.LogLevelInfo}
	f.size = proxyservice.LocalDataSize{CacheBytes: 2048, HistoryBytes: 1024}
	f.python = &pythonpluginservice.RuntimeStatus{Enabled: false}
	f.data = store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: service, Shortcuts: f.shortcut, Proxy: f.proxy, Python: fakePython{f}}})
	if err := f.data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f.dispatch = shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig {
		if f.data.Settings.Current == nil {
			return nil
		}
		return f.data.Settings.Current.Shortcuts
	})
	f.view = New(f.data, Host{
		ListFonts: func() []settingservice.FontOption {
			return []settingservice.FontOption{{Label: "Inter", Value: "Inter"}, {Label: "Fira Code", Value: "Fira Code"}}
		},
		CAInfo:  func() (*settingservice.CACertificateInfo, error) { return f.info, nil },
		CATrust: func() (*settingservice.CACertificateTrustStatus, error) { return f.trust, f.trustFail },
		GenerateCA: func(overwrite bool) error {
			if overwrite {
				f.note("generate:overwrite")
			} else {
				f.note("generate")
			}
			return nil
		},
		InstallCA: func(fingerprint string) error {
			f.note("install:" + fingerprint)
			if f.trustErr != nil {
				return f.trustErr
			}
			f.trust = &settingservice.CACertificateTrustStatus{Supported: true, Present: true, Installed: true, SHA256Fingerprint: fingerprint}
			return nil
		},
		UninstallCA: func(fingerprint string) error {
			f.note("uninstall:" + fingerprint)
			f.trust = &settingservice.CACertificateTrustStatus{Supported: true, SHA256Fingerprint: fingerprint}
			return nil
		},
		PickFile:     func(string, ...string) (string, error) { return f.picked, nil },
		PickFiles:    func(string, ...string) ([]string, error) { return []string{"/ca/a.pem", "/ca/a.pem", "/ca/b.pem"}, nil },
		RestartProxy: func() { f.restarts++ },
		LogStatus:    func() (*logger.LogStatus, error) { copied := f.log; return &copied, nil },
		SetLog: func(enabled bool) (*logger.LogStatus, error) {
			f.log.Enabled = enabled
			copied := f.log
			return &copied, nil
		},
		SetLevel: func(level logger.LogLevel) (*logger.LogStatus, error) {
			f.log.Level = level
			f.note("level:" + string(level))
			copied := f.log
			return &copied, nil
		},
		ClearLogs:    func() error { f.note("clear-logs"); return nil },
		OpenLogDir:   func() error { f.note("open-logs"); return nil },
		DataSize:     func() (proxyservice.LocalDataSize, error) { return f.size, nil },
		ClearCache:   func() error { f.note("clear-cache"); return nil },
		ClearAllData: func() error { f.note("clear-all"); return nil },
		ResetApp:     func() error { f.note("reset"); return nil },
		PythonStatus: func() (*pythonpluginservice.RuntimeStatus, error) { return f.python, nil },
		TestPython: func(string) (*pythonpluginservice.RuntimeStatus, error) {
			if f.testErr != nil {
				return nil, f.testErr
			}
			return &pythonpluginservice.RuntimeStatus{Ready: true, Implementation: "CPython", PythonMajor: 3, PythonMinor: 13, PythonPatch: 1}, nil
		},
		FindPython: func(string) ([]pythonpluginservice.InterpreterCandidate, error) { return f.found, nil },
		Close:      func() { f.closed++ },
	}, f.dispatch, f.notifier, f.confirm)
	f.view.MacOS = false
	f.view.Begin()
	f.tester = ui.NewTester(f.view.Build, 1300, 1400)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

type fakePython struct{ f *fixture }

func (p fakePython) ConfigureRuntime(config settingservice.PythonPluginConfig) (*pythonpluginservice.RuntimeStatus, error) {
	p.f.note("configure-python")
	if err := p.f.service.SavePythonPluginConfig(&config); err != nil {
		return nil, err
	}
	return &pythonpluginservice.RuntimeStatus{Enabled: config.Enabled}, nil
}

func (p fakePython) GetRuntimeStatus() (*pythonpluginservice.RuntimeStatus, error) {
	return p.f.python, nil
}

func (f *fixture) settle() {
	for range 6 {
		f.data.Runtime.Jobs.Wait()
		f.window.Drain()
		f.tester.Frame()
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\n%v", label, err, f.tester.Texts())
	}
	f.settle()
}

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

// clickControl clicks the control beside a row's label.
func (f *fixture) clickControl(label string) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	x, y := rect.X+182+14, rect.Y+rect.H/2
	f.tester.Press(x, y)
	f.tester.Release(x, y)
	f.settle()
}

// clickAction clicks the button at the end of a row.
func (f *fixture) clickAction(label string) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	// Pages are at most 1040 wide after the 160-wide navigation and their
	// 28-wide margin, so the button ends near x = 1218.
	x, y := float32(1200), rect.Y+rect.H/2
	f.tester.Press(x, y)
	f.tester.Release(x, y)
	f.settle()
}

// dismissToasts waits for the notifications to go away.
func (f *fixture) dismissToasts() {
	f.tester.Advance(30 * time.Second)
	f.settle()
}

func (f *fixture) open(tab string) {
	f.t.Helper()
	f.click(tab)
}

func (f *fixture) draft() *settingservice.Settings { return f.data.Settings.Draft }

func TestSixSectionsAndDraftsSurviveNavigation(t *testing.T) {
	f := newFixture(t)
	for _, label := range []string{locales.SettingsTabGeneral(), locales.SettingsTabProxy(), locales.SettingsTabPythonPlugins(), locales.SettingsTabShortcuts(), locales.SettingsTabLogs(), locales.SettingsTabStorage()} {
		if !f.has(label) {
			t.Fatalf("no %q", label)
		}
		f.click(label)
	}
	f.open(locales.SettingsTabGeneral())
	f.draft().CommonConfig.PointerCursor = true
	f.settle()
	f.open(locales.SettingsTabStorage())
	f.draft().CacheConfig.MaxWsMessages = 42
	f.settle()
	f.open(locales.SettingsTabGeneral())
	if !f.draft().CommonConfig.PointerCursor || !f.view.Dirty() {
		t.Fatal("changes stay while switching sections")
	}
	f.click(locales.SettingsSave())
	if f.view.Dirty() {
		t.Fatal("saved together")
	}
	stored, err := f.service.Snapshot()
	if err != nil || !stored.CommonConfig.PointerCursor || stored.CacheConfig.MaxWsMessages != 42 {
		t.Fatalf("%v %+v %+v", err, stored.CommonConfig, stored.CacheConfig)
	}
	if !f.data.Settings.Current.CommonConfig.PointerCursor {
		t.Fatal("every window follows")
	}
	if !f.has(locales.SettingsSavedNotice()) && !f.has(locales.SettingsSavedAppliedNotice()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestCloseConfirmsWhenDirty(t *testing.T) {
	f := newFixture(t)
	if !f.view.CloseRequested() {
		t.Fatal("a clean window closes")
	}
	f.view.Begin()
	f.settle()
	f.draft().CommonConfig.PointerCursor = true
	f.settle()
	if f.view.CloseRequested() || !f.confirm.Open() {
		t.Fatal("asks first")
	}
	f.settle()
	if !f.has(locales.SettingsQuitDirtyMessage()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Accept()
	f.settle()
	if f.closed != 1 || f.data.Settings.Draft != nil {
		t.Fatal(f.closed)
	}
}

func TestSaveKeepsNewerLayout(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.PointerCursor = true
	f.settle()
	layout := settingservice.TrafficDetailLayoutVertical
	if f.data.Settings.TrafficDetailLayout() == layout {
		layout = settingservice.TrafficDetailLayoutHorizontal
	}
	if err := f.service.SaveTrafficDetailConfig(&settingservice.TrafficDetailConfig{Layout: layout}); err != nil {
		t.Fatal(err)
	}
	f.click(locales.SettingsSave())
	stored, _ := f.service.Snapshot()
	if stored.TrafficDetailConfig.Layout != layout {
		t.Fatal(stored.TrafficDetailConfig.Layout)
	}
}

func TestGeneralPreviewAndRestartNotice(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.ThemePrimaryColor = "emerald"
	f.settle()
	if f.data.Settings.Theme().PrimaryColor != "emerald" {
		t.Fatal("live preview", f.data.Settings.Theme().PrimaryColor)
	}
	f.view.CloseRequested()
	f.confirm.Accept()
	f.settle()
	if f.data.Settings.Theme().PrimaryColor == "emerald" {
		t.Fatal("closing discards the preview")
	}
	f.view.Begin()
	f.settle()
	window := f.draft().WindowConfig
	if window.FrameMode == settingservice.WindowFrameModeCustom {
		window.FrameMode = settingservice.WindowFrameModeSystem
	} else {
		window.FrameMode = settingservice.WindowFrameModeCustom
	}
	f.settle()
	if !f.has(locales.SettingsWindowFrameModeRestartNotice()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.draft().CommonConfig.AppFontSize = 20
	f.settle()
	if !f.has(locales.SettingsFontPreviewText()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.SettingsFontSizeReset() + " " + locales.SettingsAppFontSizeLabel())
	if f.draft().CommonConfig.AppFontSize != 16 {
		t.Fatal(f.draft().CommonConfig.AppFontSize)
	}
	f.clickControl(locales.SettingsAppFontLabel())
	if !f.view.general.fontsLoaded {
		t.Fatal("fonts load when the list opens")
	}
	if !f.has("Inter") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestRestoreDefaultsFillsDraft(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.AppFontSize = 22
	f.draft().ProxyConfig.Port = 9999
	f.settle()
	f.click(locales.SettingsReset())
	if f.draft().CommonConfig.AppFontSize != 16 || f.draft().ProxyConfig.Port != 8080 || !f.view.Dirty() {
		t.Fatalf("%+v", f.draft().CommonConfig)
	}
}

func TestCustomUpstreamMustBeValid(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	proxy := f.draft().ProxyConfig
	proxy.UpstreamMode = settingservice.UpstreamProxyModeCustom
	proxy.UpstreamProxy = ""
	f.settle()
	f.click(locales.SettingsSave())
	if !f.has(locales.SettingsUpstreamProxyCustomRequired()) || !f.view.Dirty() {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.dismissToasts()
	proxy.UpstreamProxy = "not a url"
	f.settle()
	f.click(locales.SettingsSave())
	if !f.view.Dirty() {
		t.Fatal("invalid is blocked")
	}
	f.dismissToasts()
	proxy.UpstreamProxy = "http://127.0.0.1:8888"
	f.settle()
	f.click(locales.SettingsSave())
	if f.view.Dirty() {
		t.Fatalf("%v", f.tester.Texts())
	}
	stored, _ := f.service.Snapshot()
	if stored.ProxyConfig.UpstreamProxy != "http://127.0.0.1:8888" {
		t.Fatal(stored.ProxyConfig.UpstreamProxy)
	}
}

func TestRestartPromptAfterProxyChange(t *testing.T) {
	f := newFixture(t)
	f.proxy.result = proxyservice.ProxyConfigApplyResult{Applied: true, RestartRequired: true, RestartReasons: []string{"listen_address_changed"}}
	f.open(locales.SettingsTabProxy())
	f.draft().ProxyConfig.UpstreamMode = settingservice.UpstreamProxyModeNone
	f.settle()
	f.click(locales.SettingsSave())
	if !f.has(locales.SettingsSavedPartialRestartNotice(locales.SettingsRestartReasonListenAddressChanged())) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.CaptureRestart())
	if f.restarts != 1 {
		t.Fatal(f.restarts)
	}
}

func TestCustomCAModeRequiresPathsAndDisablesGeneration(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	proxy := f.draft().ProxyConfig
	proxy.UseBuiltinCA = false
	f.settle()
	if !f.has(locales.ToolbarCaCertPath()) || !f.has(locales.ToolbarCaKeyPath()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	if !f.view.proxy.generationDisabled(false, true) || f.view.proxy.generationHint(false, false) != locales.SettingsCaTrustErrorsCaCustomGenerationDisabled() {
		t.Fatal("generation is off in custom mode")
	}
	f.click(locales.SettingsSave())
	if !f.has(locales.SettingsCaTrustErrorsCaCustomPathsRequired()) || !f.view.Dirty() {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.picked = "/certs/ca.crt"
	f.click(locales.SettingsSelectCaCertFile())
	if proxy.CACertPath != "/certs/ca.crt" {
		t.Fatal(proxy.CACertPath)
	}
	proxy.CAKeyPath = "/certs/ca.key"
	f.dismissToasts()
	f.click(locales.SettingsSave())
	if f.view.Dirty() {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestEveryTrustStatus(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	state := &f.view.proxy
	if !f.has(locales.SettingsCaTrustNotInstalled()) || !f.has(locales.SettingsCaStatusReady()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	cases := []struct {
		name    string
		trust   *settingservice.CACertificateTrustStatus
		fail    error
		want    string
		blocked bool
	}{
		{"installed", &settingservice.CACertificateTrustStatus{Supported: true, Present: true, Installed: true, SHA256Fingerprint: "ab"}, nil, locales.SettingsCaTrustInstalled(), false},
		{"partial", &settingservice.CACertificateTrustStatus{Supported: true, Present: true, SHA256Fingerprint: "ab"}, nil, locales.SettingsCaTrustPartial(), false},
		{"error", &settingservice.CACertificateTrustStatus{Supported: true, Error: "query failed", SHA256Fingerprint: "ab"}, nil, locales.SettingsCaTrustStatusUnavailable(), true},
		{"unknown", nil, errors.New("no store"), locales.SettingsCaTrustStatusUnavailable(), true},
	}
	for _, testCase := range cases {
		f.trust, f.trustFail = testCase.trust, testCase.fail
		f.view.loadCA()
		f.settle()
		if !f.has(testCase.want) {
			t.Fatalf("%s: %v", testCase.name, f.tester.Texts())
		}
		if testCase.blocked && !state.trustUnknown() {
			t.Fatal(testCase.name)
		}
	}
	f.trust, f.trustFail = &settingservice.CACertificateTrustStatus{Supported: false}, nil
	f.view.loadCA()
	f.settle()
	if f.has(locales.SettingsCaTrustNotInstalled()) || f.has(locales.SettingsCaTrustInstall()) {
		t.Fatalf("Linux offers no trust control: %v", f.tester.Texts())
	}
}

func TestTrustInstallUninstallAndCanceled(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	f.click(locales.SettingsCaTrustInstall())
	if !f.called("install:ab:cd") || !f.has(locales.SettingsCaTrustInstalled()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.SettingsCaTrustUninstall())
	if !f.called("uninstall:ab:cd") || !f.has(locales.SettingsCaTrustNotInstalled()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.trustErr = errors.New("ca_trust_canceled")
	f.click(locales.SettingsCaTrustInstall())
	if f.has(locales.SettingsCaTrustInstallFailed("ca_trust_canceled")) {
		t.Fatal("cancel is silent")
	}
	f.trustErr = errors.New("ca_trust_timeout")
	f.click(locales.SettingsCaTrustInstall())
	if !f.has(locales.SettingsCaTrustInstallFailed(locales.SettingsCaTrustErrorsCaTrustTimeout())) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestRegenerateNeedsUninstallAndConfirmation(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	f.click(locales.SettingsRegenerateCa())
	if !f.confirm.Open() || !f.has(locales.SettingsRegenerateCaConfirm()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Accept()
	f.settle()
	if !f.called("generate:overwrite") || !f.has(locales.SettingsCaGeneratedRestartNotice()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.trust = &settingservice.CACertificateTrustStatus{Supported: true, Present: true, Installed: true, SHA256Fingerprint: "ab"}
	f.view.loadCA()
	f.settle()
	if !f.view.proxy.generationDisabled(true, false) || f.view.proxy.generationHint(true, false) != locales.SettingsCaTrustUninstallFirst() {
		t.Fatal("installed CA must be uninstalled first")
	}
}

func TestInvalidBuiltinCAStillOffersGeneration(t *testing.T) {
	f := newFixture(t)
	f.info = &settingservice.CACertificateInfo{Error: "expired"}
	f.open(locales.SettingsTabProxy())
	if !f.has(locales.SettingsCaTrustErrorsCaBuiltinUnavailable()) || f.called("generate") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.SettingsGenerateCa())
	if !f.called("generate") {
		t.Fatal("offers generation")
	}
}

func TestRootsAndClientCertificatesPersist(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	f.view.pickRoots(f.draft().ProxyConfig)
	f.settle()
	if got := f.draft().ProxyConfig.RootCAPaths; !slices.Equal(got, []string{"/ca/a.pem", "/ca/b.pem"}) {
		t.Fatal(got)
	}
	f.click(locales.SettingsAddClientCert())
	f.settle()
	if !f.has(locales.SettingsClientCertMissingFields(strings.Join([]string{locales.SettingsClientCertTargetHost(), locales.SettingsClientCertFile(), locales.SettingsClientKeyFile()}, locales.SettingsFieldSeparator()))) {
		t.Fatalf("%v", f.tester.Texts())
	}
	cert := &f.draft().ProxyConfig.ClientCerts[0]
	cert.Hostname, cert.CertPath, cert.KeyPath = "api.example.com", "/c.pem", "/k.pem"
	f.settle()
	f.click(locales.SettingsSave())
	stored, _ := f.service.Snapshot()
	if len(stored.ProxyConfig.RootCAPaths) != 2 || len(stored.ProxyConfig.ClientCerts) != 1 || stored.ProxyConfig.ClientCerts[0].Hostname != "api.example.com" {
		t.Fatalf("%+v", stored.ProxyConfig)
	}
}

func TestPythonTrustConfirmationDetectAndTest(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabPythonPlugins())
	config := f.draft().PythonPluginConfig
	f.clickControl(locales.SettingsPythonPluginsEnabledLabel())
	if config.Enabled || !f.confirm.Open() || !f.has(locales.SettingsPythonTrustedCodeConfirmMessage()) {
		t.Fatal("asks first", config.Enabled)
	}
	f.click(locales.SettingsPythonTrustedCodeCancel())
	if config.Enabled {
		t.Fatal("declining keeps it off")
	}
	f.clickControl(locales.SettingsPythonPluginsEnabledLabel())
	f.confirm.Accept()
	f.settle()
	if !config.Enabled {
		t.Fatal("accepted")
	}
	f.found = nil
	f.click(locales.SettingsPythonInterpreterDetect())
	if !f.has(locales.SettingsPythonInterpreterDetectNone()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.found = []pythonpluginservice.InterpreterCandidate{{InterpreterPath: "/usr/bin/python3.13", Implementation: "CPython", PythonMajor: 3, PythonMinor: 13, PythonPatch: 1}, {InterpreterPath: "/usr/bin/python3.11", Implementation: "CPython", PythonMajor: 3, PythonMinor: 11, Current: true}}
	f.click(locales.SettingsPythonInterpreterDetect())
	if !f.view.python.discovery || f.view.python.selected != "/usr/bin/python3.11" || !f.has("CPython 3.13.1") || !f.has(locales.SettingsPythonInterpreterDetectLatest()) {
		t.Fatalf("%+v %v", f.view.python, f.tester.Texts())
	}
	f.click("/usr/bin/python3.13")
	f.click(locales.SettingsPythonInterpreterDetectUse())
	if config.InterpreterPath != "/usr/bin/python3.13" || !f.has(locales.SettingsPythonInterpreterTestSuccess("CPython 3.13.1")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.testErr = errors.New("bad interpreter")
	f.click(locales.SettingsPythonInterpreterTest())
	if !f.has(locales.SettingsPythonInterpreterTestFailed("bad interpreter")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	config.InterpreterPath = "/other"
	f.settle()
	if f.has(locales.SettingsPythonInterpreterTestFailed("bad interpreter")) {
		t.Fatal("result resets with the path")
	}
	f.click(locales.SettingsSave())
	if !f.called("configure-python") {
		t.Fatal(f.calls)
	}
	f.data.Settings.PythonStatus = &pythonpluginservice.RuntimeStatus{Enabled: true, Error: "worker crashed"}
	f.settle()
	if !f.has("worker crashed") || !f.has(locales.SettingsPythonRuntimeError()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func holdKey(f *fixture, mods ui.Modifiers, key ui.Key) {
	f.tester.Frame()
	f.tester.KeyRepeat(mods, key)
	f.settle()
	f.tester.KeyUp(key)
	f.settle()
}

func TestShortcutRecordConflictClearRestore(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabShortcuts())
	config := f.draft().Shortcuts
	label := locales.ShortcutsCommandsAppSave()
	f.view.shortcuts.search = "save"
	f.settle()
	if !f.has(label) || f.has(locales.ShortcutsCommandsWorkspaceNewHttpRequest()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.shortcuts.search = "zzz"
	f.settle()
	if !f.has(locales.ShortcutsEmptySearch()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.shortcuts.search = ""
	f.settle()
	f.click(locales.ShortcutsRecordFor(label))
	if f.view.shortcuts.recording != shortcuts.AppSave || !f.has(locales.ShortcutsRecording()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	holdKey(f, 0, ui.KeyEscape)
	if f.view.shortcuts.recording != "" {
		t.Fatal("escape cancels")
	}
	if _, overridden := config.Overrides[shortcuts.AppSave]; overridden {
		t.Fatal("no change")
	}
	f.click(locales.ShortcutsRecordFor(label))
	holdKey(f, ui.Ctrl, ui.KeyK)
	override := config.Overrides[shortcuts.AppSave]
	if override.Binding == nil || override.Binding.Key != "k" || !slices.Contains(override.Binding.Modifiers, settingservice.ShortcutModifierPrimary) {
		t.Fatalf("%+v", override)
	}
	newTab := locales.ShortcutsCommandsWorkspaceNewHttpRequest()
	f.click(locales.ShortcutsRecordFor(newTab))
	holdKey(f, ui.Ctrl, ui.KeyK)
	if !f.confirm.Open() {
		t.Fatal("asks about the conflict")
	}
	f.confirm.Accept()
	f.settle()
	if config.Overrides[shortcuts.AppSave].Binding != nil || config.Overrides[shortcuts.WorkspaceNewHTTPRequest].Binding.Key != "k" {
		t.Fatalf("%+v", config.Overrides)
	}
	f.click(locales.ShortcutsRecordFor(newTab))
	holdKey(f, 0, ui.KeyBackspace)
	if config.Overrides[shortcuts.WorkspaceNewHTTPRequest].Binding != nil {
		t.Fatal("backspace clears")
	}
	f.click(locales.ShortcutsResetFor(newTab))
	if _, overridden := config.Overrides[shortcuts.WorkspaceNewHTTPRequest]; overridden {
		t.Fatal("restored")
	}
	f.click(locales.ShortcutsClearFor(newTab))
	f.settle()
	f.click(locales.ShortcutsResetAll())
	f.confirm.Accept()
	f.settle()
	for _, command := range shortcuts.Catalog {
		if _, overridden := config.Overrides[command.ID]; overridden {
			t.Fatal(command.ID)
		}
	}
}

func TestShortcutRuntimeStatusAndScope(t *testing.T) {
	f := newFixture(t)
	f.data.Settings.ShortcutRuntime = shortcutservice.ShortcutRuntimeState{Commands: map[string]shortcutservice.ShortcutCommandRuntimeState{shortcuts.AppShowMainWindow: {Status: shortcutservice.ShortcutStatusConflict}}, Warnings: []string{"macNonANSICompatibility"}}
	f.draft().Shortcuts.Overrides = map[string]settingservice.ShortcutOverride{shortcuts.AppShowMainWindow: {Binding: &settingservice.ShortcutBinding{Key: "h", Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierPrimary}}, Scope: settingservice.ShortcutScopeGlobal}}
	f.open(locales.SettingsTabShortcuts())
	if !f.has(locales.ShortcutsRuntimeConflict()) || !f.has(locales.ShortcutsWarningsMacNonANSICompatibility()) || !f.has(locales.ShortcutsScopeGlobal()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestUnsavedShortcutIsDiscarded(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabShortcuts())
	f.click(locales.ShortcutsClearFor(locales.ShortcutsCommandsAppSave()))
	if !f.view.Dirty() {
		t.Fatal("dirty")
	}
	f.view.CloseRequested()
	f.confirm.Accept()
	f.settle()
	if f.shortcut.applied != 0 {
		t.Fatal("not applied")
	}
	stored, _ := f.service.Snapshot()
	if stored.Shortcuts != nil {
		if _, overridden := stored.Shortcuts.Overrides[shortcuts.AppSave]; overridden {
			t.Fatal("not stored")
		}
	}
}

func TestLogLevelAppliesImmediately(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabLogs())
	if !f.has(locales.SettingsLogImmediateNotice()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.logs.level = "debug"
	f.settle()
	if !f.called("level:debug") || f.log.Level != logger.LogLevelDebug || f.view.Dirty() {
		t.Fatal(f.calls)
	}
	f.clickControl(locales.SettingsLogEnabledLabel())
	if f.log.Enabled {
		t.Fatal("toggled")
	}
	f.click(locales.SettingsLogOpenDir())
	if !f.called("open-logs") {
		t.Fatal(f.calls)
	}
	f.click(locales.SettingsLogClearLogs())
	if !f.has(locales.SettingsLogClearLogsConfirm()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Accept()
	f.settle()
	if !f.called("clear-logs") || !f.has(locales.SettingsLogSuccessClearedLogs()) {
		t.Fatal(f.calls)
	}
}

func TestStorageUsageAndEveryCleanupConfirms(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabStorage())
	if !f.has(formatSize(2048)) || !f.has(formatSize(1024)) || !f.has(formatSize(3072)) {
		t.Fatalf("%v", f.tester.Texts())
	}
	cases := []struct {
		label, confirm, call, done string
	}{
		{locales.SettingsClearCacheFiles(), locales.SettingsClearCacheFilesConfirm(), "clear-cache", locales.SettingsClearCacheSuccess()},
		{locales.SettingsClearAllData(), locales.SettingsClearAllDataConfirm(), "clear-all", locales.SettingsClearAllDataSuccess()},
	}
	for _, testCase := range cases {
		f.clickAction(testCase.label)
		if f.called(testCase.call) || !f.has(testCase.confirm) {
			t.Fatalf("%s asks first", testCase.label)
		}
		f.confirm.Accept()
		f.settle()
		if !f.called(testCase.call) || !f.has(testCase.done) {
			t.Fatalf("%v", f.tester.Texts())
		}
	}
	f.clickAction(locales.SettingsResetApplication())
	if f.called("reset") || !f.has(locales.SettingsResetApplicationConfirm()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Accept()
	f.settle()
	if !f.called("reset") || !f.data.Settings.ResetAwaitingRestart {
		t.Fatal(f.calls)
	}
}

func TestSaveShortcutRunsSave(t *testing.T) {
	f := newFixture(t)
	if f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("nothing to save")
	}
	f.draft().CommonConfig.PointerCursor = true
	f.settle()
	if !f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("runs")
	}
	f.settle()
	if f.view.Dirty() {
		t.Fatal("saved")
	}
}

func TestFailedSaveKeepsDraft(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabShortcuts())
	f.shortcut.fail = errors.New("registrar broke")
	f.click(locales.ShortcutsClearFor(locales.ShortcutsCommandsAppSave()))
	f.click(locales.SettingsSave())
	if !f.view.Dirty() || !f.has(locales.SettingsSaveFailed("registrar broke")) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestResetBlocksTheWindowThenOffersExit(t *testing.T) {
	f := newFixture(t)
	exits := 0
	f.view.Host.ExitAfterReset = func() { exits++ }
	f.view.Host.ResetApp = func() error { f.note("reset"); return nil }
	f.data.Settings.Resetting = true
	f.settle()
	if !f.has(locales.SettingsResetApplicationProgress()) || f.view.CloseRequested() || exits != 0 {
		t.Fatalf("%v", f.tester.Texts())
	}
	if f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("shortcuts are off")
	}
	f.data.Settings.ResetAwaitingRestart = true
	f.settle()
	if !f.has(locales.SettingsResetApplicationReady()) || !f.has(locales.SettingsResetApplicationReadyDescription()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.SettingsResetApplicationExit())
	if exits != 1 {
		t.Fatal(exits)
	}
	if f.view.CloseRequested() || exits != 2 {
		t.Fatal("closing the window exits too", exits)
	}
}

func TestFailedResetUnblocks(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabStorage())
	f.view.Host.ResetApp = func() error { return errors.New("ca_trust_timeout") }
	f.clickAction(locales.SettingsResetApplication())
	f.confirm.Accept()
	f.settle()
	if f.data.Settings.Resetting || f.has(locales.SettingsResetApplicationProgress()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	if !f.has(locales.SettingsResetApplicationFailed(locales.SettingsCaTrustErrorsCaTrustTimeout())) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestDiscardForUpdateUsesItsOwnWording(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.PointerCursor = true
	f.settle()
	proceeded, canceled := 0, 0
	f.view.AskDiscardBeforeQuit(true, func() { proceeded++ }, func() { canceled++ })
	f.settle()
	if !f.has(locales.SettingsUpdateDirtyMessage()) || !f.has(locales.SettingsUpdateDirtyConfirm()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.confirm.Cancel()
	if canceled != 1 || proceeded != 0 || !f.view.Dirty() {
		t.Fatal(canceled, proceeded)
	}
	f.view.AskDiscardBeforeQuit(true, func() { proceeded++ }, func() { canceled++ })
	f.confirm.Accept()
	f.settle()
	if proceeded != 1 || f.closed != 1 {
		t.Fatal(proceeded, f.closed)
	}
}

func TestCustomFrameDrawsWindowControls(t *testing.T) {
	f := newFixture(t)
	closes := 0
	f.view.Host.Frame = widgets.WindowFrame{Custom: true, Title: "Settings window", Close: func() { closes++ }}
	f.settle()
	if !f.has("Settings window") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click("Close")
	if closes != 1 {
		t.Fatal(closes)
	}
	f.view.Host.Frame.MacOS = true
	f.settle()
	if f.has("Minimize") {
		t.Fatal("macOS keeps its native controls")
	}
	f.view.Host.Frame = widgets.WindowFrame{Title: "Settings window"}
	f.settle()
	if f.has("Settings window") {
		t.Fatal("the system draws the title bar")
	}
}
