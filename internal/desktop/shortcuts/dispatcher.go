package shortcuts

import (
	"slices"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

// Handler runs a command on the current page.
type Handler struct {
	CommandID string
	// When reports whether the page can take the command now. Nil means yes.
	When func() bool
	Run  func()
	// Priority picks among handlers of one command. Among equals, the last
	// declared wins, which is the innermost page.
	Priority int
}

// Dispatcher routes key presses of one window to its handlers. Views declare
// handlers while building, so only what is on screen can run.
type Dispatcher struct {
	config    func() *settingservice.ShortcutConfig
	suspended bool
	macOS     bool
	handlers  []Handler
	modal     bool
}

// NewDispatcher returns a dispatcher reading saved bindings from config.
func NewDispatcher(config func() *settingservice.ShortcutConfig) *Dispatcher {
	return &Dispatcher{config: config, macOS: IsMacOS()}
}

// Begin starts a build pass.
func (d *Dispatcher) Begin() {
	d.suspended = false
	d.handlers = d.handlers[:0]
	d.modal = false
}

// Handle declares a handler for this build pass.
func (d *Dispatcher) Handle(handler Handler) { d.handlers = append(d.handlers, handler) }

// Suspend turns off every binding for this build pass.
func (d *Dispatcher) Suspend() { d.suspended = true }

// NoteModal records that a dialog is open in this build pass.
func (d *Dispatcher) NoteModal() { d.modal = true }

type keyPress struct {
	modifiers ui.Modifiers
	key       ui.Key
}

// Install connects the resolved bindings to the window. Call it once per build
// pass, after the view declared its handlers.
func (d *Dispatcher) Install(c *ui.Context) {
	if d.suspended {
		return
	}
	editable := c.FocusTakesText()
	groups := map[keyPress][]string{}
	var order []keyPress
	config := d.config()
	for _, command := range Catalog {
		resolved := ResolveCommand(command, config)
		// System-wide shortcuts are registered with the operating system
		// and must not also run from the window.
		if resolved.Scope == settingservice.ShortcutScopeGlobal || resolved.Binding == nil {
			continue
		}
		if (d.modal && !command.AllowInModal) || (editable && !command.AllowInEditable) {
			continue
		}
		binding, ok := Resolve(resolved.Binding, d.macOS)
		if !ok {
			continue
		}
		key, ok := uiKey(binding.Key)
		if !ok {
			continue
		}
		press := keyPress{modifiers: uiModifiers(binding), key: key}
		if _, seen := groups[press]; !seen {
			order = append(order, press)
		}
		groups[press] = append(groups[press], command.ID)
	}
	for _, press := range order {
		handler, ok := d.selectHandler(groups[press])
		if !ok {
			continue
		}
		c.OnWindowShortcut(press.modifiers, press.key, handler.Run)
	}
}

func (d *Dispatcher) selectHandler(ids []string) (Handler, bool) {
	var selected Handler
	found := false
	for _, handler := range d.handlers {
		matched := slices.Contains(ids, handler.CommandID)
		if !matched || (handler.When != nil && !handler.When()) {
			continue
		}
		if !found || handler.Priority >= selected.Priority {
			selected, found = handler, true
		}
	}
	return selected, found
}

// Invoke runs a command requested from outside the window, such as a
// system-wide shortcut. It reports whether a handler ran.
func (d *Dispatcher) Invoke(id string) bool {
	command, known := CommandByID(id)
	if !known || (d.modal && !command.AllowInModal) {
		return false
	}
	handler, ok := d.selectHandler([]string{id})
	if ok {
		handler.Run()
	}
	return ok
}
