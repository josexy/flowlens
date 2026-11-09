package app

import (
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/update"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
)

// updateWindowController opens the single Software Update window.
type updateWindowController struct {
	data     *store.Store
	canClose func() bool
	blocked  func() bool

	window *mygo.Window
	view   *update.View
}

func (s *updateWindowController) open() {
	if s.blocked() {
		return
	}
	if s.window != nil {
		s.window.Show()
		s.window.Focus()
		return
	}
	options := mygo.WindowOptions{
		Title: locales.UpdaterWindowTitle(), Width: updaterWindowDefaultWidth, Height: updaterWindowDefaultHeight,
		MinWidth: updaterWindowMinWidth, MinHeight: updaterWindowMinHeight,
	}
	frame := secondaryFrame(&options, s.data.Settings.UsesCustomFrame(), locales.UpdaterWindowTitle(), func() *mygo.Window { return s.window })
	s.view = update.New(s.data, update.Host{
		OpenURL: mygo.Shell.OpenExternal,
		Close: func() {
			if s.window != nil {
				s.window.Destroy()
			}
		},
		Frame: frame,
	}, &widgets.Notifier{})
	options.Content = ui.View(s.view.Build)
	window := mygo.NewWindow(options)
	s.window = window
	s.data.Runtime.Dispatcher.Register(updaterWindowName, window)
	window.OnClose(func(event *mygo.CloseEvent) {
		if !s.canClose() && !s.view.CloseRequested() {
			event.PreventDefault()
		}
	})
	window.OnClosed(func() {
		s.data.Runtime.Dispatcher.Unregister(updaterWindowName)
		s.window, s.view = nil, nil
	})
	window.Show()
	window.Focus()
}

// updateRestartHandler restarts into a staged update through the guards that
// protect quitting.
type updateRestartHandler struct {
	blocked func(proceed func()) bool
	restart func() error
	failed  func(error)

	inFlight atomic.Bool
}

func (h *updateRestartHandler) request() {
	if !h.inFlight.CompareAndSwap(false, true) {
		return
	}
	if h.blocked(func() { go h.request() }) {
		h.inFlight.Store(false)
		return
	}
	if err := h.restart(); err != nil {
		h.inFlight.Store(false)
		logger.G().Warnf("Restart for update failed: %v", err)
		h.failed(err)
	}
}
