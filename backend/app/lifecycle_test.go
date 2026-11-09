package app

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/database"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type fakeWindow struct{ hidden, shown, focused int }

func (w *fakeWindow) Hide()  { w.hidden++ }
func (w *fakeWindow) Show()  { w.shown++ }
func (w *fakeWindow) Focus() { w.focused++ }

type lifecycleFixture struct {
	lifecycle *appLifecycle
	window    *fakeWindow
	settings  *settingservice.SettingService
	prepared  *atomic.Int32
	quits     *atomic.Int32
}

func newLifecycleFixture(t *testing.T, behavior settingservice.MainWindowCloseBehavior, guards ...quitGuard) *lifecycleFixture {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	config, err := settings.GetWindowConfig()
	if err != nil {
		t.Fatal(err)
	}
	copied := *config
	copied.MainWindowCloseBehavior = behavior
	if err := settings.UpdateWindowConfig(&copied); err != nil {
		t.Fatal(err)
	}
	fixture := &lifecycleFixture{window: &fakeWindow{}, settings: settings, prepared: &atomic.Int32{}, quits: &atomic.Int32{}}
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		uiReadyTimeout: time.Millisecond,
		prepare:        func() error { fixture.prepared.Add(1); return nil },
		quit:           func() { fixture.quits.Add(1) },
	})
	fixture.lifecycle = &appLifecycle{window: fixture.window, settings: settings, coordinator: coordinator, guards: guards}
	fixture.lifecycle.trayAvailable.Store(true)
	return fixture
}

func (f *lifecycleFixture) waitForQuit(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.quits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.quits.Load() != 1 {
		t.Fatalf("quits = %d", f.quits.Load())
	}
}

func TestClosingHidesToTrayByDefault(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorHideToTray)
	if !f.lifecycle.MainWindowClosing() {
		t.Fatal("close should be prevented")
	}
	if f.window.hidden != 1 || f.lifecycle.coordinator.InProgress() {
		t.Fatalf("hidden %d, quitting %v", f.window.hidden, f.lifecycle.coordinator.InProgress())
	}
}

func TestClosingQuitsWhenConfigured(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.MainWindowClosing()
	f.waitForQuit(t)
	if f.window.hidden != 0 {
		t.Fatal("window hidden")
	}
}

func TestClosingQuitsWhenTrayIsUnavailable(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorHideToTray)
	f.lifecycle.trayAvailable.Store(false)
	f.lifecycle.MainWindowClosing()
	f.waitForQuit(t)
	if f.window.hidden != 0 {
		t.Fatal("hidden with no tray to reopen from")
	}
}

func TestRepeatedQuitRunsShutdownOnce(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.RequestQuit()
	f.lifecycle.RequestQuit()
	f.waitForQuit(t)
	f.lifecycle.RequestQuit()
	if f.prepared.Load() != 1 || f.quits.Load() != 1 {
		t.Fatalf("prepared %d, quits %d", f.prepared.Load(), f.quits.Load())
	}
}

func TestBeforeQuitDefersToShutdownThenAllowsIt(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	if !f.lifecycle.BeforeQuit() {
		t.Fatal("first quit must go through shutdown")
	}
	f.waitForQuit(t)
	if f.lifecycle.BeforeQuit() {
		t.Fatal("quit should be allowed after shutdown")
	}
}

func TestDirtySettingsBlockQuitUntilConfirmed(t *testing.T) {
	var resume func()
	dirty := true
	guard := func(_ string, proceed func()) bool {
		if !dirty {
			return false
		}
		resume = proceed
		return true
	}
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, guard)
	f.lifecycle.RequestQuit()
	if f.lifecycle.coordinator.InProgress() || resume == nil {
		t.Fatal("quit should wait for the user")
	}
	dirty = false
	resume()
	f.waitForQuit(t)
}

func TestCancelingAGuardKeepsTheApplicationRunning(t *testing.T) {
	blocked := 0
	guard := func(string, func()) bool { blocked++; return true }
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, guard)
	f.lifecycle.RequestQuit()
	time.Sleep(10 * time.Millisecond)
	if blocked != 1 || f.prepared.Load() != 0 || f.quits.Load() != 0 {
		t.Fatalf("blocked %d, prepared %d, quits %d", blocked, f.prepared.Load(), f.quits.Load())
	}
}

func TestGuardsRunInOrderAndStopAtTheFirstBlock(t *testing.T) {
	var calls []string
	first := func(string, func()) bool { calls = append(calls, "settings"); return true }
	second := func(string, func()) bool { calls = append(calls, "rewrite"); return false }
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, first, second)
	f.lifecycle.RequestQuit()
	if len(calls) != 1 || calls[0] != "settings" {
		t.Fatalf("calls %v", calls)
	}
}

func TestClosingDuringShutdownKeepsTheWindowOpen(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.coordinator.Request()
	if !f.lifecycle.MainWindowClosing() || f.window.shown != 1 || f.window.focused != 1 {
		t.Fatalf("shown %d focused %d", f.window.shown, f.window.focused)
	}
}

func TestQuitWhileResetIsPreparingIsIgnored(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.resetting.Store(true)
	f.lifecycle.RequestQuit()
	if f.lifecycle.coordinator.InProgress() {
		t.Fatal("quit started during reset preparation")
	}
	f.lifecycle.resetReady.Store(true)
	f.lifecycle.RequestQuit()
	f.waitForQuit(t)
}

func TestWindowClosesOnceShutdownHasFinished(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.RequestQuit()
	f.waitForQuit(t)
	if f.lifecycle.MainWindowClosing() {
		t.Fatal("the final quit must be able to close the window")
	}
}
