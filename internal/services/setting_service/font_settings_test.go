package settingservice

import (
	"testing"
	"time"
)

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
