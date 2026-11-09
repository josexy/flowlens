package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAtCreatesCurrentSchema(t *testing.T) {
	db := openTestDatabase(t)

	for _, table := range []string{
		"api_nodes", "api_request_payloads", "app_settings",
		"python_plugins", "python_plugin_rules",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("query table %q: %v", table, err)
		}
	}
	for _, index := range []string{
		"api_nodes_root_name_uq", "api_nodes_child_name_uq", "api_nodes_parent_idx",
		"python_plugins_sort_idx", "python_plugin_rules_plugin_sort_idx",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name); err != nil {
			t.Fatalf("query index %q: %v", index, err)
		}
	}
	for _, trigger := range []string{"api_nodes_parent_folder_insert", "api_nodes_parent_folder_update"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&name); err != nil {
			t.Fatalf("query trigger %q: %v", trigger, err)
		}
	}
	for _, table := range []string{"schema_migrations", "legacy_imports"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("query removed table %q: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("removed table %q still exists", table)
		}
	}
}

func TestOpenAtPythonPluginSchemaIsIdempotentAndCascadesRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseFileName)
	first, err := OpenAt(path)
	if err != nil {
		t.Fatalf("first OpenAt: %v", err)
	}
	if _, err := first.Exec(`
		INSERT INTO python_plugins(
			id, name, description, enabled, sort_order, params_json,
			active_revision, last_good_revision, validation_status,
			validation_error, created_at, updated_at
		) VALUES ('11111111-1111-4111-8111-111111111111', 'Plugin', '', 0, 0, '{}', '', '', 'unavailable', '', 1, 1)
	`); err != nil {
		first.Close()
		t.Fatalf("insert plugin: %v", err)
	}
	if _, err := first.Exec(`
		INSERT INTO python_plugin_rules(
			id, plugin_id, enabled, method, url_pattern, sort_order, created_at, updated_at
		) VALUES (
			'22222222-2222-4222-8222-222222222222',
			'11111111-1111-4111-8111-111111111111', 1, 'GET', 'https://example.com/*', 0, 1, 1
		)
	`); err != nil {
		first.Close()
		t.Fatalf("insert plugin rule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	second, err := OpenAt(path)
	if err != nil {
		t.Fatalf("second OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := second.Exec(`DELETE FROM python_plugins WHERE id = '11111111-1111-4111-8111-111111111111'`); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	var ruleCount int
	if err := second.QueryRow(`SELECT COUNT(*) FROM python_plugin_rules`).Scan(&ruleCount); err != nil {
		t.Fatalf("count plugin rules: %v", err)
	}
	if ruleCount != 0 {
		t.Fatalf("plugin rule count = %d, want 0 after cascade", ruleCount)
	}
}

func TestOpenAtIsIdempotentAndPreservesCurrentData(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseFileName)
	first, err := OpenAt(path)
	if err != nil {
		t.Fatalf("first OpenAt: %v", err)
	}
	if _, err := first.Exec(`
		INSERT INTO api_nodes(
			id, parent_id, kind, name, normalized_name, sort_order,
			created_at, updated_at, http_method
		) VALUES ('folder-1', NULL, 'folder', 'Folder', 'folder', 0, 1, 1, '')
	`); err != nil {
		first.Close()
		t.Fatalf("insert current schema data: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	second, err := OpenAt(path)
	if err != nil {
		t.Fatalf("second OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	var name string
	if err := second.QueryRow(`SELECT name FROM api_nodes WHERE id = 'folder-1'`).Scan(&name); err != nil {
		t.Fatalf("query preserved data: %v", err)
	}
	if name != "Folder" {
		t.Fatalf("preserved folder name = %q, want Folder", name)
	}
}

func TestOpenAtEnforcesForeignKeys(t *testing.T) {
	db := openTestDatabase(t)
	_, err := db.Exec(`
		INSERT INTO api_nodes (
			id, parent_id, kind, name, normalized_name, sort_order, created_at, updated_at
		) VALUES ('request-1', 'missing-folder', 'http', 'Request', 'request', 0, 1, 1)
	`)
	if err == nil {
		t.Fatal("expected insert with missing parent to fail")
	}
}

func TestOpenAtRejectsFailedQuickCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseFileName)
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}
	if db, err := OpenAt(path); err == nil {
		db.Close()
		t.Fatal("expected corrupt database to be rejected")
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenAt(filepath.Join(t.TempDir(), databaseFileName))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("PingContext: %v", err)
	}
	return db
}
