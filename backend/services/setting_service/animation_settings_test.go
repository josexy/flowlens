package settingservice

import "testing"

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
