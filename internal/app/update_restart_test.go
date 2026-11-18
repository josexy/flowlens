package app

import (
	"errors"
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

func TestRestartRefusedDuringShutdown(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	if !f.lifecycle.coordinator.Request() {
		t.Fatal("request")
	}
	if err := f.lifecycle.restartForUpdate(); err == nil || f.lifecycle.relaunch.Load() {
		t.Fatal("shutdown blocks restart")
	}
}
