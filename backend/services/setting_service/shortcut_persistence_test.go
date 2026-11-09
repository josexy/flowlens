package settingservice

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
)

func TestSaveShortcutConfigPreservesOtherSettingsSections(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.CommonConfig.ThemeMode = "dark"
	settings.ProxyConfig.Port = 9123
	settings.WindowConfig.Width = 1234
	settings.CacheConfig.MaxWsMessages = 4321
	settings.HistoryRetentionConfig.Enabled = true
	settings.ProcessAttributionConfig.Enabled = false
	settings.TrafficTableConfig.HiddenColumns = []string{"process"}
	if err := service.Save(); err != nil {
		t.Fatalf("Save baseline: %v", err)
	}

	sectionNames := []string{
		settingsSectionCommon,
		settingsSectionProxy,
		settingsSectionWindow,
		settingsSectionCache,
		settingsSectionHistoryRetention,
		settingsSectionProcessAttribution,
		settingsSectionTrafficTable,
	}
	before := readSettingSectionPayloads(t, service.repository, sectionNames)
	input := &ShortcutConfig{Overrides: map[string]ShortcutOverride{
		"app.showMainWindow": {
			Scope:   ShortcutScopeGlobal,
			Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{ShortcutModifierPrimary, ShortcutModifierPrimary}, Key: "A"},
		},
	}}
	if err := service.SaveShortcutConfig(input); err != nil {
		t.Fatalf("SaveShortcutConfig: %v", err)
	}
	after := readSettingSectionPayloads(t, service.repository, sectionNames)
	for _, section := range sectionNames {
		if !bytes.Equal(before[section], after[section]) {
			t.Fatalf("section %q changed during shortcut-only save", section)
		}
	}

	// Mutating the caller-owned input cannot mutate the service snapshot.
	input.Overrides["app.showMainWindow"].Binding.Key = "z"
	stored, err := service.GetShortcutConfig()
	if err != nil {
		t.Fatalf("GetShortcutConfig: %v", err)
	}
	binding := stored.Overrides["app.showMainWindow"].Binding
	if binding.Key != "a" || !reflect.DeepEqual(binding.Modifiers, []ShortcutModifier{ShortcutModifierPrimary}) {
		t.Fatalf("shortcut config was not sanitized/cloned: %+v", binding)
	}
}

func TestSaveTrafficTableConfigPreservesOtherSettingsSections(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.CommonConfig.ThemeMode = "dark"
	settings.ProxyConfig.Port = 9123
	settings.WindowConfig.Width = 1234
	settings.CacheConfig.MaxWsMessages = 4321
	settings.HistoryRetentionConfig.Enabled = true
	settings.ProcessAttributionConfig.Enabled = false
	if err := service.Save(); err != nil {
		t.Fatalf("Save baseline: %v", err)
	}

	sectionNames := []string{
		settingsSectionCommon,
		settingsSectionProxy,
		settingsSectionWindow,
		settingsSectionCache,
		settingsSectionHistoryRetention,
		settingsSectionProcessAttribution,
		settingsSectionShortcuts,
	}
	before := readSettingSectionPayloads(t, service.repository, sectionNames)
	if err := service.SaveTrafficTableConfig(&TrafficTableConfig{
		HiddenColumns: []string{"process", "destination"},
	}); err != nil {
		t.Fatalf("SaveTrafficTableConfig: %v", err)
	}
	after := readSettingSectionPayloads(t, service.repository, sectionNames)
	for _, section := range sectionNames {
		if !bytes.Equal(before[section], after[section]) {
			t.Fatalf("section %q changed during traffic-table-only save", section)
		}
	}
}

func TestUpdatePreservingShortcutsRetainsBackendLatestConfig(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	latest := &ShortcutConfig{Overrides: map[string]ShortcutOverride{
		"capture.toggleProxy": {
			Scope:   ShortcutScopeGlobal,
			Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{ShortcutModifierControl}, Key: "p"},
		},
	}}
	if err := service.SaveShortcutConfig(latest); err != nil {
		t.Fatalf("SaveShortcutConfig: %v", err)
	}

	stale := &Settings{
		CommonConfig: &CommonConfig{ThemeMode: "dark", Language: "en"},
		Shortcuts: &ShortcutConfig{Overrides: map[string]ShortcutOverride{
			"app.showMainWindow": {Scope: ShortcutScopeGlobal, Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{ShortcutModifierAlt}, Key: "s"}},
		}},
	}
	if err := service.UpdatePreservingShortcuts(stale); err != nil {
		t.Fatalf("UpdatePreservingShortcuts: %v", err)
	}
	current, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.CommonConfig.ThemeMode != "dark" {
		t.Fatalf("ordinary section was not updated: %+v", current.CommonConfig)
	}
	if !reflect.DeepEqual(current.Shortcuts, NormalizeShortcutConfig(latest)) {
		t.Fatalf("latest shortcuts were overwritten: got=%+v want=%+v", current.Shortcuts, latest)
	}
}

func TestUpdatePreservingShortcutsPersistsProcessAttribution(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	stale, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stale.ProcessAttributionConfig.Enabled = false
	if err := service.UpdatePreservingShortcuts(stale); err != nil {
		t.Fatalf("UpdatePreservingShortcuts: %v", err)
	}
	if err := service.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := New(db)
	config, err := GetProcessAttributionConfig(reloaded)
	if err != nil {
		t.Fatalf("GetProcessAttributionConfig: %v", err)
	}
	if config.Enabled {
		t.Fatal("expected ordinary settings save to persist disabled process attribution")
	}
}

func TestUpdatePreservingShortcutsPersistsTrafficTableConfig(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	stale, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stale.TrafficTableConfig.HiddenColumns = []string{"process", "destination"}
	if err := service.UpdatePreservingShortcuts(stale); err != nil {
		t.Fatalf("UpdatePreservingShortcuts: %v", err)
	}
	if err := service.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := New(db)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get reloaded settings: %v", err)
	}
	want := []string{"process", "destination"}
	if !reflect.DeepEqual(settings.TrafficTableConfig.HiddenColumns, want) {
		t.Fatalf("hidden columns = %#v, want %#v", settings.TrafficTableConfig.HiddenColumns, want)
	}
}

func TestShortcutOnlySavePreservesProcessAttribution(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service := New(db)
	if err := service.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.ProcessAttributionConfig.Enabled = false
	if err := service.Save(); err != nil {
		t.Fatalf("Save baseline: %v", err)
	}

	if err := service.SaveShortcutConfig(&ShortcutConfig{Overrides: map[string]ShortcutOverride{
		"capture.toggleProxy": {
			Scope:   ShortcutScopeGlobal,
			Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{ShortcutModifierControl}, Key: "p"},
		},
	}}); err != nil {
		t.Fatalf("SaveShortcutConfig: %v", err)
	}

	reloaded := New(db)
	config, err := GetProcessAttributionConfig(reloaded)
	if err != nil {
		t.Fatalf("GetProcessAttributionConfig: %v", err)
	}
	if config.Enabled {
		t.Fatal("expected shortcut-only save to preserve disabled process attribution")
	}
}

func TestNormalizeShortcutConfigPreservesLiteralSpaceKey(t *testing.T) {
	config := &ShortcutConfig{Overrides: map[string]ShortcutOverride{
		"app.showMainWindow": {
			Scope:   ShortcutScopeGlobal,
			Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{ShortcutModifierControl}, Key: " "},
		},
	}}
	normalized := NormalizeShortcutConfig(config)
	if got := normalized.Overrides["app.showMainWindow"].Binding; got == nil || got.Key != " " {
		t.Fatalf("literal KeyboardEvent.key space was not preserved: %+v", got)
	}
}

func readSettingSectionPayloads(t *testing.T, repository *settingRepository, sections []string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(sections))
	for _, section := range sections {
		var payload []byte
		if err := repository.db.QueryRowContext(context.Background(), `SELECT payload_json FROM app_settings WHERE section = ?`, section).Scan(&payload); err != nil {
			t.Fatalf("read section %q: %v", section, err)
		}
		result[section] = append([]byte(nil), payload...)
	}
	return result
}
