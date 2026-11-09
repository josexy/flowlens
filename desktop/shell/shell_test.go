package shell

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/pkg/database"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

type fixture struct {
	app     *App
	data    *store.Store
	tester  *ui.Tester
	window  *runtimetest.Window
	service *settingservice.SettingService
	closed  int
	minimal int
}

func (f *fixture) settle() {
	f.data.Runtime.Jobs.Wait()
	f.window.Drain()
	f.tester.Frame()
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
	data := store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: service}})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{data: data, window: window, service: service}
	f.app = New(data, Host{
		MinimizeWindow:       func() { f.minimal++ },
		ToggleMaximizeWindow: func() {},
		IsWindowMaximized:    func() bool { return false },
		CloseWindow:          func() { f.closed++ },
		OpenURL:              func(string) error { return nil },
		Environment: func() appservice.EnvironmentInfo {
			return appservice.EnvironmentInfo{AppVersion: "v9", GoVersion: "go1", MyGoVersion: "v0.8.3", BuildCommit: "abc", GOOS: "linux", GOARCH: "arm64"}
		},
	})
	f.app.MacOS = false
	f.app.Surfaces[store.WorkspaceCapture] = Surface{
		Sidebar: func(c *ui.Context, _ nui.Theme) { ui.Text(c, "capture-sidebar") },
		Content: func(c *ui.Context, _ nui.Theme) { ui.Text(c, "capture-content") },
	}
	f.app.Surfaces[store.WorkspaceRewriteRules] = Surface{
		Sidebar: func(c *ui.Context, _ nui.Theme) { ui.Text(c, "rewrite-sidebar") },
		Content: func(c *ui.Context, _ nui.Theme) { ui.Text(c, "rewrite-content") },
	}
	f.tester = ui.NewTester(f.app.View, 1280, 800)
	f.tester.SetLocale("en")
	return f
}

func TestSwitchingSectionsChangesSurfaces(t *testing.T) {
	f := newFixture(t)
	if !f.tester.HasText("capture-sidebar") || !f.tester.HasText("capture-content") {
		t.Fatalf("texts %q", f.tester.Texts())
	}
	if err := f.tester.Click(locales.MenuRewriteRules()); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	if !f.tester.HasText("rewrite-sidebar") || f.tester.HasText("capture-content") {
		t.Fatalf("texts %q", f.tester.Texts())
	}
}

func TestCollapsingTheSecondarySidebarKeepsItsWidth(t *testing.T) {
	f := newFixture(t)
	f.data.Workbench.SecondarySidebarPercent = 30
	if err := f.tester.Click(locales.MenuCapture()); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	if f.tester.HasText("capture-sidebar") || !f.tester.HasText("capture-content") {
		t.Fatalf("collapsed: %q", f.tester.Texts())
	}
	if err := f.tester.Click(locales.MenuCapture()); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	if !f.tester.HasText("capture-sidebar") || f.data.Workbench.SecondarySidebarPercent != 30 {
		t.Fatalf("expanded: %q, %v", f.tester.Texts(), f.data.Workbench.SecondarySidebarPercent)
	}
}

func TestWorkbenchShortcutsSwitchSections(t *testing.T) {
	f := newFixture(t)
	f.tester.Key(ui.Cmd, ui.Key4)
	f.tester.Frame()
	if f.data.Workbench.ActiveSection != store.WorkspaceRewriteRules {
		t.Fatalf("section %q", f.data.Workbench.ActiveSection)
	}
}

func TestCustomTitleBarShowsControlsOnWindowsAndLinux(t *testing.T) {
	f := newFixture(t)
	if !f.tester.HasText(locales.AppTitle()) {
		t.Fatalf("texts %q", f.tester.Texts())
	}
	if err := f.tester.Click("Minimize"); err != nil {
		t.Fatal(err)
	}
	if err := f.tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	if f.minimal != 1 || f.closed != 1 {
		t.Fatalf("minimize %d, close %d", f.minimal, f.closed)
	}
}

func TestMacOSKeepsNativeWindowControls(t *testing.T) {
	f := newFixture(t)
	f.app.MacOS = true
	f.tester.Frame()
	if _, found := f.tester.Find("Minimize"); found {
		t.Fatal("macOS draws no window controls of its own")
	}
}

func TestSystemTitleBarHidesTheCustomOne(t *testing.T) {
	f := newFixture(t)
	f.data.Settings.ActiveFrameMode = settingservice.WindowFrameModeSystem
	f.tester.Frame()
	if f.tester.HasText(locales.AppTitle()) {
		t.Fatalf("texts %q", f.tester.Texts())
	}
}

func TestChangingTheFrameModeWaitsForRestart(t *testing.T) {
	f := newFixture(t)
	f.data.Settings.BeginEdit()
	f.data.Settings.Draft.WindowConfig.FrameMode = settingservice.WindowFrameModeSystem
	f.data.Settings.MarkDirty(store.SectionWindow)
	f.data.Settings.Save(nil)
	f.settle()
	if !f.data.Settings.UsesCustomFrame() || !f.tester.HasText(locales.AppTitle()) {
		t.Fatal("the running window keeps its mode until restart")
	}
}

func TestStatusBarShowsCounts(t *testing.T) {
	f := newFixture(t)
	f.app.Counts = func() (int, int) { return 12, 3 }
	f.tester.Frame()
	want := locales.StatusTotal("12") + " " + locales.StatusSelected("3")
	if !f.tester.HasText(want) {
		t.Fatalf("texts %q", f.tester.Texts())
	}
}

func TestEnvironmentInformation(t *testing.T) {
	f := newFixture(t)
	if err := f.tester.Click(locales.StatusActionEnvironment()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	f.tester.Frame()
	for _, text := range []string{"v9", "go1", "abc", "linux/arm64"} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %q", text, f.tester.Texts())
		}
	}
}

func TestLayoutToggleSavesTheLayout(t *testing.T) {
	f := newFixture(t)
	if err := f.tester.Click(locales.StatusSwitchHorizontalLayout()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	got, _ := f.service.Snapshot()
	if got.TrafficDetailConfig.Layout != settingservice.TrafficDetailLayoutHorizontal {
		t.Fatalf("layout %q", got.TrafficDetailConfig.Layout)
	}
}

func TestShutdownIndicatorReportsWhenDrawn(t *testing.T) {
	f := newFixture(t)
	reported := 0
	f.app.Host.ShutdownUIReady = func() { reported++ }
	f.data.ShuttingDown = true
	f.tester.Frame()
	f.tester.Frame()
	if !f.tester.HasText(locales.AppShutdownTitle()) || reported != 1 {
		t.Fatalf("reported %d, texts %q", reported, f.tester.Texts())
	}
}

func TestStatusBarFollowsUpdateState(t *testing.T) {
	f := newFixture(t)
	opened := 0
	f.app.Host.OpenUpdate = func() { opened++ }
	rt := store.NewRuntime()
	t.Cleanup(rt.Close)
	f.data.Updater = store.NewUpdaterStore(rt, nil)
	cases := map[appservice.UpdatePhase]string{
		appservice.UpdatePhaseIdle:        locales.StatusActionUpdate(),
		appservice.UpdatePhaseUpToDate:    locales.StatusActionUpdate(),
		appservice.UpdatePhaseChecking:    locales.StatusUpdateChecking(),
		appservice.UpdatePhaseAvailable:   locales.StatusUpdateAvailable(),
		appservice.UpdatePhaseDownloading: locales.UpdaterDownloadingTitle(),
		appservice.UpdatePhaseVerifying:   locales.UpdaterVerifyingTitle(),
		appservice.UpdatePhasePreparing:   locales.UpdaterPreparingTitle(),
		appservice.UpdatePhaseReady:       locales.StatusUpdateReady(),
		appservice.UpdatePhaseError:       locales.StatusUpdateError(),
	}
	for phase, label := range cases {
		f.data.Updater.Snapshot.Phase = phase
		f.tester.Frame()
		f.tester.Frame()
		if !f.tester.HasText(label) {
			t.Fatalf("%s: no %q in %q", phase, label, f.tester.Texts())
		}
		if err := f.tester.Click(label); err != nil {
			t.Fatal(err)
		}
		f.tester.Frame()
	}
	if opened != len(cases) {
		t.Fatal(opened)
	}
}

func TestThemeAndLanguageToggles(t *testing.T) {
	f := newFixture(t)
	mode := f.app.currentThemeMode()
	f.app.toggleThemeMode()
	if f.app.currentThemeMode() != nextThemeMode(mode) {
		t.Fatalf("mode %q", f.app.currentThemeMode())
	}
	f.app.toggleLanguage()
	if f.data.Settings.Language() != "zh" {
		t.Fatalf("language %q", f.data.Settings.Language())
	}
}
