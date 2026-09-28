package app

import (
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// matchUpdaterAsset selects either an updater-compatible artifact or the best
// manual-install artifact for the current installation. Keeping both paths in
// the same provider lets every released platform check versions and read notes.
func matchUpdaterAsset(req updater.CheckRequest, assets []github.ReleaseAsset, selfUpdate bool) int {
	var suffixes []string
	switch req.Platform {
	case "darwin":
		if selfUpdate {
			suffixes = []string{"_darwin_universal.zip"}
		} else if req.Arch == "arm64" {
			suffixes = []string{"_macos_arm64.dmg", "_macos_universal.dmg"}
		} else {
			suffixes = []string{"_macos_universal.dmg"}
		}
	case "windows":
		if req.Arch == "amd64" {
			if selfUpdate {
				suffixes = []string{"_windows_amd64.exe"}
			} else {
				suffixes = []string{"_windows_x64_setup.exe"}
			}
		}
	case "linux":
		if !selfUpdate && req.Arch == "amd64" {
			suffixes = []string{"_linux_x64.appimage"}
		}
	}
	for _, suffix := range suffixes {
		for index, asset := range assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), suffix) {
				return index
			}
		}
	}
	return -1
}

// updaterHost connects the headless framework updater to FlowLens events and
// shutdown. Restart is routed through a permanent application listener so
// closing the custom update window cannot remove the guarded restart entry point.
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
