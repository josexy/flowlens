package app

import (
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	shortcutservice "github.com/josexy/flowlens/backend/services/shortcut_service"
)

// hotkeyModule is the part of MyGo's global shortcut module FlowLens uses.
type hotkeyModule interface {
	Register(accelerator string, callback func()) error
	Unregister(accelerator string)
	IsRegistered(accelerator string) bool
}

// systemShortcuts registers system-wide shortcuts with the desktop. Accelerators
// from the shortcut service ("Cmd+Shift+K", "Option+A") are what MyGo parses.
type systemShortcuts struct{ hotkeys hotkeyModule }

func newSystemShortcuts() systemShortcuts { return systemShortcuts{hotkeys: mygo.GlobalShortcut} }

func (s systemShortcuts) Register(accelerator string, callback func()) error {
	return s.hotkeys.Register(accelerator, callback)
}

func (s systemShortcuts) Unregister(accelerator string) error {
	s.hotkeys.Unregister(accelerator)
	return nil
}

func (s systemShortcuts) IsRegistered(accelerator string) bool {
	return s.hotkeys.IsRegistered(accelerator)
}

// shortcutInvoker runs system-wide shortcuts on the main thread. Rapid presses
// of the proxy toggle collapse into one.
type shortcutInvoker struct {
	onMain        func(func())
	show          func()
	toggleProxy   func()
	debounce      time.Duration
	afterDebounce func(time.Duration, func())
	inFlight      atomic.Bool
}

func (i *shortcutInvoker) invoke(commandID string) {
	switch commandID {
	case shortcutservice.CommandShowMainWindow:
		i.onMain(i.show)
	case shortcutservice.CommandToggleProxy:
		if !i.inFlight.CompareAndSwap(false, true) {
			return
		}
		i.onMain(func() {
			i.show()
			i.toggleProxy()
		})
		i.afterDebounce(i.debounce, func() { i.inFlight.Store(false) })
	}
}
