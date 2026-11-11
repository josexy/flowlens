package settings

import (
	"testing"
	"time"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

func setRenderTheme(t *testing.T, data *store.Store, dark bool) {
	t.Helper()
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := data.Settings.SetThemeMode(mode); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSettings(t *testing.T) {
	f := newFixture(t)
	for _, tab := range []string{locales.SettingsTabGeneral(), locales.SettingsTabProxy(), locales.SettingsTabShortcuts()} {
		f.open(tab)
		rendertest.ShootThemes(t, f.tester, "settings-"+tab, func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
	}
}

func TestRenderRootCAAddMenu(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	// The section's own name is "Add Root CA" too: open the menu as its
	// button does.
	f.view.proxy.rootMenu.Open = true
	f.settle()
	for _, label := range []string{locales.SettingsSelectFiles(), locales.SettingsAddPathManually()} {
		if !f.has(label) {
			t.Fatalf("no %q in %v", label, f.tester.Texts())
		}
	}
	rendertest.ShootThemes(t, f.tester, "settings-root-ca-add", func(dark bool) {
		setRenderTheme(t, f.data, dark)
		f.view.proxy.rootMenu.Open = true
		f.settle()
		f.tester.Advance(time.Second)
		f.settle()
	})
}
