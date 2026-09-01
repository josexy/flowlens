package settingservice

import (
	"encoding/json"
	"path/filepath"
	"testing"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
)

func TestPythonPluginConfigDefaultsAndSanitizes(t *testing.T) {
	service := &SettingService{}
	if err := service.Update(&Settings{}); err != nil {
		t.Fatalf("Update defaults: %v", err)
	}
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get defaults: %v", err)
	}
	if got := settings.PythonPluginConfig; got == nil || got.Enabled || got.InterpreterPath != "" || got.HookTimeoutMs != 5000 {
		t.Fatalf("default python plugin config = %+v", got)
	}

	if err := service.Update(&Settings{PythonPluginConfig: &PythonPluginConfig{
		Enabled: true, InterpreterPath: "   ", HookTimeoutMs: 99,
	}}); err != nil {
		t.Fatalf("Update low timeout: %v", err)
	}
	settings, _ = service.Get()
	if got := settings.PythonPluginConfig; !got.Enabled || got.InterpreterPath != "" || got.HookTimeoutMs != 100 {
		t.Fatalf("sanitized low config = %+v", got)
	}

	if err := service.Update(&Settings{PythonPluginConfig: &PythonPluginConfig{
		InterpreterPath: ` C:\Python311\python.exe `, HookTimeoutMs: 60001,
	}}); err != nil {
		t.Fatalf("Update high timeout: %v", err)
	}
	settings, _ = service.Get()
	if got := settings.PythonPluginConfig; got.InterpreterPath != `C:\Python311\python.exe` || got.HookTimeoutMs != 60000 {
		t.Fatalf("sanitized high config = %+v", got)
	}
}

func TestLoadLegacyDatabaseWithoutPythonPluginConfigUsesDefaults(t *testing.T) {
	service := newPythonSettingsTestService(t)
	if err := service.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	if _, err := service.repository.db.Exec(`DELETE FROM app_settings WHERE section = ?`, settingsSectionPythonPlugins); err != nil {
		t.Fatalf("delete python plugin section: %v", err)
	}

	reloaded := New(service.repository.db)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get reloaded settings: %v", err)
	}
	if got := settings.PythonPluginConfig; got == nil || got.Enabled || got.InterpreterPath != "" || got.HookTimeoutMs != 5000 {
		t.Fatalf("legacy python plugin config = %+v", got)
	}
}

func TestSavePythonPluginConfigIsNarrowAndPreservedByStaleOrdinaryUpdate(t *testing.T) {
	service := newPythonSettingsTestService(t)
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.CommonConfig.ThemeMode = "dark"
	settings.ProxyConfig.Port = 9123
	if err := service.Save(); err != nil {
		t.Fatalf("Save baseline: %v", err)
	}

	beforeCommon := readRawSettingSection(t, service, settingsSectionCommon)
	beforeProxy := readRawSettingSection(t, service, settingsSectionProxy)
	if err := service.SavePythonPluginConfig(&PythonPluginConfig{
		Enabled: true, InterpreterPath: `C:\Python311\python.exe`, HookTimeoutMs: 1200,
	}); err != nil {
		t.Fatalf("SavePythonPluginConfig: %v", err)
	}
	if got := readRawSettingSection(t, service, settingsSectionCommon); string(got) != string(beforeCommon) {
		t.Fatal("python-only save changed common settings")
	}
	if got := readRawSettingSection(t, service, settingsSectionProxy); string(got) != string(beforeProxy) {
		t.Fatal("python-only save changed proxy settings")
	}

	stale := &Settings{
		CommonConfig:       &CommonConfig{ThemeMode: "light", Language: "en"},
		PythonPluginConfig: &PythonPluginConfig{Enabled: false, HookTimeoutMs: 5000},
	}
	if err := service.UpdatePreservingShortcuts(stale); err != nil {
		t.Fatalf("UpdatePreservingShortcuts: %v", err)
	}
	if err := service.Save(); err != nil {
		t.Fatalf("Save stale ordinary settings: %v", err)
	}

	reloaded := New(service.repository.db)
	config, err := GetPythonPluginConfig(reloaded)
	if err != nil {
		t.Fatalf("GetPythonPluginConfig: %v", err)
	}
	if !config.Enabled || config.InterpreterPath != `C:\Python311\python.exe` || config.HookTimeoutMs != 1200 {
		t.Fatalf("stale ordinary update overwrote python config: %+v", config)
	}
}

func TestSavePythonPluginConfigFailurePreservesMemory(t *testing.T) {
	service := newPythonSettingsTestService(t)
	baseline := &PythonPluginConfig{InterpreterPath: `C:\Python311\python.exe`, HookTimeoutMs: 2500}
	if err := service.SavePythonPluginConfig(baseline); err != nil {
		t.Fatalf("save baseline: %v", err)
	}
	if _, err := service.repository.db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatalf("drop app_settings: %v", err)
	}
	if err := service.SavePythonPluginConfig(&PythonPluginConfig{Enabled: true, HookTimeoutMs: 9999}); err == nil {
		t.Fatal("save unexpectedly succeeded without app_settings")
	}
	config, err := GetPythonPluginConfig(service)
	if err != nil {
		t.Fatalf("GetPythonPluginConfig: %v", err)
	}
	if config.Enabled || config.InterpreterPath != baseline.InterpreterPath || config.HookTimeoutMs != baseline.HookTimeoutMs {
		t.Fatalf("failed save mutated memory: %+v", config)
	}
}

func newPythonSettingsTestService(t *testing.T) *SettingService {
	t.Helper()
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return service
}

func readRawSettingSection(t *testing.T, service *SettingService, section string) []byte {
	t.Helper()
	var payload []byte
	if err := service.repository.db.QueryRow(`SELECT payload_json FROM app_settings WHERE section = ?`, section).Scan(&payload); err != nil {
		t.Fatalf("read settings section %q: %v", section, err)
	}
	var compact any
	if err := json.Unmarshal(payload, &compact); err != nil {
		t.Fatalf("decode settings section %q: %v", section, err)
	}
	return append([]byte(nil), payload...)
}
