package app

import (
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// matchUpdaterAsset prefers the updater-specific artifacts when a release also
// contains installers or legacy packages that would otherwise match Wails'
// filename heuristic. Other platform/architecture combinations retain Wails'
// default matching behavior.
func matchUpdaterAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	suffix := ""
	switch req.Platform {
	case "darwin":
		suffix = "_darwin_universal.zip"
	case "windows":
		if req.Arch == "amd64" {
			suffix = "_windows_amd64.exe"
		}
	case "linux":
		return -1 // The built-in updater cannot replace an AppImage mount.
	}
	if suffix != "" {
		for index, asset := range assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), suffix) {
				return index
			}
		}
		return -1
	}
	return github.DefaultAssetMatcher(req, assets)
}

// updaterHost keeps the framework's download/window flow, but routes restart
// through FlowLens before Wails spawns its helper. A permanent app listener owns
// that action so closing the updater window cannot remove the restart entry point.
type updaterHost struct {
	app  *application.App
	quit func()
}

func (h *updaterHost) Emit(name string, data ...any) bool {
	return h.app.Event.Emit(name, data...)
}

func (h *updaterHost) OnEvent(name string, callback func(any)) func() {
	if name == updater.EventUserRestart {
		return func() {}
	}
	return h.app.Event.On(name, func(event *application.CustomEvent) { callback(event.Data) })
}

func (h *updaterHost) OpenWindow(opts updater.WindowOptions) updater.WindowHandle {
	window := h.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: opts.Title, Width: opts.Width, Height: opts.Height,
		Frameless: opts.Frameless, AlwaysOnTop: opts.AlwaysOnTop,
		DisableResize: opts.DisableResize, HTML: opts.InitialHTML,
		AllowSimpleEventEmit: true,
	})
	window.Show()
	return window.AsUpdaterWindow()
}

func (h *updaterHost) Quit() { h.quit() }

type updaterRestartHandler struct {
	inFlight atomic.Bool
	// allowRestart opens the existing settings confirmation when necessary.
	allowRestart func() bool
	restart      func() error
}

func (h *updaterRestartHandler) request() error {
	if !h.inFlight.CompareAndSwap(false, true) {
		return nil
	}
	if !h.allowRestart() {
		h.inFlight.Store(false)
		return nil
	}
	if err := h.restart(); err != nil {
		h.inFlight.Store(false)
		return err
	}
	// A successful restart has spawned the helper and started shutdown. Keep
	// subsequent clicks from spawning additional helpers against the same file.
	return nil
}
