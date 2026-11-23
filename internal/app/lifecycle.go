package app

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/josexy/flowlens/internal/pkg/logger"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

// quitGuard interrupts quitting for unsaved work. It runs on the main thread
// and returns true when it took over; proceed continues the quit once the user
// has resolved the work. A guard never calls proceed while it is being asked.
type quitGuard func(reason string, proceed func()) (blocked bool)

// Reasons a guard is asked to protect unsaved work.
const (
	quitReasonQuit   = "quit"
	quitReasonUpdate = "update"
)

// mainWindowControl is what closing and quitting need from the main window.
type mainWindowControl interface {
	Hide()
	Show()
	Focus()
}

// appLifecycle decides what closing the main window and quitting do.
type appLifecycle struct {
	window      mainWindowControl
	settings    *settingservice.SettingService
	saveGeom    func()
	coordinator *gracefulShutdownCoordinator
	guards      []quitGuard
	// onMain runs a function on the main thread, where the guards read the
	// windows' state: mygo.RunOnMain, or an immediate call in tests.
	onMain func(func())

	trayAvailable atomic.Bool
	relaunch      atomic.Bool
	mu            sync.Mutex
}

// RequestQuit begins quitting unless unsaved work needs the user first. It
// runs on the main thread: the tray, quitting, and closing the window call it
// there, and a resolved guard continues through onMain.
func (l *appLifecycle) RequestQuit() {
	l.mu.Lock()
	defer l.mu.Unlock()
	logger.G().Info("Application quit requested")
	if l.coordinator.InProgress() {
		return
	}
	if l.blockedByGuards(quitReasonQuit, func() { l.onMain(l.RequestQuit) }) {
		return
	}
	l.coordinator.Request()
}

// restartForUpdate shuts down and starts the staged version. It fails, leaving
// the application running, when a shutdown is already under way.
func (l *appLifecycle) restartForUpdate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.coordinator.InProgress() || l.coordinator.CanQuit() {
		return errors.New("the application is already shutting down")
	}
	l.relaunch.Store(true)
	if !l.coordinator.Request() {
		l.relaunch.Store(false)
		return errors.New("the application is already shutting down")
	}
	return nil
}

// blockedByGuards asks the guards in order; it runs on the main thread.
func (l *appLifecycle) blockedByGuards(reason string, proceed func()) bool {
	for _, guard := range l.guards {
		if guard(reason, proceed) {
			return true
		}
	}
	return false
}

// BeforeQuit reports whether the application must stay open: quitting goes
// through the shutdown coordinator first.
func (l *appLifecycle) BeforeQuit() (prevent bool) {
	if l.coordinator.CanQuit() {
		return false
	}
	l.RequestQuit()
	return true
}

// MainWindowClosing reports whether the window must stay open. Closing hides to
// the tray or quits, by the user's setting.
func (l *appLifecycle) MainWindowClosing() (prevent bool) {
	if l.coordinator.CanQuit() {
		return false // quitting closes every window, this one included
	}
	if l.coordinator.InProgress() {
		logger.G().Info("Main window close ignored while application shutdown is in progress")
		l.window.Show()
		l.window.Focus()
		return true
	}
	if l.saveGeom != nil {
		l.saveGeom()
	}
	if err := l.settings.Save(); err != nil {
		logger.G().Errorf("Save settings on window close failed: %v", err)
	}
	quit := currentMainWindowCloseBehavior(l.settings) == settingservice.MainWindowCloseBehaviorQuit
	if quit || !l.trayAvailable.Load() {
		logger.G().Info("Main window close intercepted, requesting application quit")
		l.RequestQuit()
		return true
	}
	logger.G().Info("Main window close intercepted, hiding to tray")
	l.window.Hide()
	return true
}
