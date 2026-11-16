package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/settings"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

func restartHandler(f *lifecycleFixture, failed *[]error) *updateRestartHandler {
	return &updateRestartHandler{
		blocked: func(proceed func()) bool { return f.lifecycle.blockedByGuards(quitReasonUpdate, proceed) },
		restart: f.lifecycle.restartForUpdate,
		failed:  func(err error) { *failed = append(*failed, err) },
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
	rewrite := &rewriteQuitGuard{}
	rewrite.setDirty(true)
	var request *rewriteQuitRequest
	var proceed func()
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, func(reason string, next func()) bool {
		blocked, asked := rewrite.request(reason)
		if asked != nil {
			request, proceed = asked, next
		}
		return blocked
	})
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	if request == nil || request.Reason != quitReasonUpdate || f.lifecycle.relaunch.Load() {
		t.Fatalf("%+v", request)
	}
	if _, confirmed := rewrite.confirm(request.RequestID, request.Reason, "discard"); !confirmed {
		t.Fatal("confirmed")
	}
	proceed()
	deadline := time.Now().Add(2 * time.Second)
	for !f.lifecycle.relaunch.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !f.lifecycle.relaunch.Load() {
		t.Fatal("discarded drafts allow the restart")
	}
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
	controller.dirty.Store(true)
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
	if !f.lifecycle.relaunch.Load() || controller.dirty.Load() {
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
	controller.dirty.Store(true)
	var failed []error
	handler := restartHandler(f, &failed)
	handler.request()
	drain()
	confirm.Cancel()
	if f.lifecycle.relaunch.Load() || !controller.dirty.Load() || controller.asking.Load() {
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

func TestRestartRefusedDuringShutdownOrReset(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.resetting.Store(true)
	if err := f.lifecycle.restartForUpdate(); err == nil {
		t.Fatal("reset blocks restart")
	}
	f.lifecycle.resetting.Store(false)
	if !f.lifecycle.coordinator.Request() {
		t.Fatal("request")
	}
	if err := f.lifecycle.restartForUpdate(); err == nil || f.lifecycle.relaunch.Load() {
		t.Fatal("shutdown blocks restart")
	}
}

type fakeResetGuard struct {
	removeErr           error
	committed, canceled int
}

func (g *fakeResetGuard) RemoveBuiltinCATrust() error { return g.removeErr }
func (g *fakeResetGuard) Commit()                     { g.committed++ }
func (g *fakeResetGuard) Cancel() {
	if g.committed == 0 {
		g.canceled++
	}
}

func resetFixture(t *testing.T, f *lifecycleFixture, guard resetGuard) (*resetRequest, *int) {
	t.Helper()
	committed := new(int)
	return &resetRequest{
		lifecycle: f.lifecycle, language: func() string { return "en" },
		begin:         func() (resetGuard, error) { return guard, nil },
		cancelUpdates: func() {},
		saveManifest:  saveResetManifest,
		clearMarker:   clearResetMarker,
		committed:     func() { *committed++ },
	}, committed
}

func TestResetCommitsForTheNextLaunch(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	guard := &fakeResetGuard{}
	request, committed := resetFixture(t, f, guard)
	if err := request.run(); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadResetManifest()
	if err != nil || manifest == nil || manifest.Phase != resetPending || manifest.Language != "en" {
		t.Fatalf("%+v %v", manifest, err)
	}
	if guard.committed != 1 || *committed != 1 || !f.lifecycle.resetReady.Load() || !f.lifecycle.resetting.Load() {
		t.Fatal("committed")
	}
	if err := request.run(); err == nil {
		t.Fatal("only once")
	}
	if err := clearResetMarker(); err != nil {
		t.Fatal(err)
	}
}

func TestCATrustFailureLeavesDataIntact(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	root, err := resetDirectory()
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(filepath.Dir(root), "keep.txt")
	if err := os.MkdirAll(filepath.Dir(data), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	guard := &fakeResetGuard{removeErr: errors.New("user refused the trust prompt")}
	request, committed := resetFixture(t, f, guard)
	if err := request.run(); !errors.Is(err, guard.removeErr) {
		t.Fatal(err)
	}
	manifest, err := loadResetManifest()
	if err != nil || manifest != nil {
		t.Fatalf("no marker survives: %+v %v", manifest, err)
	}
	if contents, err := os.ReadFile(data); err != nil || string(contents) != "mine" {
		t.Fatal(err)
	}
	if guard.canceled != 1 || guard.committed != 0 || *committed != 0 || f.lifecycle.resetting.Load() || f.lifecycle.resetReady.Load() {
		t.Fatalf("%+v", guard)
	}
	guard.removeErr = nil
	if err := request.run(); err != nil {
		t.Fatal("a later reset still works", err)
	}
	_ = clearResetMarker()
}

func TestResetRefusedDuringShutdown(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	request, _ := resetFixture(t, f, &fakeResetGuard{})
	f.lifecycle.coordinator.Request()
	if err := request.run(); err == nil || f.lifecycle.resetting.Load() {
		t.Fatal(err)
	}
}

func TestQuitIsBlockedWhileResetRuns(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.resetting.Store(true)
	f.lifecycle.RequestQuit()
	if f.lifecycle.coordinator.InProgress() {
		t.Fatal("reset must finish first")
	}
	f.lifecycle.resetReady.Store(true)
	f.lifecycle.RequestQuit()
	if !f.lifecycle.coordinator.InProgress() {
		t.Fatal("exit proceeds once committed")
	}
}

func TestResetStartupActions(t *testing.T) {
	directory := t.TempDir()
	visible, shown := false, 0
	actions := &resetStartupActions{directory: directory, visible: func() bool { return visible }, show: func() { shown++ }}
	if err := actions.run("show"); err == nil || shown != 1 {
		t.Fatal("an invisible window is an error", err)
	}
	if err := actions.run("ready"); err == nil {
		t.Fatal("cannot authorise invisibly")
	}
	if err := actions.run("retry"); err == nil {
		t.Fatal("retry waits for ready")
	}
	if _, err := os.Stat(filepath.Join(directory, "action.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	visible = true
	if err := actions.run("ready"); err != nil {
		t.Fatal(err)
	}
	if err := actions.run("retry"); err != nil {
		t.Fatal(err)
	}
	var action resetStartupAction
	if err := readResetStartupJSON(directory, "action.json", &action); err != nil || action.Action != "retry" || !action.Ready || action.Sequence != 2 {
		t.Fatalf("%+v %v", action, err)
	}
}
