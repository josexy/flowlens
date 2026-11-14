package editor

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/internal/menutest"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// openTabMenu right-clicks the tab titled title, and waits for its menu to
// pop in.
func openTabMenu(t *testing.T, f *fixture, title string) {
	t.Helper()
	menutest.Close(f.tester, f.settle)
	if err := menutest.Open(f.tester, f.settle, title); err != nil {
		t.Fatal(err)
	}
	f.tester.Advance(time.Second)
	f.settle()
}

// capsAfter returns, lowercased, the texts on show after label: an item's
// shortcut caps follow its label.
func capsAfter(t *testing.T, f *fixture, label string) string {
	t.Helper()
	texts := f.tester.Texts()
	at := slices.Index(texts, label)
	if at < 0 {
		t.Fatalf("no %q in %v", label, texts)
	}
	return strings.ToLower(strings.Join(texts[at+1:min(at+4, len(texts))], " "))
}

func TestTabMenuShowsShortcuts(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	openTabMenu(t, f, locales.WorkspaceTabBarUntitled())
	index := slices.Index(f.tabKeys(), tab.Key)
	byID := func() map[string]nui.MenuItem {
		items := map[string]nui.MenuItem{}
		for _, item := range f.editor.tabMenuItems(index, tab) {
			items[item.ID] = item
		}
		return items
	}
	items := byID()
	for id, command := range map[string]string{"save-api": shortcuts.AppSave, "close": shortcuts.WorkspaceCloseTab} {
		keys := f.editor.kbds(command)
		if len(keys) == 0 || items[id].Shortcut != strings.Join(keys, "+") {
			t.Fatalf("%s shows %q, want %v", id, items[id].Shortcut, keys)
		}
		after := capsAfter(t, f, items[id].Label)
		for _, key := range keys {
			if !strings.Contains(after, strings.ToLower(nui.FormatKey(key, runtime.GOOS))) {
				t.Fatalf("no %q cap after %q: %q", key, items[id].Label, after)
			}
		}
	}
	rendertest.ShootThemes(t, f.tester, "tab-menu", func(dark bool) {
		f.setDark(dark)
		openTabMenu(t, f, locales.WorkspaceTabBarUntitled())
	})
	menutest.Close(f.tester, f.settle)

	// Disabling Close Tab removes its hint.
	f.data.Settings.Current.Shortcuts = &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		shortcuts.WorkspaceCloseTab: {Binding: nil},
	}}
	if items = byID(); items["close"].Shortcut != "" || items["save-api"].Shortcut == "" {
		t.Fatalf("close %q, save %q", items["close"].Shortcut, items["save-api"].Shortcut)
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
