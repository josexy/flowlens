package settingservice

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
)

func TestProcessAttributionEnabledFastPathTracksUpdates(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{
		ProcessAttributionConfig: &ProcessAttributionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("Update disabled: %v", err)
	}
	enabled, err := ProcessAttributionEnabled(svc)
	if err != nil {
		t.Fatalf("ProcessAttributionEnabled disabled: %v", err)
	}
	if enabled {
		t.Fatal("process attribution fast path remained enabled after disabled update")
	}

	if err := svc.Update(&Settings{
		ProcessAttributionConfig: &ProcessAttributionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("Update enabled: %v", err)
	}
	enabled, err = ProcessAttributionEnabled(svc)
	if err != nil {
		t.Fatalf("ProcessAttributionEnabled enabled: %v", err)
	}
	if !enabled {
		t.Fatal("process attribution fast path remained disabled after enabled update")
	}
}

func TestProcessAttributionEnabledFastPathDoesNotWaitForSettingsLock(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{
		ProcessAttributionConfig: &ProcessAttributionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	type result struct {
		enabled bool
		err     error
	}
	resultCh := make(chan result, 1)
	svc.mu.Lock()
	go func() {
		enabled, err := ProcessAttributionEnabled(svc)
		resultCh <- result{enabled: enabled, err: err}
	}()

	select {
	case got := <-resultCh:
		svc.mu.Unlock()
		if got.err != nil {
			t.Fatalf("ProcessAttributionEnabled: %v", got.err)
		}
		if !got.enabled {
			t.Fatal("process attribution fast path returned disabled")
		}
	case <-time.After(250 * time.Millisecond):
		svc.mu.Unlock()
		t.Fatal("process attribution fast path waited for settings mutex")
	}
}

func TestUpdateFillsCommonConfigDefaults(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig == nil {
		t.Fatal("CommonConfig was not initialized")
	}
	if settings.ProxyConfig == nil {
		t.Fatal("ProxyConfig was not initialized")
	}
	if settings.CacheConfig == nil {
		t.Fatal("CacheConfig was not initialized")
	}
	if settings.WindowConfig == nil {
		t.Fatal("WindowConfig was not initialized")
	}
	if settings.HistoryRetentionConfig == nil {
		t.Fatal("HistoryRetentionConfig was not initialized")
	}
	if settings.Shortcuts == nil || settings.Shortcuts.Overrides == nil {
		t.Fatal("Shortcuts was not initialized with an overrides map")
	}
	if settings.WindowConfig.Width <= 0 || settings.WindowConfig.Height <= 0 {
		t.Fatalf("WindowConfig size was not initialized: %+v", settings.WindowConfig)
	}
	if settings.WindowConfig.FrameMode != WindowFrameModeCustom {
		t.Fatalf("expected default window frame mode %q, got %q", WindowFrameModeCustom, settings.WindowConfig.FrameMode)
	}
	if settings.WindowConfig.MainWindowCloseBehavior != MainWindowCloseBehaviorHideToTray {
		t.Fatalf(
			"expected default main window close behavior %q, got %q",
			MainWindowCloseBehaviorHideToTray,
			settings.WindowConfig.MainWindowCloseBehavior,
		)
	}
	if settings.CommonConfig.ThemeMode != defaultThemeMode {
		t.Fatalf("expected default theme mode %q, got %q", defaultThemeMode, settings.CommonConfig.ThemeMode)
	}
	if settings.CommonConfig.Language != defaultLanguage {
		t.Fatalf("expected default language %q, got %q", defaultLanguage, settings.CommonConfig.Language)
	}
	if settings.CommonConfig.LogLevel != string(logger.DefaultLogLevel()) {
		t.Fatalf("expected default log level %q, got %q", logger.DefaultLogLevel(), settings.CommonConfig.LogLevel)
	}
	if settings.CommonConfig.LogDisabled {
		t.Fatal("expected logging to be enabled by default")
	}
	if settings.HistoryRetentionConfig.Enabled {
		t.Fatal("expected history retention cleanup to be disabled by default")
	}
	if settings.HistoryRetentionConfig.Value != defaultHistoryRetentionValue || settings.HistoryRetentionConfig.Unit != HistoryRetentionUnitDay {
		t.Fatalf("unexpected history retention defaults: %+v", settings.HistoryRetentionConfig)
	}
}

func TestUpdateSanitizesHistoryRetentionConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *HistoryRetentionConfig
		want HistoryRetentionConfig
	}{
		{
			name: "valid config is preserved",
			cfg: &HistoryRetentionConfig{
				Enabled: true,
				Value:   3,
				Unit:    HistoryRetentionUnitMonth,
			},
			want: HistoryRetentionConfig{
				Enabled: true,
				Value:   3,
				Unit:    HistoryRetentionUnitMonth,
			},
		},
		{
			name: "minimum value is preserved",
			cfg: &HistoryRetentionConfig{
				Enabled: true,
				Value:   minHistoryRetentionValue,
				Unit:    HistoryRetentionUnitHour,
			},
			want: HistoryRetentionConfig{
				Enabled: true,
				Value:   minHistoryRetentionValue,
				Unit:    HistoryRetentionUnitHour,
			},
		},
		{
			name: "maximum value is preserved",
			cfg: &HistoryRetentionConfig{
				Enabled: true,
				Value:   maxHistoryRetentionValue,
				Unit:    HistoryRetentionUnitYear,
			},
			want: HistoryRetentionConfig{
				Enabled: true,
				Value:   maxHistoryRetentionValue,
				Unit:    HistoryRetentionUnitYear,
			},
		},
		{
			name: "zero value uses defaults",
			cfg:  &HistoryRetentionConfig{},
			want: HistoryRetentionConfig{
				Value: defaultHistoryRetentionValue,
				Unit:  HistoryRetentionUnitDay,
			},
		},
		{
			name: "out of range value and invalid unit use defaults",
			cfg: &HistoryRetentionConfig{
				Enabled: true,
				Value:   maxHistoryRetentionValue + 1,
				Unit:    HistoryRetentionUnit("quarter"),
			},
			want: HistoryRetentionConfig{
				Value: defaultHistoryRetentionValue,
				Unit:  HistoryRetentionUnitDay,
			},
		},
		{
			name: "negative value disables cleanup",
			cfg: &HistoryRetentionConfig{
				Enabled: true,
				Value:   -1,
				Unit:    HistoryRetentionUnitWeek,
			},
			want: HistoryRetentionConfig{
				Value: defaultHistoryRetentionValue,
				Unit:  HistoryRetentionUnitDay,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &SettingService{}
			if err := svc.Update(&Settings{HistoryRetentionConfig: tt.cfg}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			settings, err := svc.Get()
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got := *settings.HistoryRetentionConfig; got != tt.want {
				t.Fatalf("HistoryRetentionConfig = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUpdateSanitizesProxyAdvancedDefaults(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{
		ProxyConfig: &ProxyConfig{
			Mode:         ProxyMode("bad"),
			Host:         "",
			Port:         70000,
			CACertPath:   "",
			CAKeyPath:    "",
			UpstreamMode: UpstreamProxyMode("bad"),
			IncludeHosts: []string{" api.example.com ", "", "*.example.com"},
			ExcludeHosts: []string{" ", "cdn.example.com"},
			RootCAPaths:  []string{"", " certs/root.crt "},
			ClientCerts:  []ClientCertConfig{{}, {Enabled: true, Hostname: " api.example.com ", CertPath: " client.crt ", KeyPath: " client.key "}},
		},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	cfg := settings.ProxyConfig
	if cfg.Mode != ProxyModeHTTP || cfg.Host != defaultProxyHost || cfg.Port != defaultProxyPort {
		t.Fatalf("proxy defaults not sanitized: %+v", cfg)
	}
	if cfg.CACertPath != defaultProxyCACertPath || cfg.CAKeyPath != defaultProxyCAKeyPath {
		t.Fatalf("ca path defaults not sanitized: cert=%q key=%q", cfg.CACertPath, cfg.CAKeyPath)
	}
	if cfg.UpstreamMode != UpstreamProxyModeSystem {
		t.Fatalf("expected invalid upstream mode to fall back to system, got %q", cfg.UpstreamMode)
	}
	if strings.Join(cfg.IncludeHosts, ",") != "api.example.com,*.example.com" {
		t.Fatalf("include hosts not sanitized: %#v", cfg.IncludeHosts)
	}
	if strings.Join(cfg.ExcludeHosts, ",") != "cdn.example.com" {
		t.Fatalf("exclude hosts not sanitized: %#v", cfg.ExcludeHosts)
	}
	if strings.Join(cfg.RootCAPaths, ",") != "certs/root.crt" {
		t.Fatalf("root ca paths not sanitized: %#v", cfg.RootCAPaths)
	}
	if len(cfg.ClientCerts) != 1 || cfg.ClientCerts[0].Hostname != "api.example.com" {
		t.Fatalf("client certs not sanitized: %#v", cfg.ClientCerts)
	}
}

func TestUpdateMigratesLegacyUpstreamProxyMode(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *ProxyConfig
		wantMode     UpstreamProxyMode
		wantUpstream string
		wantDisable  bool
	}{
		{
			name: "custom proxy",
			cfg: &ProxyConfig{
				UpstreamProxy: " http://proxy.example:8080 ",
			},
			wantMode:     UpstreamProxyModeCustom,
			wantUpstream: " http://proxy.example:8080 ",
		},
		{
			name: "legacy disabled proxy means no upstream proxy",
			cfg: &ProxyConfig{
				DisableProxy: true,
			},
			wantMode:    UpstreamProxyModeNone,
			wantDisable: true,
		},
		{
			name:     "empty legacy config defaults to system proxy",
			cfg:      &ProxyConfig{},
			wantMode: UpstreamProxyModeSystem,
		},
		{
			name: "explicit none is preserved",
			cfg: &ProxyConfig{
				UpstreamMode:  UpstreamProxyModeNone,
				UpstreamProxy: "http://proxy.example:8080",
			},
			wantMode:     UpstreamProxyModeNone,
			wantUpstream: "http://proxy.example:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &SettingService{}
			if err := svc.Update(&Settings{ProxyConfig: tt.cfg}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			settings, err := svc.Get()
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			got := settings.ProxyConfig
			if got.UpstreamMode != tt.wantMode {
				t.Fatalf("UpstreamMode = %q, want %q", got.UpstreamMode, tt.wantMode)
			}
			if got.UpstreamProxy != tt.wantUpstream {
				t.Fatalf("UpstreamProxy = %q, want %q", got.UpstreamProxy, tt.wantUpstream)
			}
			if got.DisableProxy != tt.wantDisable {
				t.Fatalf("DisableProxy = %t, want %t", got.DisableProxy, tt.wantDisable)
			}
		})
	}
}

func TestUpdateSanitizesCommonPreferenceDefaults(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{
		CommonConfig: &CommonConfig{
			LogLevel:  "verbose",
			ThemeMode: "sepia",
			Language:  "fr",
		},
		WindowConfig: &WindowConfig{
			Width:                   100,
			Height:                  100,
			FrameMode:               WindowFrameMode("floating"),
			MainWindowCloseBehavior: MainWindowCloseBehavior("minimize"),
		},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != defaultThemeMode {
		t.Fatalf("expected invalid theme to fall back to %q, got %q", defaultThemeMode, settings.CommonConfig.ThemeMode)
	}
	if settings.CommonConfig.Language != defaultLanguage {
		t.Fatalf("expected invalid language to fall back to %q, got %q", defaultLanguage, settings.CommonConfig.Language)
	}
	if settings.CommonConfig.LogLevel != string(logger.DefaultLogLevel()) {
		t.Fatalf("expected invalid log level to fall back to %q, got %q", logger.DefaultLogLevel(), settings.CommonConfig.LogLevel)
	}
	if settings.WindowConfig.Width <= 100 || settings.WindowConfig.Height <= 100 {
		t.Fatalf("expected undersized window config to be clamped, got %+v", settings.WindowConfig)
	}
	if settings.WindowConfig.FrameMode != WindowFrameModeCustom {
		t.Fatalf("expected invalid window frame mode to fall back to %q, got %q", WindowFrameModeCustom, settings.WindowConfig.FrameMode)
	}
	if settings.WindowConfig.MainWindowCloseBehavior != MainWindowCloseBehaviorHideToTray {
		t.Fatalf(
			"expected invalid main window close behavior to fall back to %q, got %q",
			MainWindowCloseBehaviorHideToTray,
			settings.WindowConfig.MainWindowCloseBehavior,
		)
	}
}

func TestMainWindowCloseBehaviorPersists(t *testing.T) {
	configureTestSettingsPath(t)
	service := newPersistentTestSettingService(t)
	settings, err := service.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.WindowConfig.MainWindowCloseBehavior = MainWindowCloseBehaviorQuit
	if err := service.Update(settings); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := service.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	reloadedSettings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}
	if reloadedSettings.WindowConfig.MainWindowCloseBehavior != MainWindowCloseBehaviorQuit {
		t.Fatalf(
			"reloaded main window close behavior = %q, want %q",
			reloadedSettings.WindowConfig.MainWindowCloseBehavior,
			MainWindowCloseBehaviorQuit,
		)
	}
}

func TestSetPreferencesUpdateLoadedSettings(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := svc.SetThemeMode("dark"); err != nil {
		t.Fatalf("SetThemeMode: %v", err)
	}
	if err := svc.SetLanguage("en"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != "dark" {
		t.Fatalf("expected theme mode dark, got %q", settings.CommonConfig.ThemeMode)
	}
	if settings.CommonConfig.Language != "en" {
		t.Fatalf("expected language en, got %q", settings.CommonConfig.Language)
	}
}

func TestSetLogPreferencesUpdateLoadedSettings(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := SetLogEnabled(svc, false); err != nil {
		t.Fatalf("SetLogEnabled: %v", err)
	}
	if err := SetLogLevel(svc, "debug"); err != nil {
		t.Fatalf("SetLogLevel: %v", err)
	}

	logCfg, err := GetLogConfig(svc)
	if err != nil {
		t.Fatalf("GetLogConfig: %v", err)
	}
	if logCfg.Enabled {
		t.Fatal("expected log config to be disabled")
	}
	if logCfg.Level != "debug" {
		t.Fatalf("expected log level debug, got %q", logCfg.Level)
	}
}

func TestActiveWindowFrameModeIsRuntimeOnly(t *testing.T) {
	svc := &SettingService{}
	if got := svc.GetActiveWindowFrameMode(); got != WindowFrameModeCustom {
		t.Fatalf("expected default active window frame mode %q, got %q", WindowFrameModeCustom, got)
	}

	SetActiveWindowFrameMode(svc, WindowFrameModeSystem)
	if got := svc.GetActiveWindowFrameMode(); got != WindowFrameModeSystem {
		t.Fatalf("expected active window frame mode %q, got %q", WindowFrameModeSystem, got)
	}

	SetActiveWindowFrameMode(svc, WindowFrameMode("floating"))
	if got := svc.GetActiveWindowFrameMode(); got != WindowFrameModeCustom {
		t.Fatalf("expected invalid active window frame mode to fall back to %q, got %q", WindowFrameModeCustom, got)
	}
}

func TestBuildLocalIPv4AddressOptionsFiltersAndSorts(t *testing.T) {
	options := buildLocalIPv4AddressOptions([]localInterfaceAddress{
		{InterfaceName: "wifi", IP: net.ParseIP("192.168.1.20")},
		{InterfaceName: "loopback", IP: net.ParseIP("127.0.0.1")},
		{InterfaceName: "ethernet", IP: net.ParseIP("10.0.0.2")},
		{InterfaceName: "ethernet", IP: net.ParseIP("10.0.0.2")},
		{InterfaceName: "ethernet", IP: net.ParseIP("::1")},
		{InterfaceName: "ethernet", IP: net.ParseIP("0.0.0.0")},
	})

	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.Value)
	}
	if strings.Join(values, ",") != "0.0.0.0,127.0.0.1,10.0.0.2,192.168.1.20" {
		t.Fatalf("unexpected local IPv4 options: %#v", options)
	}
	if options[0].Label != "ALL" {
		t.Fatalf("expected ALL to be the first option, got %#v", options[0])
	}
	if options[2].Label != "10.0.0.2 (ethernet)" || options[2].InterfaceName != "ethernet" {
		t.Fatalf("expected interface label metadata, got %#v", options[2])
	}
}

func TestLoadEmptyDatabaseUsesDefaultsUntilSave(t *testing.T) {
	settingPath := configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)

	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != defaultThemeMode {
		t.Fatalf("expected default theme mode %q, got %q", defaultThemeMode, settings.CommonConfig.ThemeMode)
	}
	if settings.CommonConfig.LogLevel != string(logger.DefaultLogLevel()) {
		t.Fatalf("expected default log level %q, got %q", logger.DefaultLogLevel(), settings.CommonConfig.LogLevel)
	}
	var sectionCount int
	if err := svc.repository.db.QueryRow(`SELECT COUNT(*) FROM app_settings`).Scan(&sectionCount); err != nil {
		t.Fatalf("count settings sections: %v", err)
	}
	if sectionCount != 0 {
		t.Fatalf("settings section count after Load = %d, want 0", sectionCount)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := svc.repository.db.QueryRow(`SELECT COUNT(*) FROM app_settings`).Scan(&sectionCount); err != nil {
		t.Fatalf("count saved settings sections: %v", err)
	}
	if sectionCount != 9 {
		t.Fatalf("settings section count after Save = %d, want 9", sectionCount)
	}
	if _, err := os.Stat(settingPath); !os.IsNotExist(err) {
		t.Fatalf("SQLite Save unexpectedly created legacy settings file: %v", err)
	}
}

func TestLoadDefaultsProcessAttributionEnabled(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.ProcessAttributionConfig == nil {
		t.Fatal("ProcessAttributionConfig was not initialized")
	}
	if !settings.ProcessAttributionConfig.Enabled {
		t.Fatal("expected process attribution to be enabled by default")
	}

	var sectionCount int
	if err := svc.repository.db.QueryRow(
		`SELECT COUNT(*) FROM app_settings WHERE section = ?`,
		settingsSectionProcessAttribution,
	).Scan(&sectionCount); err != nil {
		t.Fatalf("count process attribution sections: %v", err)
	}
	if sectionCount != 0 {
		t.Fatalf("process attribution section count after default load = %d, want 0", sectionCount)
	}
}

func TestProcessAttributionConfigRoundTripsThroughRepository(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.ProcessAttributionConfig.Enabled = false
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	enabled, err := ProcessAttributionEnabled(svc)
	if err != nil {
		t.Fatalf("ProcessAttributionEnabled after Save: %v", err)
	}
	if enabled {
		t.Fatal("process attribution fast path remained enabled after Save")
	}

	reloaded := newPersistentTestSettingService(t)
	config, err := GetProcessAttributionConfig(reloaded)
	if err != nil {
		t.Fatalf("GetProcessAttributionConfig: %v", err)
	}
	if config.Enabled {
		t.Fatal("expected disabled process attribution to round-trip")
	}
	enabled, err = ProcessAttributionEnabled(reloaded)
	if err != nil {
		t.Fatalf("ProcessAttributionEnabled after reload: %v", err)
	}
	if enabled {
		t.Fatal("process attribution fast path did not load the persisted disabled value")
	}
}

func TestTrafficTableConfigDefaultsAndSanitizes(t *testing.T) {
	svc := &SettingService{}
	if err := svc.Update(&Settings{}); err != nil {
		t.Fatalf("Update defaults: %v", err)
	}

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get defaults: %v", err)
	}
	if settings.TrafficTableConfig == nil {
		t.Fatal("TrafficTableConfig was not initialized")
	}
	if len(settings.TrafficTableConfig.HiddenColumns) != 0 {
		t.Fatalf("default hidden columns = %#v, want empty", settings.TrafficTableConfig.HiddenColumns)
	}

	input := []string{" process ", "process", "unknown", "host", ""}
	if err := svc.Update(&Settings{
		TrafficTableConfig: &TrafficTableConfig{HiddenColumns: input},
	}); err != nil {
		t.Fatalf("Update malformed config: %v", err)
	}
	settings, err = svc.Get()
	if err != nil {
		t.Fatalf("Get malformed config: %v", err)
	}
	if want := []string{"host", "process"}; !reflect.DeepEqual(settings.TrafficTableConfig.HiddenColumns, want) {
		t.Fatalf("hidden columns = %#v, want %#v", settings.TrafficTableConfig.HiddenColumns, want)
	}

	allColumns := []string{
		"id", "method", "host", "path", "process",
		"statusCode", "type", "destination", "protocol", "duration", "size",
	}
	if err := svc.Update(&Settings{
		TrafficTableConfig: &TrafficTableConfig{HiddenColumns: allColumns},
	}); err != nil {
		t.Fatalf("Update all-hidden config: %v", err)
	}
	settings, err = svc.Get()
	if err != nil {
		t.Fatalf("Get all-hidden config: %v", err)
	}
	if slices.Contains(settings.TrafficTableConfig.HiddenColumns, "id") {
		t.Fatalf("all-hidden config kept id hidden: %#v", settings.TrafficTableConfig.HiddenColumns)
	}
	if len(settings.TrafficTableConfig.HiddenColumns) != len(allColumns)-1 {
		t.Fatalf(
			"all-hidden fallback = %#v, want %d hidden columns",
			settings.TrafficTableConfig.HiddenColumns,
			len(allColumns)-1,
		)
	}
}

func TestTrafficTableConfigRoundTripsThroughRepository(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	settings.TrafficTableConfig.HiddenColumns = []string{"process", "destination"}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	got, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get reloaded settings: %v", err)
	}
	want := []string{"process", "destination"}
	if !reflect.DeepEqual(got.TrafficTableConfig.HiddenColumns, want) {
		t.Fatalf("hidden columns = %#v, want %#v", got.TrafficTableConfig.HiddenColumns, want)
	}
}

func TestLoadLegacyDatabaseWithoutTrafficTableUsesDefaults(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	if _, err := svc.repository.db.Exec(
		`DELETE FROM app_settings WHERE section = ?`,
		settingsSectionTrafficTable,
	); err != nil {
		t.Fatalf("delete traffic table section: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get reloaded settings: %v", err)
	}
	if settings.TrafficTableConfig == nil || len(settings.TrafficTableConfig.HiddenColumns) != 0 {
		t.Fatalf("legacy traffic table config = %#v, want empty defaults", settings.TrafficTableConfig)
	}
}

func TestSaveTrafficTableConfigPersistsSanitizedV1Section(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	input := &TrafficTableConfig{
		HiddenColumns: []string{" process ", "unknown", "host", "process"},
	}
	if err := svc.SaveTrafficTableConfig(input); err != nil {
		t.Fatalf("SaveTrafficTableConfig: %v", err)
	}
	input.HiddenColumns[0] = "id"

	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []string{"host", "process"}
	if !reflect.DeepEqual(settings.TrafficTableConfig.HiddenColumns, want) {
		t.Fatalf("in-memory hidden columns = %#v, want %#v", settings.TrafficTableConfig.HiddenColumns, want)
	}

	var payloadVersion int
	var payload []byte
	if err := svc.repository.db.QueryRow(`
		SELECT payload_version, payload_json
		FROM app_settings
		WHERE section = ?
	`, settingsSectionTrafficTable).Scan(&payloadVersion, &payload); err != nil {
		t.Fatalf("read traffic table section: %v", err)
	}
	if payloadVersion != settingsPayloadVersion {
		t.Fatalf("traffic table payload version = %d, want %d", payloadVersion, settingsPayloadVersion)
	}
	var stored TrafficTableConfig
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatalf("decode traffic table section: %v", err)
	}
	if !reflect.DeepEqual(stored.HiddenColumns, want) {
		t.Fatalf("stored hidden columns = %#v, want %#v", stored.HiddenColumns, want)
	}
}

func TestSaveTrafficTableConfigFailurePreservesInMemoryConfig(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.SaveTrafficTableConfig(&TrafficTableConfig{
		HiddenColumns: []string{"process"},
	}); err != nil {
		t.Fatalf("SaveTrafficTableConfig baseline: %v", err)
	}
	if _, err := svc.repository.db.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatalf("drop app_settings: %v", err)
	}

	if err := svc.SaveTrafficTableConfig(&TrafficTableConfig{
		HiddenColumns: []string{"host"},
	}); err == nil {
		t.Fatal("SaveTrafficTableConfig unexpectedly succeeded without app_settings")
	}
	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get after failed save: %v", err)
	}
	want := []string{"process"}
	if !reflect.DeepEqual(settings.TrafficTableConfig.HiddenColumns, want) {
		t.Fatalf("hidden columns after failed save = %#v, want %#v", settings.TrafficTableConfig.HiddenColumns, want)
	}
}

func TestLoadLegacyDatabaseWithoutShortcutsUsesDefaults(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Update(&Settings{
		CommonConfig: &CommonConfig{ThemeMode: "dark", Language: "en"},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := svc.repository.db.Exec(`DELETE FROM app_settings WHERE section = ?`, settingsSectionShortcuts); err != nil {
		t.Fatalf("delete shortcuts section: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != "dark" || settings.CommonConfig.Language != "en" {
		t.Fatalf("legacy settings were not preserved: %+v", settings.CommonConfig)
	}
	if settings.ProxyConfig == nil || settings.WindowConfig == nil || settings.CacheConfig == nil || settings.HistoryRetentionConfig == nil || settings.ProcessAttributionConfig == nil || settings.TrafficTableConfig == nil {
		t.Fatalf("legacy settings were not initialized: %+v", settings)
	}
	if settings.Shortcuts == nil || len(settings.Shortcuts.Overrides) != 0 {
		t.Fatalf("missing shortcuts section = %+v, want empty defaults", settings.Shortcuts)
	}
}

func TestShortcutSettingsRoundTripPreservesDisabledAndUnknownOverrides(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	want := map[string]ShortcutOverride{
		"custom.unknown-command": {
			Binding: &ShortcutBinding{
				Modifiers: []ShortcutModifier{ShortcutModifierPrimary, ShortcutModifierShift},
				Key:       "k",
			},
			Scope: ShortcutScopeApplication,
		},
		"custom.disabled": {
			Binding: nil,
			Scope:   ShortcutScopeGlobal,
		},
	}
	if err := svc.Update(&Settings{Shortcuts: &ShortcutConfig{Overrides: want}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var sectionCount int
	if err := svc.repository.db.QueryRow(`SELECT COUNT(*) FROM app_settings`).Scan(&sectionCount); err != nil {
		t.Fatalf("count settings sections: %v", err)
	}
	if sectionCount != 9 {
		t.Fatalf("settings section count = %d, want 9", sectionCount)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(settings.Shortcuts.Overrides, want) {
		t.Fatalf("shortcut overrides = %#v, want %#v", settings.Shortcuts.Overrides, want)
	}
	if disabled := settings.Shortcuts.Overrides["custom.disabled"]; disabled.Binding != nil {
		t.Fatalf("disabled shortcut binding = %#v, want nil", disabled.Binding)
	}
}

func TestLoadSanitizesMalformedShortcutOverrides(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	payload := `{"overrides":{
		"Custom.Valid":{"binding":{"modifiers":["SHIFT","unsupported","primary","CONTROL","shift"],"key":" KeyA "},"scope":"APPLICATION"},
		"custom.disabled":{"binding":null,"scope":"global"},
		"bad..id":{"binding":null,"scope":"application"},
		"bad.scope":{"binding":null,"scope":"window"},
		"bad.modifiers":{"binding":{"modifiers":"primary","key":"k"},"scope":"application"},
		"bad.key":{"binding":{"modifiers":["alt"],"key":" "},"scope":"application"}
	}}`
	if _, err := svc.repository.db.Exec(`
		UPDATE app_settings SET payload_json = ? WHERE section = ?
	`, payload, settingsSectionShortcuts); err != nil {
		t.Fatalf("replace shortcuts payload: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := map[string]ShortcutOverride{
		"Custom.Valid": {
			Binding: &ShortcutBinding{
				Modifiers: []ShortcutModifier{ShortcutModifierPrimary, ShortcutModifierControl, ShortcutModifierShift},
				Key:       "KeyA",
			},
			Scope: ShortcutScopeApplication,
		},
		"custom.disabled": {
			Scope: ShortcutScopeGlobal,
		},
		"bad.key": {
			Binding: &ShortcutBinding{
				Modifiers: []ShortcutModifier{ShortcutModifierAlt},
				Key:       " ",
			},
			Scope: ShortcutScopeApplication,
		},
	}
	if !reflect.DeepEqual(settings.Shortcuts.Overrides, want) {
		t.Fatalf("sanitized shortcut overrides = %#v, want %#v", settings.Shortcuts.Overrides, want)
	}
}

func TestLoadPreservesShortcutCommandIDsAndCanonicalizesKeys(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	payload := `{"overrides":{
		"app.showMainWindow":{"binding":{"modifiers":["PRIMARY"],"key":"ENTER"},"scope":"application"},
		"workspace.newHTTPRequest":{"binding":{"modifiers":["control"],"key":"arrowleft"},"scope":"application"},
		"unknown.CamelCase":{"binding":{"modifiers":[],"key":"Q"},"scope":"global"}
	}}`
	if _, err := svc.repository.db.Exec(`
		UPDATE app_settings SET payload_json = ? WHERE section = ?
	`, payload, settingsSectionShortcuts); err != nil {
		t.Fatalf("replace shortcuts payload: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := map[string]ShortcutOverride{
		"app.showMainWindow": {
			Binding: &ShortcutBinding{
				Modifiers: []ShortcutModifier{ShortcutModifierPrimary},
				Key:       "Enter",
			},
			Scope: ShortcutScopeApplication,
		},
		"workspace.newHTTPRequest": {
			Binding: &ShortcutBinding{
				Modifiers: []ShortcutModifier{ShortcutModifierControl},
				Key:       "ArrowLeft",
			},
			Scope: ShortcutScopeApplication,
		},
		"unknown.CamelCase": {
			Binding: &ShortcutBinding{Modifiers: []ShortcutModifier{}, Key: "q"},
			Scope:   ShortcutScopeGlobal,
		},
	}
	if !reflect.DeepEqual(settings.Shortcuts.Overrides, want) {
		t.Fatalf("shortcut overrides = %#v, want %#v", settings.Shortcuts.Overrides, want)
	}
}

func TestLoadDropsShortcutOverrideWithoutBindingButKeepsExplicitNull(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	payload := `{"overrides":{
		"app.showMainWindow":{"scope":"application"},
		"workspace.newHTTPRequest":{"binding":null,"scope":"application"}
	}}`
	if _, err := svc.repository.db.Exec(`
		UPDATE app_settings SET payload_json = ? WHERE section = ?
	`, payload, settingsSectionShortcuts); err != nil {
		t.Fatalf("replace shortcuts payload: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := map[string]ShortcutOverride{
		"workspace.newHTTPRequest": {
			Scope: ShortcutScopeApplication,
		},
	}
	if !reflect.DeepEqual(settings.Shortcuts.Overrides, want) {
		t.Fatalf("shortcut overrides = %#v, want %#v", settings.Shortcuts.Overrides, want)
	}
}

func TestGetLazilyLoadsSettings(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Update(&Settings{CommonConfig: &CommonConfig{ThemeMode: "dark", Language: "en"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded := newPersistentTestSettingService(t)

	settings, err := reloaded.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != "dark" {
		t.Fatalf("expected theme mode dark, got %q", settings.CommonConfig.ThemeMode)
	}
	if settings.CommonConfig.Language != "en" {
		t.Fatalf("expected language en, got %q", settings.CommonConfig.Language)
	}
	if settings.CommonConfig.LogLevel != string(logger.DefaultLogLevel()) {
		t.Fatalf("expected missing log level to default to %q, got %q", logger.DefaultLogLevel(), settings.CommonConfig.LogLevel)
	}
}

func TestLoadIgnoresLegacySettingsJSON(t *testing.T) {
	settingPath := configureTestSettingsPath(t)
	content := []byte(`{"commonConfig":`)
	if err := os.WriteFile(settingPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile settings: %v", err)
	}
	svc := newPersistentTestSettingService(t)
	settings, err := svc.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.CommonConfig.ThemeMode != defaultThemeMode || settings.CommonConfig.Language != defaultLanguage {
		t.Fatalf("legacy JSON affected SQLite defaults: %+v", settings.CommonConfig)
	}
	after, err := os.ReadFile(settingPath)
	if err != nil {
		t.Fatalf("ReadFile settings: %v", err)
	}
	if string(after) != string(content) {
		t.Fatal("legacy settings JSON was modified")
	}
}

func TestLoadRejectsCorruptedDatabaseSettingPayload(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := svc.Save(); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}
	if _, err := svc.repository.db.Exec(`
		UPDATE app_settings SET payload_json = '{' WHERE section = ?
	`, settingsSectionCommon); err != nil {
		t.Fatalf("corrupt settings payload: %v", err)
	}

	reloaded := newPersistentTestSettingService(t)
	if err := reloaded.Load(); err == nil {
		t.Fatal("expected corrupted database settings payload to fail")
	}
}

func TestGenerateCurrentCACertificateCreatesValidPair(t *testing.T) {
	svc := newTestSettingServiceWithCAPaths(t)

	info, err := svc.GenerateCurrentCACertificate(GenerateCACertificateRequest{})
	if err != nil {
		t.Fatalf("GenerateCurrentCACertificate: %v", err)
	}
	if !info.CertExists || !info.KeyExists {
		t.Fatalf("expected cert and key to exist: %+v", info)
	}
	if !info.ValidPair {
		t.Fatalf("expected valid cert/key pair: %+v", info)
	}
	if !info.IsCA {
		t.Fatalf("expected generated certificate to be a CA: %+v", info)
	}
	if !strings.Contains(info.Subject, defaultCACertCommonName) {
		t.Fatalf("expected default common name in subject, got %q", info.Subject)
	}
	if info.NotBeforeMicros <= 0 || info.NotAfterMicros <= info.NotBeforeMicros {
		t.Fatalf("expected valid CA certificate timestamps: %+v", info)
	}

	reloaded, err := svc.GetCACertificateInfo()
	if err != nil {
		t.Fatalf("GetCACertificateInfo: %v", err)
	}
	if !reloaded.ValidPair || reloaded.SHA256Fingerprint == "" {
		t.Fatalf("expected reloadable cert info with fingerprint: %+v", reloaded)
	}
	if reloaded.NotBeforeMicros != info.NotBeforeMicros || reloaded.NotAfterMicros != info.NotAfterMicros {
		t.Fatalf("reloaded CA certificate timestamps = %d/%d, want %d/%d", reloaded.NotBeforeMicros, reloaded.NotAfterMicros, info.NotBeforeMicros, info.NotAfterMicros)
	}
}

func TestGenerateCurrentCACertificateDoesNotOverwriteWithoutFlag(t *testing.T) {
	svc := newTestSettingServiceWithCAPaths(t)

	if _, err := svc.GenerateCurrentCACertificate(GenerateCACertificateRequest{}); err != nil {
		t.Fatalf("initial GenerateCurrentCACertificate: %v", err)
	}
	if _, err := svc.GenerateCurrentCACertificate(GenerateCACertificateRequest{}); err == nil {
		t.Fatal("expected overwrite=false generation to fail when cert exists")
	}
}

func TestGenerateCurrentCACertificateOverwriteBacksUpExistingFiles(t *testing.T) {
	svc := newTestSettingServiceWithCAPaths(t)

	first, err := svc.GenerateCurrentCACertificate(GenerateCACertificateRequest{})
	if err != nil {
		t.Fatalf("initial GenerateCurrentCACertificate: %v", err)
	}
	second, err := svc.GenerateCurrentCACertificate(GenerateCACertificateRequest{
		Overwrite:  true,
		CommonName: "FlowLens Test CA",
		ValidDays:  30,
	})
	if err != nil {
		t.Fatalf("overwrite GenerateCurrentCACertificate: %v", err)
	}
	if !strings.Contains(second.Subject, "FlowLens Test CA") {
		t.Fatalf("expected custom common name in subject, got %q", second.Subject)
	}
	if first.SHA256Fingerprint == second.SHA256Fingerprint {
		t.Fatal("expected regenerated certificate fingerprint to change")
	}

	certBackups, err := filepath.Glob(first.CertPath + ".*.bak")
	if err != nil {
		t.Fatalf("glob cert backups: %v", err)
	}
	keyBackups, err := filepath.Glob(first.KeyPath + ".*.bak")
	if err != nil {
		t.Fatalf("glob key backups: %v", err)
	}
	if len(certBackups) != 1 || len(keyBackups) != 1 {
		t.Fatalf("expected one cert and one key backup, got cert=%v key=%v", certBackups, keyBackups)
	}
}

func newTestSettingServiceWithCAPaths(t *testing.T) *SettingService {
	t.Helper()
	dir := t.TempDir()
	svc := &SettingService{}
	err := svc.Update(&Settings{
		ProxyConfig: &ProxyConfig{
			Mode:       ProxyModeHTTP,
			Host:       "127.0.0.1",
			Port:       8080,
			CACertPath: filepath.Join(dir, "ca.crt"),
			CAKeyPath:  filepath.Join(dir, "ca.key"),
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return svc
}

func configureTestSettingsPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	return filepath.Join(baseDir, "settings.json")
}

func newPersistentTestSettingService(t *testing.T) *SettingService {
	t.Helper()
	db, err := appdatabase.Open()
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}
