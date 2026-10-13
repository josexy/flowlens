package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	appservice "github.com/josexy/flowlens/backend/services/app_service"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func readResetStartupState(directory string) (appservice.ResetStartupState, error) {
	var snapshot resetStartupSnapshot
	if err := readResetStartupJSON(directory, "state.json", &snapshot); err != nil {
		return appservice.ResetStartupState{}, err
	}
	if snapshot.Token != filepath.Base(directory) || time.Since(time.UnixMilli(snapshot.UpdatedAt)) > 30*time.Second {
		return appservice.ResetStartupState{}, errors.New("reset startup session expired")
	}
	return snapshot.ResetStartupState, nil
}

func runResetStartupWindow(assets Assets) {
	directory, err := resetStartupSessionDirectory()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	// Validate the private UI session independently of the reset manifest so a
	// damaged marker can be reported without loading any application data.
	read := func() (appservice.ResetStartupState, error) { return readResetStartupState(directory) }
	if _, err := read(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	app := application.New(application.Options{
		Name: "FlowLens", Icon: assets.AppIcon,
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(assets.Frontend)},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Linux: application.LinuxOptions{ProgramName: "FlowLens", DisableQuitOnLastWindowClosed: true},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "reset-startup", Title: "FlowLens", URL: "/#/reset-startup",
		Width: 480, Height: 380, MinWidth: 400, MinHeight: 340,
		Hidden: true, DisableResize: true, InitialPosition: application.WindowCentered,
		BackgroundColour: application.NewRGBA(36, 36, 41, 255),
		Linux:            application.LinuxWindow{Icon: assets.AppIcon},
	})
	var actionMu sync.Mutex
	var sequence uint64
	var frontendReady bool
	action := func(name string) error {
		actionMu.Lock()
		defer actionMu.Unlock()
		if name == "show" {
			window.Show().Focus()
			if !window.IsVisible() {
				return errors.New("reset progress window did not become visible")
			}
			return nil
		}
		if name == "ready" && !window.IsVisible() {
			return errors.New("reset progress window is not visible")
		}
		if name == "ready" {
			frontendReady = true
		}
		if name == "retry" && !frontendReady {
			return errors.New("application_reset_in_progress")
		}
		sequence++
		return writeResetStartupJSON(directory, "action.json", resetStartupAction{Sequence: sequence, Action: name, Ready: frontendReady})
	}
	service := appservice.NewResetStartupService(read, action)
	app.RegisterService(application.NewService(service))
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		_ = action("exit")
	})
	// Close even if Vue fails to load or the application process is interrupted.
	// The heartbeat also prevents orphaned progress windows after a crash.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				state, err := read()
				if err != nil || state.Close {
					app.Quit()
					return
				}
			}
		}
	}()
	if err := app.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
}
