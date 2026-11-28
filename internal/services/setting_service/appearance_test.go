package settingservice

import (
	"testing"
	"time"
)

func savedThemeColors(t *testing.T, svc *SettingService) themeColors {
	t.Helper()
	settings, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	return commonThemeColors(settings.CommonConfig)
}

func stageThemeColors(t *testing.T, svc *SettingService, primary, neutral string) {
	t.Helper()
	settings, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	copy := *settings
	common := *settings.CommonConfig
	common.ThemePrimaryColor, common.ThemeNeutralColor = primary, neutral
	copy.CommonConfig = &common
	if err := svc.UpdatePreservingShortcuts(&copy); err != nil {
		t.Fatal(err)
	}
}

func TestThemeColorDefaultsAndValidation(t *testing.T) {
	for _, colors := range []themeColors{{}, {"#fff", "red"}, {"Blue", "invalid"}, {"rose", "olive"}} {
		svc := &SettingService{}
		if err := svc.Update(&Settings{CommonConfig: &CommonConfig{
			ThemeMode: "dark", Language: "en", ThemePrimaryColor: colors.primary, ThemeNeutralColor: colors.neutral,
		}}); err != nil {
			t.Fatal(err)
		}
		settings, _ := svc.Get()
		want := themeColors{normalizeThemePrimaryColor(colors.primary), normalizeThemeNeutralColor(colors.neutral)}
		if commonThemeColors(settings.CommonConfig) != want || settings.CommonConfig.ThemePrimaryColor != want.primary || settings.CommonConfig.ThemeNeutralColor != want.neutral {
			t.Fatalf("colors were not normalized: %+v", settings.CommonConfig)
		}
		if settings.CommonConfig.ThemeMode != "dark" || settings.CommonConfig.Language != "en" {
			t.Fatal("palette normalization changed existing preferences")
		}
	}
}

func TestThemeColorsRoundTripAndLegacyDefaults(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if _, err := svc.repository.db.Exec(`INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES ('common', 1, '{"themeMode":"dark","language":"en"}', 0)`); err != nil {
		t.Fatal(err)
	}
	if colors := savedThemeColors(t, svc); colors != (themeColors{"blue", "slate"}) {
		t.Fatalf("legacy palette: %+v", colors)
	}
	stageThemeColors(t, svc, "violet", "mauve")
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := newPersistentTestSettingService(t)
	if colors := savedThemeColors(t, reloaded); colors != (themeColors{"violet", "mauve"}) {
		t.Fatalf("reloaded palette: %+v", colors)
	}
	settings, _ := reloaded.Get()
	if settings.CommonConfig.ThemeMode != "dark" || settings.CommonConfig.Language != "en" {
		t.Fatal("palette save changed theme mode or language")
	}
}

func TestThemeColorFailedSaveRollsBackStagedColors(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	stageThemeColors(t, svc, "purple", "zinc")
	if _, err := svc.repository.db.Exec(`CREATE TRIGGER fail_theme_save BEFORE INSERT ON app_settings
		WHEN NEW.section = 'common' BEGIN SELECT RAISE(ABORT, 'injected save failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err == nil {
		t.Fatal("injected save failure was not reported")
	}
	if colors := savedThemeColors(t, svc); colors != (themeColors{"blue", "slate"}) {
		t.Fatalf("failed colors remained staged for a later save: %+v", colors)
	}
	if _, err := svc.repository.db.Exec(`DROP TRIGGER fail_theme_save`); err != nil {
		t.Fatal(err)
	}
	_ = svc.SetLanguage("en")
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	if colors := savedThemeColors(t, newPersistentTestSettingService(t)); colors != (themeColors{"blue", "slate"}) {
		t.Fatalf("later unrelated save persisted failed colors: %+v", colors)
	}
}

func TestFontSizesRoundTripThroughOrdinarySettingsSave(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if _, err := svc.repository.db.Exec(`
		INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES (?, ?, ?, ?)
	`, settingsSectionCommon, settingsPayloadVersion, `{"themeMode":"dark","language":"en","appFontFamily":"Arial","codeFontFamily":"Consolas"}`, time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert legacy common settings: %v", err)
	}
	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get legacy settings: %v", err)
	}
	if settings.CommonConfig.AppFontSize != 16 || settings.CommonConfig.CodeFontSize != 13 {
		t.Fatalf("legacy font size defaults: %+v", settings.CommonConfig)
	}
	for _, sizes := range [][2]int{{12, 10}, {20, 18}, {24, 32}, {16, 13}} {
		settings.CommonConfig.AppFontSize = sizes[0]
		settings.CommonConfig.CodeFontSize = sizes[1]
		if err := svc.UpdatePreservingShortcuts(settings); err != nil {
			t.Fatalf("UpdatePreservingShortcuts: %v", err)
		}
		if err := svc.Save(); err != nil {
			t.Fatalf("Save: %v", err)
		}
		svc = newPersistentTestSettingService(t)
		settings, err = svc.Get()
		if err != nil {
			t.Fatalf("Get reloaded settings: %v", err)
		}
		common := settings.CommonConfig
		if common.AppFontSize != sizes[0] || common.CodeFontSize != sizes[1] {
			t.Fatalf("reloaded font sizes: %+v, want %v", common, sizes)
		}
		if common.AppFontFamily != "Arial" || common.CodeFontFamily != "Consolas" || common.ThemeMode != "dark" || common.Language != "en" {
			t.Fatalf("font size save changed existing preferences: %+v", common)
		}
	}
}

func TestInvalidFontSizesUseDefaults(t *testing.T) {
	for _, sizes := range [][2]int{{0, 0}, {-1, -1}, {11, 9}, {25, 33}} {
		svc := &SettingService{}
		if err := svc.Update(&Settings{CommonConfig: &CommonConfig{
			AppFontSize: sizes[0], CodeFontSize: sizes[1],
		}}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		settings, err := svc.Get()
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if settings.CommonConfig.AppFontSize != 16 || settings.CommonConfig.CodeFontSize != 13 {
			t.Fatalf("invalid font sizes %v were not replaced: %+v", sizes, settings.CommonConfig)
		}
	}
}

func TestAnimationsRoundTripAndLegacyDefault(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if _, err := svc.repository.db.Exec(`INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES ('common', 1, '{"themeMode":"dark","language":"en"}', 0)`); err != nil {
		t.Fatal(err)
	}
	settings, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	if settings.CommonConfig.DisableAnimations {
		t.Fatal("legacy settings must keep animations enabled")
	}
	for _, disabled := range []bool{true, false} {
		settings.CommonConfig.DisableAnimations = disabled
		if err := svc.UpdatePreservingShortcuts(settings); err != nil {
			t.Fatal(err)
		}
		if err := svc.Save(); err != nil {
			t.Fatal(err)
		}
		svc = newPersistentTestSettingService(t)
		settings, err = svc.Get()
		if err != nil {
			t.Fatal(err)
		}
		if common := settings.CommonConfig; common.DisableAnimations != disabled || common.ThemeMode != "dark" || common.Language != "en" {
			t.Fatalf("reloaded settings: %+v, want disabled=%v", common, disabled)
		}
	}
}
