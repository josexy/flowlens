package settings

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/pkg/logger"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
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

func TestRenderStorageSection(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			f := newFixture(t)
			i18n.SetLocaleFunc(func() string { return language })
			f.tester.SetLocale(language)
			f.settle()
			f.open(locales.SettingsTabStorage())
			f.clickAction(locales.SettingsClearCacheFiles())
			f.confirm.Accept()
			f.settle()
			if !f.has(locales.SettingsClearCacheSuccess()) {
				t.Fatalf("no cleanup notice in %v", f.tester.Texts())
			}
			rendertest.ShootThemes(t, f.tester, "settings-storage-"+language, func(dark bool) {
				setRenderTheme(t, f.data, dark)
				f.settle()
				f.tester.Advance(time.Second)
				f.settle()
			})
		})
	}
}

func TestRenderCertificateStates(t *testing.T) {
	installed := &settingservice.CACertificateTrustStatus{Supported: true, Present: true, Installed: true, SHA256Fingerprint: "ab:cd"}
	for _, state := range []struct {
		name  string
		setup func(f *fixture)
		texts []string
	}{
		{"not-installed", func(*fixture) {}, []string{locales.SettingsCaStatusReady(), locales.SettingsCaTrustNotInstalled()}},
		{"installed", func(f *fixture) { f.trust = installed; f.view.loadCA() }, []string{locales.SettingsCaTrustInstalled(), locales.SettingsCaTrustUninstallFirst()}},
		{"unsaved", func(f *fixture) { f.draft().ProxyConfig.CACertPath = "/certs/other.crt" }, []string{locales.SettingsCaTrustNotInstalled(), locales.SettingsCaActionsSaveFirst()}},
		{"unknown", func(f *fixture) { f.trustFail = errors.New("no store"); f.view.loadCA() }, []string{locales.SettingsCaTrustStatusUnavailable()}},
	} {
		t.Run(state.name, func(t *testing.T) {
			f := newFixture(t)
			f.open(locales.SettingsTabProxy())
			state.setup(f)
			f.settle()
			f.dismissToasts()
			for _, text := range state.texts {
				if !f.has(text) {
					t.Fatalf("no %q in %v", text, f.tester.Texts())
				}
			}
			rendertest.ShootThemes(t, f.tester, "settings-certificate-"+state.name, func(dark bool) {
				setRenderTheme(t, f.data, dark)
				f.settle()
				f.tester.Advance(time.Second)
				f.settle()
			})
			if state.name == "not-installed" {
				// Disabled buttons open no tooltip; the other states show their
				// reason in the panel instead.
				rendertest.RequireTooltip(t, f.tester, locales.SettingsCaTrustInstall(), locales.SettingsCaTrustInstall())
				rendertest.RequireTooltip(t, f.tester, locales.SettingsRegenerateCa(), locales.SettingsRegenerateCa())
			}
		})
	}
}

func TestSaveKeepsGeneralControlsStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFixture(t)
		setRenderTheme(t, f.data, dark)
		f.draft().CommonConfig.AppFontSize++
		f.settle()
		f.tester.Advance(time.Second)
		before := f.tester.Image()
		f.view.committing, f.data.Settings.Saving = true, true
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			rendertest.RequireSamePixels(t, before, f.tester.Image(), ui.Rect{X: 160, Y: 0, W: 1140, H: 1300}, "settings page")
			for _, control := range []struct {
				label string
				role  ui.Role
			}{
				{locales.SettingsThemeColorsPrimary(), ui.RolePopUpButton},
				{locales.SettingsThemeColorsNeutral(), ui.RolePopUpButton},
				{locales.SettingsAnimationsLabel(), ui.RoleSwitch},
				{locales.SettingsAppFontSizeLabel(), ui.RoleTextField},
				{locales.SettingsReset(), ui.RoleButton},
			} {
				info, ok := f.tester.AccessByRole(control.role, control.label)
				if !ok || !info.Disabled {
					t.Errorf("%q lost its pending-write guard: found=%v disabled=%v", control.label, ok, info.Disabled)
				}
			}
		}
		name := "settings-saving-light"
		if dark {
			name = "settings-saving-dark"
		}
		rendertest.Shoot(t, f.tester, name)
		f.view.committing, f.data.Settings.Saving = false, false
	}
}

func TestLogSwitchKeepsRequestedValueUntilSaveCompletes(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := newFixture(t)
		f.open(locales.SettingsTabLogs())
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })
		t.Cleanup(finish)
		f.view.Host.SetLog = func(enabled bool) (*logger.LogStatus, error) {
			<-release
			if fail {
				return nil, errors.New("write failed")
			}
			return &logger.LogStatus{Enabled: enabled, Level: logger.LogLevelInfo}, nil
		}
		label := locales.SettingsLogEnabledLabel()
		info, ok := f.tester.AccessByRole(ui.RoleSwitch, label)
		if !ok || !info.Checked {
			t.Fatalf("initial log switch: %+v", info)
		}
		f.tester.ClickAt(info.Bounds.X+10, info.Bounds.Y+info.Bounds.H/2)
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			info, ok = f.tester.AccessByRole(ui.RoleSwitch, label)
			if !ok || info.Checked || !info.Disabled || !f.view.logs.status.Enabled {
				t.Fatalf("pending switch reverted or changed the saved snapshot: %+v", info)
			}
		}
		finish()
		f.settle()
		info, _ = f.tester.AccessByRole(ui.RoleSwitch, label)
		if info.Disabled || info.Checked != fail {
			t.Fatalf("switch did not settle to the persisted value: %+v", info)
		}
		if fail && !f.has(locales.SettingsLogErrorToggle("write failed")) {
			t.Fatal("save error was hidden")
		}
	}
}

func TestSettingsToastIsTopCenterWithoutProgress(t *testing.T) {
	f := newFixture(t)
	f.notifier.Success("Settings saved here")
	f.tester.Frame()
	f.tester.Frame()
	rect, ok := f.tester.Find("Settings saved here")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	if center := rect.X + rect.W/2; center < 650-60 || center > 650+60 || rect.Y > 120 {
		t.Fatalf("toast at %+v, want top-center of 1300 wide", rect)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}

func TestShortcutRowActionsShowTooltips(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabShortcuts())
	label := locales.ShortcutsCommandsAppSave()
	f.view.shortcuts.search = "save"
	f.settle()
	rendertest.RequireTooltip(t, f.tester, locales.ShortcutsClearFor(label), locales.ShortcutsClearFor(label))
	f.click(locales.ShortcutsClearFor(label))
	rendertest.RequireTooltip(t, f.tester, locales.ShortcutsResetFor(label), locales.ShortcutsResetFor(label))
}
