package editor

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

// openTabMenu right-clicks the tab titled title, with menus drawn in the
// window so that their shortcut hints are text.
func openTabMenu(t *testing.T, f *fixture, title string) {
	t.Helper()
	f.drawnMenus = true
	f.settle()
	if err := f.tester.RightClick(title); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if len(f.tester.Menu()) == 0 {
		t.Fatal("no menu")
	}
}

func TestTabMenuShowsShortcuts(t *testing.T) {
	if shortcuts.IsMacOS() {
		t.Skip("the drawn hints read Ctrl+ on Windows and Linux")
	}
	f := newFake(t)
	f.newHTTP()
	openTabMenu(t, f, locales.WorkspaceTabBarUntitled())
	for _, hint := range []string{"Ctrl+S", "Ctrl+W"} {
		if !f.tester.HasText(hint) {
			t.Fatalf("no %q in %v", hint, f.tester.Texts())
		}
	}
	rendertest.ShootThemes(t, f.tester, "tab-menu", func(dark bool) {
		f.setDark(dark)
		f.tester.CloseMenu()
		f.settle()
		openTabMenu(t, f, locales.WorkspaceTabBarUntitled())
	})
	f.tester.CloseMenu()
	f.settle()

	// Disabling Close Tab removes its hint.
	f.data.Settings.Current.Shortcuts = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		shortcuts.WorkspaceCloseTab: {Binding: nil},
	}}
	openTabMenu(t, f, locales.WorkspaceTabBarUntitled())
	if f.tester.HasText("Ctrl+W") || !f.tester.HasText("Ctrl+S") {
		t.Fatalf("hints %v", f.tester.Texts())
	}
}

// The new tab button opens its menu with one click, and its items create
// tabs.
func TestNewTabButtonOpensItsMenu(t *testing.T) {
	f := newFake(t)
	before := len(f.tabKeys())
	f.click(locales.WorkspaceTabBarNew())
	if !f.editor.newMenu.Open {
		t.Fatal("one click on New leaves its menu closed")
	}
	f.click(locales.WorkspaceTabBarNewHttp())
	if f.editor.newMenu.Open || len(f.tabKeys()) != before+1 {
		t.Fatalf("menu open %v, tabs %v", f.editor.newMenu.Open, f.tabKeys())
	}
	f.click(locales.WorkspaceTabBarNew())
	f.click(locales.WorkspaceTabBarNewWs())
	if len(f.tabKeys()) != before+2 {
		t.Fatalf("tabs %v", f.tabKeys())
	}
}

func TestNewTabMenuShowsBindings(t *testing.T) {
	f := newFake(t)
	f.editor.newMenu.Open = true
	f.settle()
	f.tester.Advance(time.Second)
	f.settle()
	texts := f.tester.Texts()
	label := slices.Index(texts, locales.WorkspaceTabBarNewHttp())
	if label < 0 {
		t.Fatalf("no New HTTP in %v", texts)
	}
	keys := f.editor.kbds(shortcuts.WorkspaceNewHTTPRequest)
	if len(keys) == 0 {
		t.Fatal("New HTTP has no default binding")
	}
	// The item's caps follow its label.
	after := strings.Join(texts[label:], " ")
	for _, key := range keys {
		if !strings.Contains(strings.ToLower(after), strings.ToLower(nui.FormatKey(key, runtime.GOOS))) {
			t.Fatalf("no %q cap after New HTTP in %v", key, texts[label:])
		}
	}
}
