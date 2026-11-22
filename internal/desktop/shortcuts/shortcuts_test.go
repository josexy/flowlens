package shortcuts

import (
	"testing"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestDefaultBindings(t *testing.T) {
	for _, test := range []struct{ id, want string }{
		{AppSave, "Ctrl+S"}, {AppOpenSettings, "Ctrl+,"}, {WorkbenchCapture, "Ctrl+1"}, {WorkbenchMemStats, "Ctrl+6"},
		{WorkspaceNewHTTPRequest, "Ctrl+N"}, {WorkspaceNewWebSocket, "Ctrl+Shift+N"}, {WorkspaceCloseTab, "Ctrl+W"},
		{WorkspaceNextTab, "Ctrl+Tab"}, {WorkspacePreviousTab, "Ctrl+Shift+Tab"}, {RequestEditorSend, "Ctrl+Enter"}, {CaptureFocusFilter, "Ctrl+F"},
	} {
		command, ok := CommandByID(test.id)
		if !ok {
			t.Fatalf("no command %q", test.id)
		}
		if got := Display(command.DefaultBinding, false); got != test.want {
			t.Errorf("%s: got %q, want %q", test.id, got, test.want)
		}
	}
	if len(Catalog) != 20 {
		t.Fatalf("catalog has %d commands", len(Catalog))
	}
	recording, ok := CommandByID(CaptureToggleRecording)
	if !ok || recording.DefaultBinding != nil || recording.GlobalCapable || recording.DefaultScope != settingservice.ShortcutScopeApplication {
		t.Fatalf("recording command must be independently configurable and unbound: %+v", recording)
	}
}

func TestPrimaryFollowsThePlatform(t *testing.T) {
	binding := bind("s", modifierPrimary)
	mac, _ := Resolve(binding, true)
	other, _ := Resolve(binding, false)
	if !mac.Super || mac.Control || !other.Control || other.Super {
		t.Fatalf("mac %+v, other %+v", mac, other)
	}
	if got := Display(binding, true); got != "⌘S" {
		t.Fatalf("mac label %q", got)
	}
	if got := Display(binding, false); got != "Ctrl+S" {
		t.Fatalf("label %q", got)
	}
	if got := KbdKeys(bind(" ", modifierPrimary, modifierShift), true); len(got) != 3 || got[0] != "shift" || got[1] != "meta" || got[2] != "space" {
		t.Fatalf("kbd %v", got)
	}
}

func TestNormalization(t *testing.T) {
	normalized := NormalizeBinding(&Binding{Key: "S", Modifiers: []settingservice.ShortcutModifier{modifierShift, modifierPrimary, modifierShift}})
	if normalized.Key != "s" || len(normalized.Modifiers) != 2 || normalized.Modifiers[0] != modifierPrimary {
		t.Fatalf("got %+v", normalized)
	}
	if NormalizeBinding(&Binding{Key: "  "}) != nil || NormalizeBinding(nil) != nil {
		t.Fatal("empty bindings")
	}
	if NormalizeKey("Esc") != "Escape" || NormalizeKey(" ") != " " || NormalizeKey("Spacebar") != " " {
		t.Fatal("aliases")
	}
}

func TestConflictsComparePressedKeys(t *testing.T) {
	if !BindingsConflict(bind("s", modifierPrimary), bind("S", modifierControl), false) {
		t.Fatal("Primary and Control are the same key on this platform")
	}
	if BindingsConflict(bind("s", modifierPrimary), bind("s", modifierControl), true) {
		t.Fatal("Command and Control differ on macOS")
	}
	if BindingsConflict(nil, nil, false) {
		t.Fatal("no binding never conflicts")
	}
}

func TestResolveCommandApplyOverrides(t *testing.T) {
	save, _ := CommandByID(AppSave)
	cleared := ResolveCommand(save, &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{AppSave: {Scope: settingservice.ShortcutScopeApplication}}})
	if cleared.Binding != nil {
		t.Fatal("cleared binding should disable the command")
	}
	globalAttempt := ResolveCommand(save, &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{AppSave: {Binding: bind("k", modifierPrimary), Scope: settingservice.ShortcutScopeGlobal}}})
	if globalAttempt.Scope != settingservice.ShortcutScopeApplication {
		t.Fatal("only global-capable commands can be global")
	}
	toggle, _ := CommandByID(CaptureToggleProxy)
	global := ResolveCommand(toggle, &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{CaptureToggleProxy: {Binding: bind("k", modifierPrimary), Scope: settingservice.ShortcutScopeGlobal}}})
	if global.Scope != settingservice.ShortcutScopeGlobal {
		t.Fatal("global override")
	}
	if got := ResolveCommand(save, nil); got.Binding != save.DefaultBinding {
		t.Fatal("default")
	}
}

func TestEveryDefaultKeyMapsToAUIKey(t *testing.T) {
	for _, command := range Catalog {
		if command.DefaultBinding == nil {
			continue
		}
		if _, ok := uiKey(NormalizeKey(command.DefaultBinding.Key)); !ok {
			t.Errorf("%s: no key for %q", command.ID, command.DefaultBinding.Key)
		}
	}
	if key, ok := uiKey("!"); !ok || key != ui.Key1 {
		t.Fatal("shifted punctuation")
	}
}

type page struct {
	config     *settingservice.ShortcutConfig
	dispatcher *Dispatcher
	modalOpen  bool
	when       bool
	field      string
	saved      int
	created    int
	sent       int
	focusField bool
}

func newPage() *page {
	p := &page{when: true}
	p.dispatcher = NewDispatcher(func() *settingservice.ShortcutConfig { return p.config })
	return p
}

func (p *page) view(c *ui.Context) {
	theme := nui.App(c, nui.DefaultApp())
	p.dispatcher.Begin()
	p.dispatcher.Handle(Handler{CommandID: AppSave, When: func() bool { return p.when }, Run: func() { p.saved++ }})
	p.dispatcher.Handle(Handler{CommandID: WorkspaceNewHTTPRequest, Run: func() { p.created++ }})
	p.dispatcher.Handle(Handler{CommandID: RequestEditorSend, Run: func() { p.sent++ }})
	ui.Column(c).Fill().Children(func() {
		input := ui.TextInput(c, &p.field).Label("field")
		if p.focusField {
			input.AutoFocus()
		}
		if p.modalOpen {
			p.dispatcher.NoteModal()
			nui.Modal(c.Key("modal"), theme, &p.modalOpen, "Dialog", func(nui.Theme) {})
		}
	})
	p.dispatcher.Install(c)
}

func TestShortcutRunsTheCurrentHandler(t *testing.T) {
	p := newPage()
	tester := ui.NewTester(p.view, 400, 300)
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	if p.saved != 1 {
		t.Fatalf("saved %d", p.saved)
	}
}

func TestWhenGuardBlocksTheCommand(t *testing.T) {
	p := newPage()
	p.when = false
	tester := ui.NewTester(p.view, 400, 300)
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	if p.saved != 0 {
		t.Fatalf("saved %d", p.saved)
	}
}

func TestModalBlocksCommands(t *testing.T) {
	p := newPage()
	p.modalOpen = true
	tester := ui.NewTester(p.view, 400, 300)
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	if p.saved != 0 {
		t.Fatalf("saved %d", p.saved)
	}
	if p.dispatcher.Invoke(AppSave) {
		t.Fatal("modal should block an invoked command")
	}
}

func TestEditableFocusBlocksCommandsThatDoNotAllowIt(t *testing.T) {
	p := newPage()
	p.focusField = true
	tester := ui.NewTester(p.view, 400, 300)
	tester.Frame()
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyN)
	tester.Frame()
	if p.created != 0 {
		t.Fatalf("created %d", p.created)
	}
}

func TestEditableFocusAllowsCommandsThatPermitIt(t *testing.T) {
	p := newPage()
	p.focusField = true
	tester := ui.NewTester(p.view, 400, 300)
	tester.Frame()
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyEnter)
	tester.Frame()
	if p.sent != 1 {
		t.Fatalf("sent %d", p.sent)
	}
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	if p.saved != 1 {
		t.Fatalf("saved %d", p.saved)
	}
}

func TestClearedBindingDoesNothing(t *testing.T) {
	p := newPage()
	p.config = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{AppSave: {Scope: settingservice.ShortcutScopeApplication}}}
	tester := ui.NewTester(p.view, 400, 300)
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	if p.saved != 0 {
		t.Fatalf("saved %d", p.saved)
	}
}

func TestReboundCommandUsesTheNewKey(t *testing.T) {
	p := newPage()
	p.config = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{AppSave: {Binding: bind("k", modifierPrimary), Scope: settingservice.ShortcutScopeApplication}}}
	tester := ui.NewTester(p.view, 400, 300)
	tester.Key(ui.Cmd, ui.KeyS)
	tester.Frame()
	tester.Key(ui.Cmd, ui.KeyK)
	tester.Frame()
	if p.saved != 1 {
		t.Fatalf("saved %d", p.saved)
	}
}

func TestGlobalScopeIsNotBoundToTheWindow(t *testing.T) {
	p := newPage()
	toggled := 0
	p.config = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{CaptureToggleProxy: {Binding: bind("j", modifierPrimary), Scope: settingservice.ShortcutScopeGlobal}}}
	view := func(c *ui.Context) {
		p.dispatcher.Begin()
		p.dispatcher.Handle(Handler{CommandID: CaptureToggleProxy, Run: func() { toggled++ }})
		ui.Box(c).Fill()
		p.dispatcher.Install(c)
	}
	tester := ui.NewTester(view, 400, 300)
	tester.Key(ui.Cmd, ui.KeyJ)
	tester.Frame()
	if toggled != 0 {
		t.Fatalf("toggled %d", toggled)
	}
	if !p.dispatcher.Invoke(CaptureToggleProxy) || toggled != 1 {
		t.Fatalf("invoke: toggled %d", toggled)
	}
}

func TestInnermostHandlerWins(t *testing.T) {
	dispatcher := NewDispatcher(func() *settingservice.ShortcutConfig { return nil })
	var got []string
	dispatcher.Handle(Handler{CommandID: AppSave, Run: func() { got = append(got, "outer") }})
	dispatcher.Handle(Handler{CommandID: AppSave, Run: func() { got = append(got, "inner") }})
	dispatcher.Handle(Handler{CommandID: AppSave, Priority: -1, Run: func() { got = append(got, "low") }})
	dispatcher.Invoke(AppSave)
	if len(got) != 1 || got[0] != "inner" {
		t.Fatalf("got %v", got)
	}
}
