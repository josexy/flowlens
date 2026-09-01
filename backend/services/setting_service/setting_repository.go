package settingservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const (
	settingsSectionCommon             = "common"
	settingsSectionProxy              = "proxy"
	settingsSectionWindow             = "window"
	settingsSectionCache              = "cache"
	settingsSectionHistoryRetention   = "history_retention"
	settingsSectionProcessAttribution = "process_attribution"
	settingsSectionTrafficTable       = "traffic_table"
	settingsSectionPythonPlugins      = "python_plugins"
	settingsSectionShortcuts          = "shortcuts"
	settingsPayloadVersion            = 1
)

type settingRepository struct {
	db *sql.DB
}

type settingSection struct {
	name  string
	value any
}

func newSettingRepository(db *sql.DB) *settingRepository {
	return &settingRepository{db: db}
}

func (r *settingRepository) load(ctx context.Context) (*Settings, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT section, payload_version, payload_json FROM app_settings
	`)
	if err != nil {
		return nil, fmt.Errorf("query application settings: %w", err)
	}
	defer rows.Close()

	settings := new(Settings)
	for rows.Next() {
		var section string
		var payloadVersion int
		var payload []byte
		if err := rows.Scan(&section, &payloadVersion, &payload); err != nil {
			return nil, fmt.Errorf("scan application setting section: %w", err)
		}
		if !isKnownSettingsSection(section) {
			continue
		}
		if payloadVersion != settingsPayloadVersion {
			return nil, fmt.Errorf("unsupported settings payload version %d for section %q", payloadVersion, section)
		}
		if err := decodeSettingSection(settings, section, payload); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application setting sections: %w", err)
	}
	return settings, nil
}

func (r *settingRepository) save(ctx context.Context, settings *Settings) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save application settings: %w", err)
	}
	defer tx.Rollback()
	if err := saveSettingsTx(ctx, tx, settings); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit application settings: %w", err)
	}
	return nil
}

func (r *settingRepository) saveShortcutConfig(ctx context.Context, config *ShortcutConfig) error {
	if config == nil {
		return fmt.Errorf("shortcut config cannot be nil")
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode settings section %q: %w", settingsSectionShortcuts, err)
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(section) DO UPDATE SET
			payload_version = excluded.payload_version,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, settingsSectionShortcuts, settingsPayloadVersion, payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save settings section %q: %w", settingsSectionShortcuts, err)
	}
	return nil
}

func (r *settingRepository) saveTrafficTableConfig(ctx context.Context, config *TrafficTableConfig) error {
	if config == nil {
		return fmt.Errorf("traffic table config cannot be nil")
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode settings section %q: %w", settingsSectionTrafficTable, err)
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(section) DO UPDATE SET
			payload_version = excluded.payload_version,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, settingsSectionTrafficTable, settingsPayloadVersion, payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save settings section %q: %w", settingsSectionTrafficTable, err)
	}
	return nil
}

func (r *settingRepository) savePythonPluginConfig(ctx context.Context, config *PythonPluginConfig) error {
	if config == nil {
		return fmt.Errorf("python plugin config cannot be nil")
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode settings section %q: %w", settingsSectionPythonPlugins, err)
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(section) DO UPDATE SET
			payload_version = excluded.payload_version,
			payload_json = excluded.payload_json,
			updated_at = excluded.updated_at
	`, settingsSectionPythonPlugins, settingsPayloadVersion, payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save settings section %q: %w", settingsSectionPythonPlugins, err)
	}
	return nil
}

func saveSettingsTx(ctx context.Context, tx *sql.Tx, settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	sections := []settingSection{
		{name: settingsSectionCommon, value: settings.CommonConfig},
		{name: settingsSectionProxy, value: settings.ProxyConfig},
		{name: settingsSectionWindow, value: settings.WindowConfig},
		{name: settingsSectionCache, value: settings.CacheConfig},
		{name: settingsSectionHistoryRetention, value: settings.HistoryRetentionConfig},
		{name: settingsSectionProcessAttribution, value: settings.ProcessAttributionConfig},
		{name: settingsSectionTrafficTable, value: settings.TrafficTableConfig},
		{name: settingsSectionPythonPlugins, value: settings.PythonPluginConfig},
		{name: settingsSectionShortcuts, value: settings.Shortcuts},
	}
	now := time.Now().UnixMilli()
	for _, section := range sections {
		if section.value == nil {
			return fmt.Errorf("settings section %q cannot be nil", section.name)
		}
		payload, err := json.Marshal(section.value)
		if err != nil {
			return fmt.Errorf("encode settings section %q: %w", section.name, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(section) DO UPDATE SET
				payload_version = excluded.payload_version,
				payload_json = excluded.payload_json,
				updated_at = excluded.updated_at
		`, section.name, settingsPayloadVersion, payload, now); err != nil {
			return fmt.Errorf("save settings section %q: %w", section.name, err)
		}
	}
	return nil
}

func decodeSettingSection(settings *Settings, section string, payload []byte) error {
	switch section {
	case settingsSectionCommon:
		var value *CommonConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.CommonConfig = value
	case settingsSectionProxy:
		var value *ProxyConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.ProxyConfig = value
	case settingsSectionWindow:
		var value *WindowConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.WindowConfig = value
	case settingsSectionCache:
		var value *CacheConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.CacheConfig = value
	case settingsSectionHistoryRetention:
		var value *HistoryRetentionConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.HistoryRetentionConfig = value
	case settingsSectionProcessAttribution:
		var value *ProcessAttributionConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.ProcessAttributionConfig = value
	case settingsSectionTrafficTable:
		var value *TrafficTableConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.TrafficTableConfig = value
	case settingsSectionPythonPlugins:
		var value *PythonPluginConfig
		if err := json.Unmarshal(payload, &value); err != nil || value == nil {
			return settingSectionDecodeError(section, err)
		}
		settings.PythonPluginConfig = value
	case settingsSectionShortcuts:
		settings.Shortcuts = decodeShortcutConfig(payload)
	}
	return nil
}

func settingSectionDecodeError(section string, err error) error {
	if err == nil {
		return fmt.Errorf("settings section %q contains null payload", section)
	}
	return fmt.Errorf("decode settings section %q: %w", section, err)
}

func isKnownSettingsSection(section string) bool {
	switch section {
	case settingsSectionCommon,
		settingsSectionProxy,
		settingsSectionWindow,
		settingsSectionCache,
		settingsSectionHistoryRetention,
		settingsSectionProcessAttribution,
		settingsSectionTrafficTable,
		settingsSectionPythonPlugins,
		settingsSectionShortcuts:
		return true
	default:
		return false
	}
}
