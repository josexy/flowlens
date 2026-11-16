package settingservice

import "testing"

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
