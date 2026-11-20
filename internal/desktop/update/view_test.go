package update

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/database"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
)

type fakeBackend struct {
	snapshot                             appservice.UpdateSnapshot
	checks, downloads, cancels, restarts int
	restartAfterInstall                  bool
	fail                                 error
}

func (f *fakeBackend) GetUpdateSnapshot() appservice.UpdateSnapshot { return f.snapshot }
func (f *fakeBackend) CheckForUpdates() error                       { f.checks++; return f.fail }
func (f *fakeBackend) DownloadUpdate(restart bool) error {
	f.downloads++
	f.restartAfterInstall = restart
	return f.fail
}
func (f *fakeBackend) CancelUpdate() error     { f.cancels++; return f.fail }
func (f *fakeBackend) RestartForUpdate() error { f.restarts++; return f.fail }

type fixture struct {
	t       *testing.T
	backend *fakeBackend
	data    *store.Store
	window  *runtimetest.Window
	tester  *ui.Tester
	view    *View
	opened  []string
	openErr error
	closed  int
}

func newFixture(t *testing.T, phase appservice.UpdatePhase, mode appservice.UpdateApplyMode) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
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
	backend := &fakeBackend{}
	f := &fixture{t: t, backend: backend, window: window}
	f.data = store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: service}, Updater: backend})
	if err := f.data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f.view = New(f.data, Host{
		OpenURL: func(address string) error {
			f.opened = append(f.opened, address)
			return f.openErr
		},
		Close: func() { f.closed++ },
	}, &widgets.Notifier{})
	f.tester = ui.NewTester(f.view.Build, 700, 900)
	f.tester.SetLocale("en")
	f.set(phase, mode)
	return f
}

func (f *fixture) set(phase appservice.UpdatePhase, mode appservice.UpdateApplyMode) {
	snapshot := f.data.Updater.Snapshot
	snapshot.Phase, snapshot.ApplyMode, snapshot.CurrentVersion = phase, mode, "1.0.5"
	snapshot.CanCancel = phase == appservice.UpdatePhaseChecking || phase == appservice.UpdatePhaseInstalling
	if phase == appservice.UpdatePhaseIdle || phase == appservice.UpdatePhaseUpToDate || phase == appservice.UpdatePhaseChecking {
		snapshot.Release = nil
	} else if snapshot.Release == nil {
		snapshot.Release = &appservice.UpdateRelease{Version: "1.1.0", Notes: "## Changes\n\nFixed [things](https://example.com/x).", PublishedAt: "2026-02-03T04:05:06Z", ReleaseURL: "https://github.com/josexy/flowlens/releases/tag/v1.1.0"}
	}
	f.data.Updater.Snapshot = snapshot
	f.settle()
}

func (f *fixture) settle() {
	for range 4 {
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

func (f *fixture) want(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if !f.has(text) {
			f.t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

func (f *fixture) refuse(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if f.has(text) {
			f.t.Fatalf("%q in %v", text, f.tester.Texts())
		}
	}
}

const self, manual = appservice.UpdateApplyModeSelf, appservice.UpdateApplyModeManual

func TestIdleChecks(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, manual)
	f.want(locales.UpdaterIdleTitle(), locales.UpdaterCurrentVersion("1.0.5"))
	f.click(locales.UpdaterActionCheck())
	if f.backend.checks != 1 {
		t.Fatal(f.backend.checks)
	}
}

func TestCheckingCanBeCanceled(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseChecking, self)
	f.want(locales.UpdaterCheckingTitle())
	f.refuse(locales.UpdaterActionCheck(), locales.UpdaterActionClose())
	f.click(locales.UpdaterActionCancel())
	if f.backend.cancels != 1 {
		t.Fatal(f.backend.cancels)
	}
}

func TestUpToDate(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseUpToDate, self)
	f.want(locales.UpdaterUpToDateTitle(), locales.UpdaterUpToDateDescription(), locales.UpdaterActionCheck())
	f.click(locales.UpdaterActionClose())
	if f.closed != 1 {
		t.Fatal(f.closed)
	}
}

func TestSelfUpdateAvailable(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseAvailable, self)
	f.want(locales.UpdaterAvailableTitle("1.1.0"), locales.UpdaterAvailableSelfDescription(), "v1.1.0", locales.UpdaterReleaseNotes(), "Changes", locales.UpdaterRestartAfterInstall())
	f.click(locales.UpdaterActionInstallRestart())
	if f.backend.downloads != 1 || !f.backend.restartAfterInstall || len(f.opened) != 0 {
		t.Fatal(f.backend.downloads, f.opened)
	}
}

func TestManualUpdateOpensTheReleasePage(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseAvailable, manual)
	f.want(locales.UpdaterAvailableManualDescription())
	f.refuse(locales.UpdaterRestartAfterInstall(), locales.UpdaterActionInstallRestart())
	f.click(locales.UpdaterActionOpenRelease())
	if len(f.opened) != 1 || f.backend.downloads != 0 {
		t.Fatal(f.opened)
	}
	f.openErr = errors.New("no browser")
	f.click(locales.UpdaterActionOpenRelease())
	f.want(locales.UpdaterActionFailed("no browser"))
}

func TestUnsafeReleaseAddressIsRefused(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseAvailable, manual)
	f.data.Updater.Snapshot.Release.ReleaseURL = "file:///etc/passwd"
	f.settle()
	f.click(locales.UpdaterActionOpenRelease())
	if len(f.opened) != 0 {
		t.Fatal(f.opened)
	}
	f.want(locales.UpdaterActionFailed(locales.UpdaterInvalidReleaseUrl()))
	f.view.openLink("javascript:alert(1)")
	f.view.openLink("https://example.com/x")
	if len(f.opened) != 1 || f.opened[0] != "https://example.com/x" {
		t.Fatal(f.opened)
	}
}

func TestEmptyNotes(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseAvailable, self)
	release := f.data.Updater.Snapshot.Release
	release.Notes, release.ArtifactName, release.ArtifactSize = "", "update.tar.gz", 2048
	f.settle()
	f.want(locales.UpdaterReleaseNotesEmpty())
}

func TestDownloadProgress(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseInstalling, self)
	f.data.Updater.Snapshot.Progress = &appservice.UpdateProgress{Written: 512, Total: 1024, Rate: 2048}
	f.settle()
	f.want(locales.UpdaterInstallingTitle(), "50%", "512 B / 1.0 KB · 2.0 KB/s")
	f.data.Updater.Snapshot.Progress = &appservice.UpdateProgress{Written: 512}
	f.settle()
	f.want("512 B / " + locales.UpdaterUnknownSize())
	f.refuse("50%")
	f.click(locales.UpdaterActionCancel())
	if f.backend.cancels != 1 {
		t.Fatal(f.backend.cancels)
	}
}

func TestHidingDoesNotCancelTheOperation(t *testing.T) {
	for _, phase := range []appservice.UpdatePhase{appservice.UpdatePhaseChecking, appservice.UpdatePhaseInstalling} {
		f := newFixture(t, phase, self)
		f.click(locales.UpdaterActionBackground())
		if f.closed != 1 || f.backend.cancels != 0 {
			t.Fatal(f.closed, f.backend.cancels)
		}
	}
}

func TestFailedCancelKeepsTheWindow(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseInstalling, self)
	f.backend.fail = errors.New("stuck")
	f.click(locales.UpdaterActionCancel())
	if f.closed != 0 {
		t.Fatal(f.closed)
	}
	f.want(locales.UpdaterActionFailed("stuck"))
}

func TestFinishingCanHideButCannotCancel(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseInstalling, self)
	f.data.Updater.Snapshot.CanCancel = false
	f.data.Updater.Snapshot.Progress = &appservice.UpdateProgress{Written: 100, Total: 100}
	f.data.Updater.Snapshot.RestartAfterInstall = true
	f.settle()
	f.want(locales.UpdaterFinishingDescription(), locales.UpdaterAutoRestartDescription())
	f.refuse(locales.UpdaterActionCancel(), "100%", locales.UpdaterRestartAfterInstall())
	f.click(locales.UpdaterActionBackground())
	if f.closed != 1 || f.backend.cancels != 0 || f.backend.restarts != 0 {
		t.Fatal("hiding or rebuilding must not cancel or restart", f.backend)
	}
}

func TestRestartChoiceSurvivesWindowRecreation(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseAvailable, self)
	f.click(locales.UpdaterRestartAfterInstall())
	if f.data.Updater.RestartAfterInstall {
		t.Fatal("checkbox must change persistent shared state")
	}
	f.click(locales.UpdaterActionClose())
	f.view = New(f.data, f.view.Host, &widgets.Notifier{})
	f.tester = ui.NewTester(f.view.Build, 520, 520)
	f.settle()
	f.want(locales.UpdaterActionDownload())
	f.refuse(locales.UpdaterActionInstallRestart())
	f.click(locales.UpdaterActionDownload())
	if f.backend.downloads != 1 || f.backend.restartAfterInstall {
		t.Fatal(f.backend)
	}
}

func TestReadyRestartsNowOrLater(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseReady, self)
	f.want(locales.UpdaterReadyTitle())
	f.click(locales.UpdaterActionRestart())
	if f.backend.restarts != 1 {
		t.Fatal(f.backend.restarts)
	}
	f.click(locales.UpdaterActionLater())
	if f.closed != 1 || f.backend.restarts != 1 {
		t.Fatal(f.closed)
	}
}

func TestRetryFollowsTheFailedStage(t *testing.T) {
	cases := []struct {
		stage appservice.UpdatePhase
		count func(*fakeBackend) int
	}{
		{appservice.UpdatePhaseChecking, func(b *fakeBackend) int { return b.checks }},
		{appservice.UpdatePhaseInstalling, func(b *fakeBackend) int { return b.downloads }},
		{appservice.UpdatePhaseReady, func(b *fakeBackend) int { return b.restarts }},
	}
	for _, testCase := range cases {
		f := newFixture(t, appservice.UpdatePhaseError, self)
		f.data.Updater.Snapshot.Failure = &appservice.UpdateFailure{Stage: testCase.stage, Message: "signature does not match"}
		f.settle()
		title := locales.UpdaterErrorTitle()
		if testCase.stage == appservice.UpdatePhaseReady {
			title = locales.UpdaterRestartErrorTitle()
		}
		f.want(title, "signature does not match")
		f.click(locales.UpdaterActionRetry())
		if testCase.count(f.backend) != 1 {
			t.Fatalf("%s: %+v", testCase.stage, f.backend)
		}
	}
}

func TestActionFailureIsShown(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, self)
	f.backend.fail = errors.New("offline")
	f.click(locales.UpdaterActionCheck())
	f.want(locales.UpdaterActionFailed("offline"))
}

func TestPublishedAtToleratesOtherFormats(t *testing.T) {
	if got := publishedAt("yesterday"); got != logic.UnknownValue {
		t.Fatal(got)
	}
	if got := publishedAt("2026-02-03T04:05:06Z"); !strings.Contains(got, "2026") {
		t.Fatal(got)
	}
}

func TestCustomFrameDrawsWindowControls(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, self)
	closes := 0
	f.view.Host.Frame = widgets.WindowFrame{Custom: true, Title: "Update window", Close: func() { closes++ }}
	f.settle()
	f.want("Update window")
	f.click("Close")
	if closes != 1 || f.closed != 0 {
		t.Fatal(closes, f.closed)
	}
}
