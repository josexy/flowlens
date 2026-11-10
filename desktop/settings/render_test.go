package settings

import (
	"testing"

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
