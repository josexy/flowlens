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
		if tab == locales.SettingsTabProxy() {
			f.click(locales.SettingsAddClientCert())
		}
		rendertest.ShootThemes(t, f.tester, "settings-"+tab, func(dark bool) {
			setRenderTheme(t, f.data, dark)
			f.settle()
			f.tester.Advance(time.Second)
			f.settle()
		})
	}
}

func TestRenderRootCAAddMenu(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabProxy())
	f.click(locales.SettingsAddRootCaAction())
	for _, label := range []string{locales.SettingsSelectFiles(), locales.SettingsAddPathManually()} {
		if !f.has(label) {
			t.Fatalf("no %q in %v", label, f.tester.Texts())
		}
	}
	rendertest.ShootThemes(t, f.tester, "settings-root-ca-add", func(dark bool) {
		setRenderTheme(t, f.data, dark)
		f.settle()
		f.tester.Advance(time.Second)
		f.settle()
	})
	f.click(locales.SettingsAddPathManually())
	if paths := f.draft().ProxyConfig.RootCAPaths; len(paths) != 1 || paths[0] != "" || !f.has(locales.SettingsRootCaPathPlaceholder()) {
		t.Fatalf("manual entry did not show an empty Root CA path: %q", paths)
	}
	f.click(locales.SettingsRootCaPathPlaceholder() + " 1")
	f.tester.Type("/ca/manual.pem")
	f.settle()
	if path := f.draft().ProxyConfig.RootCAPaths[0]; path != "/ca/manual.pem" {
		t.Fatalf("manual entry has path %q", path)
	}
}
