package app

import (
	"errors"
	"regexp"
	"testing"
	"time"

	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
)

type fakeHotkeys struct {
	registered map[string]func()
	err        error
}

func (f *fakeHotkeys) Register(accelerator string, callback func()) error {
	if f.err != nil {
		return f.err
	}
	if f.registered == nil {
		f.registered = map[string]func(){}
	}
	f.registered[accelerator] = callback
	return nil
}
func (f *fakeHotkeys) Unregister(accelerator string) { delete(f.registered, accelerator) }
func (f *fakeHotkeys) IsRegistered(accelerator string) bool {
	_, ok := f.registered[accelerator]
	return ok
}

func TestSystemShortcutsForwardToTheDesktop(t *testing.T) {
	hotkeys := &fakeHotkeys{}
	shortcuts := systemShortcuts{hotkeys: hotkeys}
	called := 0
	if err := shortcuts.Register("Cmd+Shift+K", func() { called++ }); err != nil {
		t.Fatal(err)
	}
	if !shortcuts.IsRegistered("Cmd+Shift+K") {
		t.Fatal("not registered")
	}
	hotkeys.registered["Cmd+Shift+K"]()
	if err := shortcuts.Unregister("Cmd+Shift+K"); err != nil || shortcuts.IsRegistered("Cmd+Shift+K") || called != 1 {
		t.Fatalf("err %v, called %d", err, called)
	}
}

func TestRegistrationFailureReachesTheService(t *testing.T) {
	boom := errors.New("in use")
	if err := (systemShortcuts{hotkeys: &fakeHotkeys{err: boom}}).Register("Ctrl+A", func() {}); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

// The shortcut service writes accelerators in these forms; MyGo's parser takes
// the modifier names cmd, ctrl, option, shift, and super, ignoring case.
func TestAcceleratorFormatsAreWhatMyGoParses(t *testing.T) {
	pattern := regexp.MustCompile(`^((Cmd|Ctrl|Option|Shift|Super)\+)*[A-Za-z0-9,.;'\[\]\\/=` + "`" + `-]+$`)
	for _, accelerator := range []string{"Ctrl+A", "Cmd+Shift+K", "Option+Space", "Super+Tab", "Ctrl+Option+F5"} {
		if !pattern.MatchString(accelerator) {
			t.Errorf("%q is not in the expected form", accelerator)
		}
	}
}

func newInvoker(log *[]string) (*shortcutInvoker, *func()) {
	var release func()
	invoker := &shortcutInvoker{
		onMain:      func(fn func()) { fn() },
		show:        func() { *log = append(*log, "show") },
		toggleProxy: func() { *log = append(*log, "toggle") },
		debounce:    time.Second,
		afterDebounce: func(_ time.Duration, run func()) {
			release = run
		},
	}
	return invoker, &release
}

func TestShowMainWindowShortcut(t *testing.T) {
	var log []string
	invoker, _ := newInvoker(&log)
	invoker.invoke(shortcutservice.CommandShowMainWindow)
	if len(log) != 1 || log[0] != "show" {
		t.Fatalf("log %v", log)
	}
}

func TestRapidProxyTogglesCollapseIntoOne(t *testing.T) {
	var log []string
	invoker, release := newInvoker(&log)
	invoker.invoke(shortcutservice.CommandToggleProxy)
	invoker.invoke(shortcutservice.CommandToggleProxy)
	invoker.invoke(shortcutservice.CommandToggleProxy)
	if len(log) != 2 || log[0] != "show" || log[1] != "toggle" {
		t.Fatalf("log %v", log)
	}
	(*release)()
	invoker.invoke(shortcutservice.CommandToggleProxy)
	if len(log) != 4 {
		t.Fatalf("after debounce: %v", log)
	}
}
