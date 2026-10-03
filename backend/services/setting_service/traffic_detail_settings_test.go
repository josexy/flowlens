package settingservice

import (
	"encoding/json"
	"testing"
)

func TestTrafficDetailConfigDefaultsAndSanitizes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *TrafficDetailConfig
		want   TrafficDetailLayout
	}{
		{"missing", nil, TrafficDetailLayoutVertical},
		{"empty", &TrafficDetailConfig{}, TrafficDetailLayoutVertical},
		{"invalid", &TrafficDetailConfig{Layout: "diagonal"}, TrafficDetailLayoutVertical},
		{"vertical", &TrafficDetailConfig{Layout: TrafficDetailLayoutVertical}, TrafficDetailLayoutVertical},
		{"horizontal", &TrafficDetailConfig{Layout: TrafficDetailLayoutHorizontal}, TrafficDetailLayoutHorizontal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configureTestSettingsPath(t)
			svc := newPersistentTestSettingService(t)
			if err := svc.Update(&Settings{TrafficDetailConfig: tc.config}); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Get()
			if err != nil {
				t.Fatal(err)
			}
			if got.TrafficDetailConfig == nil || got.TrafficDetailConfig.Layout != tc.want {
				t.Fatalf("traffic detail config = %#v, want %q", got.TrafficDetailConfig, tc.want)
			}
		})
	}
}

func TestSaveTrafficDetailConfigRoundTripAndIsolation(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	input := &TrafficDetailConfig{Layout: TrafficDetailLayoutHorizontal}
	if err := svc.SaveTrafficDetailConfig(input); err != nil {
		t.Fatal(err)
	}
	input.Layout = TrafficDetailLayoutVertical
	var count, version int
	var payload []byte
	if err := svc.repository.db.QueryRow(`SELECT COUNT(*) FROM app_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("narrow save wrote %d sections, want 1", count)
	}
	if err := svc.repository.db.QueryRow(`SELECT payload_version, payload_json FROM app_settings WHERE section = ?`, settingsSectionTrafficDetail).Scan(&version, &payload); err != nil {
		t.Fatal(err)
	}
	var stored TrafficDetailConfig
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	if version != settingsPayloadVersion || stored.Layout != TrafficDetailLayoutHorizontal {
		t.Fatalf("stored version/config = %d/%#v", version, stored)
	}
	for _, service := range []*SettingService{svc, newPersistentTestSettingService(t)} {
		got, err := service.Get()
		if err != nil {
			t.Fatal(err)
		}
		if got.TrafficDetailConfig.Layout != TrafficDetailLayoutHorizontal {
			t.Fatalf("layout = %q, want horizontal", got.TrafficDetailConfig.Layout)
		}
	}
}

func TestLoadLegacyDatabaseWithoutTrafficDetailUsesDefaults(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repository.db.Exec(`DELETE FROM app_settings WHERE section = ?`, settingsSectionTrafficDetail); err != nil {
		t.Fatal(err)
	}
	got, err := newPersistentTestSettingService(t).Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrafficDetailConfig.Layout != TrafficDetailLayoutVertical {
		t.Fatalf("legacy layout = %q, want vertical", got.TrafficDetailConfig.Layout)
	}
}

func TestSaveTrafficDetailConfigFailurePreservesInMemoryConfig(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.SaveTrafficDetailConfig(&TrafficDetailConfig{Layout: TrafficDetailLayoutHorizontal}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repository.db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveTrafficDetailConfig(&TrafficDetailConfig{Layout: TrafficDetailLayoutVertical}); err == nil {
		t.Fatal("save unexpectedly succeeded without app_settings")
	}
	got, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrafficDetailConfig.Layout != TrafficDetailLayoutHorizontal {
		t.Fatalf("failed save changed layout to %q", got.TrafficDetailConfig.Layout)
	}
	if err := svc.SaveTrafficDetailConfig(nil); err == nil {
		t.Fatal("nil config unexpectedly accepted")
	}
}

func TestOrdinarySettingsSavePreservesLatestTrafficDetailLayout(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	settings, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the independent snapshot held by the settings window.
	payload, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var stale Settings
	if err := json.Unmarshal(payload, &stale); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveTrafficDetailConfig(&TrafficDetailConfig{Layout: TrafficDetailLayoutHorizontal}); err != nil {
		t.Fatal(err)
	}
	stale.CommonConfig.Language = "en"
	if err := svc.UpdatePreservingShortcuts(&stale); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := newPersistentTestSettingService(t).Get()
	if err != nil {
		t.Fatal(err)
	}
	if got.TrafficDetailConfig.Layout != TrafficDetailLayoutHorizontal || got.CommonConfig.Language != "en" {
		t.Fatalf("ordinary save lost layout or language: %#v / %q", got.TrafficDetailConfig, got.CommonConfig.Language)
	}
}
