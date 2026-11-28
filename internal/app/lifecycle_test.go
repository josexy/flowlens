package app

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/rewrite"
	"github.com/josexy/flowlens/internal/pkg/database"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
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
	t.Setenv("APPDATA", t.TempDir())
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
	fixture.lifecycle = &appLifecycle{window: fixture.window, settings: settings, coordinator: coordinator, guards: guards, onMain: func(fn func()) { fn() }}
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

func TestResolvedGuardContinuesTheQuitOnTheMainThread(t *testing.T) {
	var resume func()
	guard := func(_ string, proceed func()) bool {
		if resume != nil {
			return false
		}
		resume = proceed
		return true
	}
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit, guard)
	var onMain []string
	f.lifecycle.onMain = func(fn func()) {
		onMain = append(onMain, "posted")
		fn()
	}
	f.lifecycle.RequestQuit()
	if len(onMain) != 0 || f.lifecycle.coordinator.InProgress() {
		t.Fatal("the guard did not hold the quit")
	}
	resume()
	f.waitForQuit(t)
	if len(onMain) != 1 {
		t.Fatalf("the resolved quit continued %d times through onMain, want once", len(onMain))
	}
}

func TestClosingDuringShutdownKeepsTheWindowOpen(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	// Without the fixture's 1 ms UI-ready timeout, shutdown cannot finish
	// before the close is checked.
	f.lifecycle.coordinator = newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{})
	f.lifecycle.coordinator.Request()
	if !f.lifecycle.MainWindowClosing() || f.window.shown != 1 || f.window.focused != 1 {
		t.Fatalf("shown %d focused %d", f.window.shown, f.window.focused)
	}
}

func TestWindowClosesOnceShutdownHasFinished(t *testing.T) {
	f := newLifecycleFixture(t, settingservice.MainWindowCloseBehaviorQuit)
	f.lifecycle.RequestQuit()
	f.waitForQuit(t)
	if f.lifecycle.MainWindowClosing() {
		t.Fatal("the final quit must be able to close the window")
	}
}

// fakeRewriteView holds one quit prompt, as the rewrite view does: a reply
// clears the prompt before the decision is delivered.
// Its lock lets tests poll it while a restart continues on a goroutine.
type fakeRewriteView struct {
	mu     sync.Mutex
	dirty  bool
	prompt *rewrite.QuitPrompt
	asked  []string
}

func (v *fakeRewriteView) Dirty() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dirty
}

func (v *fakeRewriteView) QuitPending() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.prompt != nil
}

func (v *fakeRewriteView) AskQuit(prompt rewrite.QuitPrompt) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.prompt = &prompt
	v.asked = append(v.asked, prompt.Reason)
}

func (v *fakeRewriteView) askedReasons() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.asked)
}

func (v *fakeRewriteView) currentPrompt() *rewrite.QuitPrompt {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.prompt
}

func (v *fakeRewriteView) reply(decision rewrite.QuitDecision) {
	v.mu.Lock()
	prompt := v.prompt
	v.prompt = nil
	if decision != rewrite.DecisionCancel {
		v.dirty = false
	}
	v.mu.Unlock()
	prompt.Respond(decision)
}

func TestRewriteQuitGuardPassesWithoutUnsavedRules(t *testing.T) {
	view := &fakeRewriteView{}
	guard := &rewriteQuitGuard{view: view}
	if guard.guard(quitReasonQuit, func() { t.Fatal("proceeded") }) || len(view.askedReasons()) != 0 {
		t.Fatal("clean rules blocked the quit")
	}
}

func TestRewriteQuitGuardProceedsOnlyAfterSaveOrDiscard(t *testing.T) {
	for _, decision := range []rewrite.QuitDecision{rewrite.DecisionSave, rewrite.DecisionDiscard} {
		view := &fakeRewriteView{dirty: true}
		shown := 0
		guard := &rewriteQuitGuard{view: view, show: func() { shown++ }}
		proceeded := 0
		if !guard.guard(quitReasonUpdate, func() { proceeded++ }) {
			t.Fatal("unsaved rules did not block")
		}
		if shown != 1 || len(view.askedReasons()) != 1 || view.askedReasons()[0] != quitReasonUpdate || proceeded != 0 {
			t.Fatalf("shown %d, asked %v, proceeded %d", shown, view.askedReasons(), proceeded)
		}
		view.reply(decision)
		if proceeded != 1 {
			t.Fatalf("decision %d proceeded %d times", decision, proceeded)
		}
		if guard.guard(quitReasonQuit, func() {}) {
			t.Fatal("resolved rules still block")
		}
	}
}

func TestRewriteQuitGuardIgnoresStaleDecisionsAfterACancel(t *testing.T) {
	view := &fakeRewriteView{dirty: true}
	guard := &rewriteQuitGuard{view: view}
	var quits, updates int
	guard.guard(quitReasonQuit, func() { quits++ })
	cancelled := view.currentPrompt()
	view.reply(rewrite.DecisionCancel)
	if quits != 0 || !view.Dirty() {
		t.Fatal("cancel proceeded or lost the unsaved rules")
	}
	if !guard.guard(quitReasonUpdate, func() { updates++ }) {
		t.Fatal("cancel cleared the guard")
	}
	// A decision for the cancelled prompt authorizes nothing.
	cancelled.Respond(rewrite.DecisionDiscard)
	if quits != 0 || updates != 0 || !view.QuitPending() {
		t.Fatalf("stale decision: quits %d, updates %d", quits, updates)
	}
	view.reply(rewrite.DecisionSave)
	if quits != 0 || updates != 1 {
		t.Fatalf("quits %d, updates %d", quits, updates)
	}
}

func TestRewriteQuitGuardBlocksASecondQuitWhileAsking(t *testing.T) {
	view := &fakeRewriteView{dirty: true}
	guard := &rewriteQuitGuard{view: view}
	var first, second int
	guard.guard(quitReasonQuit, func() { first++ })
	if !guard.guard(quitReasonUpdate, func() { second++ }) || len(view.askedReasons()) != 1 {
		t.Fatalf("a second request replaced the open prompt: asked %v", view.askedReasons())
	}
	view.reply(rewrite.DecisionDiscard)
	if first != 1 || second != 0 {
		t.Fatalf("first %d, second %d", first, second)
	}
}

func TestGracefulShutdownCoordinatorWaitsForUIReadyAndRunsOnce(t *testing.T) {
	var showCalls atomic.Int32
	var prepareCalls atomic.Int32
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	quitCalled := make(chan struct{}, 1)

	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		showLoading: func() {
			showCalls.Add(1)
		},
		prepare: func() error {
			prepareCalls.Add(1)
			close(prepareStarted)
			<-releasePrepare
			return nil
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	const callers = 16
	var requestWinners atomic.Int32
	var requests sync.WaitGroup
	for range callers {
		requests.Go(func() {
			if coordinator.Request() {
				requestWinners.Add(1)
			}
		})
	}
	requests.Wait()

	if got := requestWinners.Load(); got != 1 {
		t.Fatalf("Request winners = %d, want 1", got)
	}
	if got := showCalls.Load(); got != 1 {
		t.Fatalf("show loading calls = %d, want 1", got)
	}
	if !coordinator.InProgress() {
		t.Fatal("coordinator should report shutdown in progress after Request")
	}
	if coordinator.CanQuit() {
		t.Fatal("coordinator must not allow the application to quit before preparation")
	}

	select {
	case <-prepareStarted:
		t.Fatal("preparation started before the UI acknowledged the loading state")
	case <-time.After(20 * time.Millisecond):
	}

	var readyWinners atomic.Int32
	var readyCalls sync.WaitGroup
	for range callers {
		readyCalls.Go(func() {
			if coordinator.UIReady() {
				readyWinners.Add(1)
			}
		})
	}
	readyCalls.Wait()
	if got := readyWinners.Load(); got != 1 {
		t.Fatalf("UIReady winners = %d, want 1", got)
	}
	waitForShutdownSignal(t, prepareStarted)
	close(releasePrepare)
	waitForShutdownSignal(t, quitCalled)

	if got := prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls = %d, want 1", got)
	}
	if !coordinator.CanQuit() {
		t.Fatal("coordinator should allow the application to quit after preparation")
	}
	if coordinator.InProgress() {
		t.Fatal("coordinator should no longer report shutdown in progress")
	}
}

func TestGracefulShutdownCoordinatorFallsBackWithoutUIReady(t *testing.T) {
	prepareCalled := make(chan struct{}, 1)
	quitCalled := make(chan struct{}, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		uiReadyTimeout: 20 * time.Millisecond,
		prepare: func() error {
			prepareCalled <- struct{}{}
			return nil
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	coordinator.Request()
	waitForShutdownSignal(t, prepareCalled)
	waitForShutdownSignal(t, quitCalled)
	if !coordinator.CanQuit() {
		t.Fatal("fallback preparation should allow the application to quit")
	}
	if coordinator.UIReady() {
		t.Fatal("late UI acknowledgement must not start preparation again")
	}
}

func TestGracefulShutdownCoordinatorHonoursMinimumVisibleDuration(t *testing.T) {
	const minimumVisible = 60 * time.Millisecond
	quitCalled := make(chan time.Time, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		minimumVisible: minimumVisible,
		prepare: func() error {
			return nil
		},
		quit: func() {
			quitCalled <- time.Now()
		},
	})

	coordinator.Request()
	readyAt := time.Now()
	coordinator.UIReady()
	quitAt := waitForShutdownValue(t, quitCalled)
	if elapsed := quitAt.Sub(readyAt); elapsed < minimumVisible-10*time.Millisecond {
		t.Fatalf("loading visible for %s, want at least %s", elapsed, minimumVisible)
	}
}

func TestGracefulShutdownCoordinatorReportsErrorAndStillQuits(t *testing.T) {
	wantErr := errors.New("save settings")
	errorReported := make(chan error, 1)
	quitCalled := make(chan struct{}, 1)
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		prepare: func() error {
			return wantErr
		},
		onPrepareError: func(err error) {
			errorReported <- err
		},
		quit: func() {
			quitCalled <- struct{}{}
		},
	})

	coordinator.Request()
	coordinator.UIReady()
	if got := waitForShutdownValue(t, errorReported); !errors.Is(got, wantErr) {
		t.Fatalf("reported error = %v, want %v", got, wantErr)
	}
	waitForShutdownSignal(t, quitCalled)
	if !coordinator.CanQuit() {
		t.Fatal("preparation errors must not prevent the selected best-effort exit policy")
	}
}

func TestGracefulShutdownCoordinatorPrepareAndWaitIsIdempotent(t *testing.T) {
	wantErr := errors.New("restore system proxy")
	var prepareCalls atomic.Int32
	coordinator := newGracefulShutdownCoordinator(gracefulShutdownCoordinatorConfig{
		prepare: func() error {
			prepareCalls.Add(1)
			return wantErr
		},
	})

	for range 2 {
		if got := coordinator.PrepareAndWait(); !errors.Is(got, wantErr) {
			t.Fatalf("PrepareAndWait error = %v, want %v", got, wantErr)
		}
	}
	if got := prepareCalls.Load(); got != 1 {
		t.Fatalf("prepare calls = %d, want 1", got)
	}
	if coordinator.CanQuit() {
		t.Fatal("synchronous fallback preparation alone must not re-enter app.Quit")
	}
}

func waitForShutdownSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	waitForShutdownValue(t, signal)
}

func waitForShutdownValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown coordinator")
		var zero T
		return zero
	}
}
