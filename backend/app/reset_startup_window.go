package app

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
	"github.com/josexy/flowlens/desktop/locale"
	"github.com/josexy/flowlens/desktop/resetstartup"
	"github.com/josexy/flowlens/desktop/theme"
)

// resetStartupActions writes what the window asks of the application process.
type resetStartupActions struct {
	directory string
	visible   func() bool
	show      func()

	mu       sync.Mutex
	sequence uint64
	ready    bool
}

func (a *resetStartupActions) run(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "show" {
		a.show()
		if !a.visible() {
			return errors.New("reset progress window did not become visible")
		}
		return nil
	}
	if name == "ready" {
		if !a.visible() {
			return errors.New("reset progress window is not visible")
		}
		a.ready = true
	}
	if name == "retry" && !a.ready {
		return errors.New("application_reset_in_progress")
	}
	a.sequence++
	return writeResetStartupJSON(a.directory, "action.json", resetStartupAction{Sequence: a.sequence, Action: name, Ready: a.ready})
}

// runResetStartupWindow shows the separate Restore Initial State progress
// process. It opens no application data.
func runResetStartupWindow() {
	directory, err := resetStartupSessionDirectory()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	read := func() (appservice.ResetStartupState, error) { return readResetStartupState(directory) }
	initial, err := read()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	mygo.App.SetName(appservice.APP_NAME)
	var quitting atomic.Bool
	stop := make(chan struct{})
	defer close(stop)
	mygo.App.WhenReady(func() {
		locale.Apply(initial.Language)
		if err := theme.RegisterFonts(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		var window *mygo.Window
		actions := &resetStartupActions{
			directory: directory,
			visible:   func() bool { return window.IsVisible() },
			show: func() {
				window.Show()
				window.Focus()
			},
		}
		service := appservice.NewResetStartupService(read, actions.run)
		view := &resetstartup.View{
			Service: service, Config: theme.Resolve(theme.Appearance{}).Config,
			Spawn: func(run func()) { go run() },
			Post:  func(run func()) { window.Update(run) },
		}
		view.Apply(initial)
		window = mygo.NewWindow(mygo.WindowOptions{
			Title: appservice.APP_NAME, Width: 480, Height: 380, MinWidth: 400, MinHeight: 340,
			Hidden: true, DisableResize: true, Content: ui.View(view.Build),
		})
		window.OnClose(func(event *mygo.CloseEvent) {
			if quitting.Load() {
				return
			}
			event.PreventDefault()
			go func() { _ = actions.run("exit") }()
		})
		window.OnReadyToShow(view.Show)
		// Polling also ends the window if the application process goes away.
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					state, err := read()
					if err != nil || state.Close {
						quitting.Store(true)
						mygo.App.Quit()
						return
					}
					window.Update(func() { view.Apply(state) })
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
}
