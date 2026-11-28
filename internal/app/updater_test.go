package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/rewrite"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/settings"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

type recordingReporter struct {
	mu       sync.Mutex
	progress []appservice.Progress
}

func (r *recordingReporter) ReportUpdateProgress(progress appservice.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, progress)
}

type engineFixture struct {
	engine   *mygoEngine
	reporter *recordingReporter
	clock    time.Time
}

func newEngineFixture(install func(context.Context, func(int64, int64)) error) *engineFixture {
	f := &engineFixture{reporter: &recordingReporter{}, clock: time.Unix(1000, 0)}
	f.engine = &mygoEngine{
		check: func(context.Context) (*availableUpdate, error) {
			return &availableUpdate{Version: "v1.2.0", Notes: "notes", Date: time.Unix(5, 0), Install: install}, nil
		},
		reporter: f.reporter,
		now:      func() time.Time { return f.clock },
	}
	return f
}

func TestMygoEngineMapsTheRelease(t *testing.T) {
	f := newEngineFixture(nil)
	release, err := f.engine.Check(context.Background())
	if err != nil || release.Version != "1.2.0" || release.Notes != "notes" || release.ReleaseURL != "https://github.com/josexy/flowlens/releases/tag/v1.2.0" {
		t.Fatalf("%+v %v", release, err)
	}
}

func TestMygoEngineMeasuresDownload(t *testing.T) {
	var f *engineFixture
	f = newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(10, 100)
		f.clock = f.clock.Add(time.Second)
		progress(50, 100)
		f.clock = f.clock.Add(time.Second)
		progress(100, 100)
		return nil
	})
	if _, err := f.engine.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.DownloadAndInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.reporter.progress) != 3 || f.reporter.progress[2].Written != 100 || f.reporter.progress[1].Written != 50 {
		t.Fatalf("%+v", f.reporter.progress)
	}
	if f.reporter.progress[2].Rate <= 0 {
		t.Fatalf("rate %+v", f.reporter.progress[2])
	}
}

func TestMygoEngineReturnsToDownloadWhenDeltaFallsBack(t *testing.T) {
	f := newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(20, 20)
		progress(5, 100)
		progress(100, 100)
		return nil
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.reporter.progress) != 3 || f.reporter.progress[1].Written != 5 || f.reporter.progress[1].Total != 100 || f.reporter.progress[1].Rate != 0 {
		t.Fatalf("fallback progress: %+v", f.reporter.progress)
	}
}

func TestMygoEngineCancelStopsInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newEngineFixture(func(ctx context.Context, progress func(int64, int64)) error {
		progress(1, 100)
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMygoEngineNeedsACheckFirst(t *testing.T) {
	f := newEngineFixture(nil)
	if err := f.engine.DownloadAndInstall(context.Background()); err == nil {
		t.Fatal("no update to install")
	}
}

func TestMygoEngineTamperedUpdateFails(t *testing.T) {
	tampered := errors.New("mygo: installing the update: bad signature")
	f := newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
		progress(100, 100)
		return tampered
	})
	_, _ = f.engine.Check(context.Background())
	if err := f.engine.DownloadAndInstall(context.Background()); !errors.Is(err, tampered) {
		t.Fatal(err)
	}
}

func releaseServer(t *testing.T, status int, body string) *manualEngine {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != releaseUserAgent {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return &manualEngine{client: server.Client(), url: server.URL, current: "1.0.5"}
}

func TestManualEngineFindsNewerRelease(t *testing.T) {
	engine := releaseServer(t, 200, `{"tag_name":"v1.1.0","name":"One","body":"# Notes","html_url":"https://github.com/josexy/flowlens/releases/tag/v1.1.0","published_at":"2026-01-02T03:04:05Z"}`)
	release, err := engine.Check(context.Background())
	if err != nil || release == nil || release.Version != "1.1.0" || release.Notes != "# Notes" || release.PublishedAt.Year() != 2026 {
		t.Fatalf("%+v %v", release, err)
	}
	if err := engine.DownloadAndInstall(context.Background()); !errors.Is(err, errManualInstall) {
		t.Fatal(err)
	}
}

func TestManualEngineUpToDate(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v1.0.5"}`, `{"tag_name":"1.0.4"}`} {
		release, err := releaseServer(t, 200, body).Check(context.Background())
		if err != nil || release != nil {
			t.Fatalf("%s: %+v %v", body, release, err)
		}
	}
	if release, err := releaseServer(t, 404, `{}`).Check(context.Background()); err != nil || release != nil {
		t.Fatalf("no releases yet: %+v %v", release, err)
	}
}

func TestManualEngineReportsFailures(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
	}{{500, ""}, {403, `{"message":"rate limited"}`}, {200, "not json"}, {200, `{"tag_name":""}`}} {
		if _, err := releaseServer(t, testCase.status, testCase.body).Check(context.Background()); err == nil {
			t.Fatalf("%+v", testCase)
		}
	}
}

func TestManualEngineCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := releaseServer(t, 200, `{}`).Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, testCase := range []struct {
		left, right string
		want        int
	}{
		{"1.0.5", "1.0.5", 0}, {"v1.0.6", "1.0.5", 1}, {"1.0.10", "1.0.9", 1}, {"1.2", "1.2.0", 0},
		{"1.0.5-beta.1", "1.0.5", -1}, {"1.0.5", "1.0.5-beta.1", 1}, {"1.0.5-beta.2", "1.0.5-beta.1", 1},
		{"1.0.5-beta.10", "1.0.5-beta.9", 1}, {"1.0.5-rc", "1.0.5-beta", 1}, {"1.0.5-1", "1.0.5-beta", -1},
		{"1.0.5-beta", "1.0.5-beta.1", -1}, {"1.0.5+abc", "1.0.5", 0},
	} {
		if got := compareVersions(testCase.left, testCase.right); got != testCase.want {
			t.Errorf("%s vs %s = %d", testCase.left, testCase.right, got)
		}
	}
}

func TestAutomaticUpdateRestartUsesLifecycleWithoutAWindow(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "restart", true: "unsaved work"}[blocked], func(t *testing.T) {
			guarded := false
			f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(reason string, _ func()) bool {
				guarded = reason == quitReasonUpdate
				return blocked
			})
			service := appservice.New()
			engine := newEngineFixture(func(_ context.Context, progress func(int64, int64)) error {
				progress(100, 100)
				return nil
			}).engine
			engine.reporter = service
			service.SetUpdateEngine(engine, appservice.UpdateApplyModeSelf)
			if err := service.ServiceStartup(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.ShutdownUpdater)
			var failed []error
			handler := restartHandler(f, &failed)
			requested := make(chan struct{}, 2)
			service.SetRestartRequester(func() {
				handler.request()
				requested <- struct{}{}
			})
			if err := service.CheckForUpdates(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(time.Second)
			for service.GetUpdateSnapshot().Phase != appservice.UpdatePhaseAvailable && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if err := service.DownloadUpdate(true); err != nil {
				t.Fatal(err)
			}
			select {
			case <-requested:
			case <-time.After(time.Second):
				t.Fatal("no automatic restart request")
			}
			if !guarded || len(failed) != 0 || f.lifecycle.relaunch.Load() == blocked {
				t.Fatalf("guarded=%v, blocked=%v, relaunch=%v, failures=%v", guarded, blocked, f.lifecycle.relaunch.Load(), failed)
			}
			if blocked {
				if service.GetUpdateSnapshot().Phase != appservice.UpdatePhaseReady {
					t.Fatal("declining a restart must retain the installed update")
				}
			} else {
				f.lifecycle.coordinator.UIReady()
				f.waitForQuit(t)
				if f.prepared.Load() != 1 {
					t.Fatal("restart skipped normal shutdown")
				}
			}
		})
	}
}

func restartHandler(f *lifecycleFixture, failed *[]error) *updateRestartHandler {
	return &updateRestartHandler{
		blocked: func(proceed func()) bool { return f.lifecycle.blockedByGuards(quitReasonUpdate, proceed) },
		onMain:  func(fn func()) { fn() },
		restart: f.lifecycle.restartForUpdate,
		failed:  func(err error) { *failed = append(*failed, err) },
	}
}

func TestUpdateRestartAsksTheGuardsOnTheMainThread(t *testing.T) {
	var onMain atomic.Bool
	var guardedOnMain []bool
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(string, func()) bool {
		guardedOnMain = append(guardedOnMain, onMain.Load())
		return false
	})
	var failed []error
	handler := restartHandler(f, &failed)
	handler.onMain = func(fn func()) {
		onMain.Store(true)
		defer onMain.Store(false)
		fn()
	}
	handler.request()
	if len(guardedOnMain) != 1 || !guardedOnMain[0] || !f.lifecycle.relaunch.Load() {
		t.Fatalf("guards on the main thread: %v, relaunch %t", guardedOnMain, f.lifecycle.relaunch.Load())
	}
}

func TestUpdateRestartRelaunches(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	var failed []error
	restartHandler(f, &failed).request()
	if !f.lifecycle.relaunch.Load() || len(failed) != 0 {
		t.Fatal(failed)
	}
	f.lifecycle.coordinator.UIReady()
	f.waitForQuit(t)
	if f.prepared.Load() != 1 {
		t.Fatal("normal shutdown runs first")
	}
}

func TestDirtyWorkBlocksUpdateRestartUntilResolved(t *testing.T) {
	var proceed func()
	var reasons []string
	blocking := true
	guard := func(reason string, next func()) bool {
		reasons = append(reasons, reason)
		proceed = next
		return blocking
	}
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, guard)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	if f.lifecycle.relaunch.Load() || f.lifecycle.coordinator.InProgress() || reasons[0] != quitReasonUpdate {
		t.Fatal("restart waits for the user", reasons)
	}
	handler.request()
	if len(reasons) != 2 {
		t.Fatal("a declined restart can be asked again", reasons)
	}
	blocking = false
	proceed()
	deadline := time.Now().Add(2 * time.Second)
	for !f.lifecycle.relaunch.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !f.lifecycle.relaunch.Load() {
		t.Fatal("resolved work continues the restart")
	}
}

func TestRewriteDraftsBlockUpdateRestart(t *testing.T) {
	view := &fakeRewriteView{dirty: true}
	guard := &rewriteQuitGuard{view: view}
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, guard.guard)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	if len(view.askedReasons()) != 1 || view.askedReasons()[0] != quitReasonUpdate || f.lifecycle.relaunch.Load() {
		t.Fatalf("asked %v", view.askedReasons())
	}
	view.reply(rewrite.DecisionDiscard)
	deadline := time.Now().Add(2 * time.Second)
	for !f.lifecycle.relaunch.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !f.lifecycle.relaunch.Load() {
		t.Fatal("discarded drafts allow the restart")
	}
}

// makeSettingsDirty gives the open Settings window an unsaved change.
func makeSettingsDirty(controller *settingsWindowController) {
	settings := controller.data.Settings
	settings.Draft.CommonConfig.DisableAnimations = !settings.Draft.CommonConfig.DisableAnimations
	settings.SyncDirty()
}

func settingsGuardFixture(t *testing.T, service *settingservice.SettingService) (*settingsWindowController, *widgets.ConfirmDialog, func()) {
	t.Helper()
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	data := store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: service}})
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	confirm := &widgets.ConfirmDialog{}
	view := settings.New(data, settings.Host{}, shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig { return nil }), &widgets.Notifier{}, confirm)
	view.Begin()
	controller := &settingsWindowController{data: data, view: view}
	return controller, confirm, window.Drain
}

func TestDirtySettingsBlockUpdateRestart(t *testing.T) {
	var controller *settingsWindowController
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(reason string, next func()) bool {
		return controller.quitGuard(reason, next)
	})
	var confirm *widgets.ConfirmDialog
	var drain func()
	controller, confirm, drain = settingsGuardFixture(t, f.settings)
	makeSettingsDirty(controller)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	drain()
	if !confirm.Open() || f.lifecycle.relaunch.Load() {
		t.Fatal("asks before discarding")
	}
	handler.request() // the question is already on screen
	drain()
	confirm.Accept()
	deadline := time.Now().Add(2 * time.Second)
	for !f.lifecycle.relaunch.Load() && time.Now().Before(deadline) {
		drain()
		time.Sleep(time.Millisecond)
	}
	if !f.lifecycle.relaunch.Load() || controller.view.Dirty() {
		t.Fatal("confirmed discard restarts")
	}
}

func TestDecliningDirtySettingsKeepsRunning(t *testing.T) {
	var controller *settingsWindowController
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(reason string, next func()) bool {
		return controller.quitGuard(reason, next)
	})
	var confirm *widgets.ConfirmDialog
	var drain func()
	controller, confirm, drain = settingsGuardFixture(t, f.settings)
	makeSettingsDirty(controller)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	drain()
	confirm.Cancel()
	if f.lifecycle.relaunch.Load() || !controller.view.Dirty() || controller.asking {
		t.Fatal("keeps editing")
	}
	handler.request()
	drain()
	if !confirm.Open() {
		t.Fatal("can ask again")
	}
}

func TestRestartFailureKeepsTheApplicationRunning(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	broken := errors.New("cannot relaunch")
	var failed []error
	handler := restartHandler(f, &failed)
	var attempts atomic.Int32
	handler.restart = func() error {
		attempts.Add(1)
		return broken
	}
	handler.request()
	if len(failed) != 1 || !errors.Is(failed[0], broken) || f.lifecycle.coordinator.InProgress() || f.prepared.Load() != 0 {
		t.Fatal(failed)
	}
	handler.request()
	if attempts.Load() != 2 {
		t.Fatal("retry is possible")
	}
}

func TestRestartRefusedDuringShutdown(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	if !f.lifecycle.coordinator.Request() {
		t.Fatal("request")
	}
	if err := f.lifecycle.restartForUpdate(); err == nil || f.lifecycle.relaunch.Load() {
		t.Fatal("shutdown blocks restart")
	}
}

func TestUpdateRestartWaitsForBothPrompts(t *testing.T) {
	var controller *settingsWindowController
	view := &fakeRewriteView{dirty: true}
	rewriteGuard := &rewriteQuitGuard{view: view}
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(reason string, next func()) bool {
		return controller.quitGuard(reason, next)
	}, rewriteGuard.guard)
	var confirm *widgets.ConfirmDialog
	var drain func()
	controller, confirm, drain = settingsGuardFixture(t, f.settings)
	makeSettingsDirty(controller)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	drain()
	if !confirm.Open() || len(view.askedReasons()) != 0 || f.lifecycle.relaunch.Load() {
		t.Fatal("the Settings prompt comes first")
	}
	confirm.Accept()
	deadline := time.Now().Add(2 * time.Second)
	for len(view.askedReasons()) == 0 && time.Now().Before(deadline) {
		drain()
		time.Sleep(time.Millisecond)
	}
	if len(view.askedReasons()) != 1 || view.askedReasons()[0] != quitReasonUpdate || f.lifecycle.relaunch.Load() {
		t.Fatalf("the rewrite prompt follows the Settings prompt: asked %v, relaunch %t", view.askedReasons(), f.lifecycle.relaunch.Load())
	}
	view.reply(rewrite.DecisionSave)
	for !f.lifecycle.relaunch.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !f.lifecycle.relaunch.Load() || len(failed) != 0 {
		t.Fatalf("restart after both prompts: relaunch %t, failures %v", f.lifecycle.relaunch.Load(), failed)
	}
}
