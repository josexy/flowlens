package app

import (
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/locale"
	"github.com/josexy/flowlens/internal/desktop/startupfailure"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/pkg/logger"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

// startupRecovery keeps FlowLens open after a failed startup: a window shows
// the failure and retries, and the main window replaces it once a retry
// succeeds.
type startupRecovery struct {
	attempt startupAttempt
	// show displays a failure, open replaces the window with the main window
	// and quit leaves. They run on the main thread, which post reaches.
	post func(func())
	show func(*startupError)
	open func(*launched)
	quit func()

	pending sync.WaitGroup
	mu      sync.Mutex
	started *launched

	// Main thread only.
	window   *mygo.Window
	view     *startupfailure.View
	quitting bool
	offs     []func()
}

func newStartupRecovery(first *launched, attempt startupAttempt) *startupRecovery {
	r := &startupRecovery{attempt: attempt, started: first, quit: mygo.App.Quit}
	r.post = func(run func()) {
		if r.window != nil {
			r.window.Update(run)
		}
	}
	r.show = r.showWindow
	r.open = r.replaceWindow
	return r
}

// spawn runs work off the main thread. finish waits for it.
func (r *startupRecovery) spawn(run func()) {
	r.pending.Add(1)
	go func() {
		defer r.pending.Done()
		run()
	}()
}

// fail shows a failure on the main thread.
func (r *startupRecovery) fail(failure *startupError) { r.show(failure) }

// retry starts the application again and reports the outcome on the main
// thread. It blocks, so the window spawns it.
func (r *startupRecovery) retry() {
	started, failure, canceled := r.attempt()
	if started != nil {
		// Whatever happens to the window, the event loop's end stops it.
		r.mu.Lock()
		r.started = started
		r.mu.Unlock()
	}
	r.post(func() {
		switch {
		case canceled:
			r.quit()
		case failure != nil:
			r.show(failure)
		default:
			r.open(started)
		}
	})
}

// finish waits for work still running once the event loop has ended and
// returns the application it started, if any.
func (r *startupRecovery) finish() *launched {
	r.pending.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

// showWindow opens the startup failure window, or shows a later failure in it.
func (r *startupRecovery) showWindow(failure *startupError) {
	locale.Apply(failure.language)
	if r.window != nil {
		r.view.Fail(failure.failure())
		r.window.Show()
		r.window.Focus()
		return
	}
	if err := theme.RegisterFonts(); err != nil {
		logger.G().Warnf("Register fonts: %v", err)
	}
	r.view = &startupfailure.View{
		Config:   theme.Resolve(theme.Appearance{}).Config,
		Retry:    r.retry,
		OpenLogs: logger.OpenDir,
		Quit:     r.quit,
		Spawn:    r.spawn,
		Post:     r.post,
	}
	r.view.Fail(failure.failure())
	r.window = mygo.NewWindow(mygo.WindowOptions{
		Title: appservice.APP_NAME, Width: 560, Height: 400, MinWidth: 440, MinHeight: 320,
		Hidden: true, Content: ui.View(r.view.Build),
	})
	show := func() {
		r.window.Show()
		r.window.Focus()
	}
	r.window.OnReadyToShow(show)
	// An attempt has to finish before closing, so it can release what it
	// opened; quitting from the system waits for it after the event loop.
	r.window.OnClose(func(event *mygo.CloseEvent) {
		if r.view.Retrying() && !r.quitting {
			event.PreventDefault()
		}
	})
	r.offs = append(r.offs,
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { r.quitting = true }),
		mygo.App.OnQuitCanceled(func() { r.quitting = false }),
		mygo.App.OnSecondInstance(func([]string, string) { mygo.RunOnMain(show) }),
	)
}

// replaceWindow opens the main window and closes the failure window once the
// main window shows.
func (r *startupRecovery) replaceWindow(started *launched) {
	openMainWindow(started, func() {
		for _, off := range r.offs {
			off()
		}
		r.offs = nil
		r.window.Destroy()
	})
}
