package database

import (
	"context"
	"database/sql"
	"fmt"
)

const currentSchemaSQL = `
CREATE TABLE IF NOT EXISTS api_nodes (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES api_nodes(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('folder', 'http', 'websocket')),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    http_method TEXT NOT NULL DEFAULT '',
    CHECK (parent_id IS NOT NULL OR kind = 'folder')
);

CREATE UNIQUE INDEX IF NOT EXISTS api_nodes_root_name_uq
    ON api_nodes(normalized_name)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS api_nodes_child_name_uq
    ON api_nodes(parent_id, normalized_name)
    WHERE parent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS api_nodes_parent_idx
    ON api_nodes(parent_id, kind, sort_order);

CREATE TABLE IF NOT EXISTS api_request_payloads (
    node_id TEXT PRIMARY KEY
        REFERENCES api_nodes(id) ON DELETE CASCADE,
    payload_version INTEGER NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS api_nodes_parent_folder_insert
BEFORE INSERT ON api_nodes
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN COALESCE(
            (SELECT kind = 'folder' FROM api_nodes WHERE id = NEW.parent_id),
            0
        ) = 0
        THEN RAISE(ABORT, 'api node parent must be a folder')
    END;
END;

CREATE TRIGGER IF NOT EXISTS api_nodes_parent_folder_update
BEFORE UPDATE OF parent_id ON api_nodes
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN COALESCE(
            (SELECT kind = 'folder' FROM api_nodes WHERE id = NEW.parent_id),
            0
        ) = 0
        THEN RAISE(ABORT, 'api node parent must be a folder')
    END;
END;

CREATE TABLE IF NOT EXISTS app_settings (
    section TEXT PRIMARY KEY,
    payload_version INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS python_plugins (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    params_json TEXT NOT NULL DEFAULT '{}',
    active_revision TEXT NOT NULL DEFAULT '',
    last_good_revision TEXT NOT NULL DEFAULT '',
    validation_status TEXT NOT NULL DEFAULT 'unavailable'
        CHECK (validation_status IN ('unavailable', 'valid', 'invalid')),
    validation_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS python_plugins_sort_idx
    ON python_plugins(sort_order, created_at, id);

CREATE TABLE IF NOT EXISTS python_plugin_rules (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL
        REFERENCES python_plugins(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    method TEXT NOT NULL,
    url_pattern TEXT NOT NULL,
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS python_plugin_rules_plugin_sort_idx
    ON python_plugin_rules(plugin_id, sort_order, created_at, id);
`

func createCurrentSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite schema creation: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, currentSchemaSQL); err != nil {
		return fmt.Errorf("create current sqlite schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit current sqlite schema: %w", err)
	}
	return nil
}
