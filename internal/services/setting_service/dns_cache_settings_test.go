package settingservice

import "testing"

func TestDNSCacheSettingsLegacyDefaultsAndPersistence(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repository.db.Exec(`INSERT INTO app_settings(section, payload_version, payload_json, updated_at) VALUES ('proxy', 1, '{"mode":"http","port":8080}', 0)`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.GetProxyConfig()
	if err != nil || cfg.DNSCacheEnabled || cfg.DNSCacheTTLSeconds != 60 {
		t.Fatalf("legacy settings: %+v %v", cfg, err)
	}
	settings, _ := svc.Get()
	settings.ProxyConfig.DNSCacheEnabled, settings.ProxyConfig.DNSCacheTTLSeconds = true, 120
	if err := svc.UpdatePreservingShortcuts(settings); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := New(svc.repository.db)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	cfg, err = reloaded.GetProxyConfig()
	if err != nil || !cfg.DNSCacheEnabled || cfg.DNSCacheTTLSeconds != 120 {
		t.Fatalf("persisted settings: %+v %v", cfg, err)
	}
}

func TestDNSCacheSettingsRejectInvalidDurations(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int{-1, 3601} {
		for _, update := range []func(*Settings) error{svc.Update, svc.UpdatePreservingShortcuts} {
			if err := update(&Settings{ProxyConfig: &ProxyConfig{DNSCacheEnabled: true, DNSCacheTTLSeconds: value}}); err == nil {
				t.Fatalf("accepted duration %d", value)
			}
			cfg, _ := svc.GetProxyConfig()
			if cfg.DNSCacheEnabled || cfg.DNSCacheTTLSeconds != 60 {
				t.Fatal("invalid configuration changed settings")
			}
		}
	}
}

func TestDNSCacheSettingsFailedSaveRestoresPersistedValues(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	settings, _ := svc.Get()
	settings.ProxyConfig.DNSCacheEnabled, settings.ProxyConfig.DNSCacheTTLSeconds = true, 90
	if err := svc.UpdatePreservingShortcuts(settings); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repository.db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatal(err)
	}
	settings, _ = svc.Get()
	settings.ProxyConfig.DNSCacheEnabled, settings.ProxyConfig.DNSCacheTTLSeconds = false, 120
	if err := svc.UpdatePreservingShortcuts(settings); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err == nil {
		t.Fatal("save succeeded without settings table")
	}
	cfg, _ := svc.GetProxyConfig()
	if !cfg.DNSCacheEnabled || cfg.DNSCacheTTLSeconds != 90 {
		t.Fatalf("failed save published candidate: %+v", cfg)
	}
}
