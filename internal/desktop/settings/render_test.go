package settings

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

func TestSettingsSelectTextMatchesAcrossPages(t *testing.T) {
	f := newFixture(t)
	textHeight := func(label string) float32 {
		t.Helper()
		bounds, found := f.tester.Find(label)
		if !found || bounds.H <= 0 {
			t.Fatalf("no visible text for %q", label)
		}
		return bounds.H
	}
	for _, size := range []int{14, 20} {
		f.open(locales.SettingsTabGeneral())
		f.draft().CommonConfig.AppFontSize = size
		f.settle()
		want := textHeight(locales.SettingsFontDefault())
		f.clickControl(locales.SettingsWindowFrameModeLabel())
		wantOption := textHeight(locales.SettingsWindowFrameModeSystem())
		f.tester.Key(0, ui.KeyEscape)
		f.settle()
		for _, field := range []struct{ tab, value string }{
			{locales.SettingsTabProxy(), locales.SettingsUpstreamProxyModeSystem()},
			{locales.SettingsTabStorage(), locales.SettingsHistoryRetentionUnitDay()},
			{locales.SettingsTabLogs(), locales.SettingsLogLevelInfo()},
		} {
			f.open(field.tab)
			if got := textHeight(field.value); math.Abs(float64(got-want)) > 1 {
				t.Errorf("app font size %d, %q text height = %g, want %g", size, field.value, got, want)
			}
		}
		f.clickControl(locales.SettingsLogLevelLabel())
		label := locales.SettingsLogLevelDebug()
		if got := textHeight(label); math.Abs(float64(got-wantOption)) > 1 {
			t.Errorf("app font size %d, log option %q height = %g, want %g", size, label, got, wantOption)
		}
		f.tester.Key(0, ui.KeyEscape)
		f.settle()
	}
}

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
	for _, tab := range []string{locales.SettingsTabGeneral(), locales.SettingsTabProxy(), locales.SettingsTabStorage(), locales.SettingsTabShortcuts(), locales.SettingsTabLogs()} {
		f.open(tab)
		switch tab {
		case locales.SettingsTabProxy():
			f.click(locales.SettingsAddClientCert())
		case locales.SettingsTabLogs():
			f.clickControl(locales.SettingsLogLevelLabel())
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
