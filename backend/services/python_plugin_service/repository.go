package pythonpluginservice

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	maxPluginNameLength        = 200
	maxPluginDescriptionLength = 4000
	maxParamsJSONBytes         = 1024 * 1024
	maxRuleURLPatternLength    = 8192
)

type repository struct {
	db *sql.DB
}

func newRepository(db *sql.DB) *repository {
	return &repository{db: db}
}

func (r *repository) createPlugin(ctx context.Context, input CreatePluginInput) (*Plugin, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if err := validateUUID(id); err != nil {
		return nil, err
	}
	name, description, paramsJSON, err := normalizePluginInput(input.Name, input.Description, input.ParamsJSON)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create python plugin: %w", err)
	}
	defer tx.Rollback()
	sortOrder, err := nextPluginSortOrderTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO python_plugins(
			id, name, description, enabled, sort_order, params_json,
			active_revision, last_good_revision, validation_status,
			validation_error, created_at, updated_at
		) VALUES (?, ?, ?, 0, ?, ?, '', '', ?, '', ?, ?)
	`, id, name, description, sortOrder, paramsJSON, ValidationStatusUnavailable, now, now); err != nil {
		return nil, fmt.Errorf("insert python plugin %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create python plugin: %w", err)
	}
	return &Plugin{
		ID: id, Name: name, Description: description, SortOrder: sortOrder,
		ParamsJSON: paramsJSON, ValidationStatus: ValidationStatusUnavailable,
		CreatedAt: now, UpdatedAt: now, Rules: []*Rule{},
	}, nil
}

func (r *repository) getPlugin(ctx context.Context, id string) (*Plugin, error) {
	id = strings.TrimSpace(id)
	plugin, err := scanPlugin(r.db.QueryRowContext(ctx, `
		SELECT id, name, description, enabled, sort_order, params_json,
		       active_revision, last_good_revision, validation_status,
		       validation_error, created_at, updated_at
		FROM python_plugins WHERE id = ?
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("python plugin %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query python plugin %q: %w", id, err)
	}
	plugin.Rules, err = r.listRules(ctx, id)
	if err != nil {
		return nil, err
	}
	return plugin, nil
}

func (r *repository) listPlugins(ctx context.Context) ([]*Plugin, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, enabled, sort_order, params_json,
		       active_revision, last_good_revision, validation_status,
		       validation_error, created_at, updated_at
		FROM python_plugins
		ORDER BY sort_order, created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query python plugins: %w", err)
	}
	defer rows.Close()
	plugins := make([]*Plugin, 0)
	for rows.Next() {
		plugin, err := scanPlugin(rows)
		if err != nil {
			return nil, fmt.Errorf("scan python plugin: %w", err)
		}
		plugin.Rules = []*Rule{}
		plugins = append(plugins, plugin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate python plugins: %w", err)
	}
	for _, plugin := range plugins {
		plugin.Rules, err = r.listRules(ctx, plugin.ID)
		if err != nil {
			return nil, err
		}
	}
	return plugins, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPlugin(scanner rowScanner) (*Plugin, error) {
	plugin := new(Plugin)
	var enabled int
	if err := scanner.Scan(
		&plugin.ID, &plugin.Name, &plugin.Description, &enabled, &plugin.SortOrder,
		&plugin.ParamsJSON, &plugin.ActiveRevision, &plugin.LastGoodRevision,
		&plugin.ValidationStatus, &plugin.ValidationError, &plugin.CreatedAt, &plugin.UpdatedAt,
	); err != nil {
		return nil, err
	}
	plugin.Enabled = enabled != 0
	return plugin, nil
}

func (r *repository) updatePlugin(ctx context.Context, id string, input UpdatePluginInput) (*Plugin, error) {
	id = strings.TrimSpace(id)
	name, description, paramsJSON, err := normalizePluginInput(input.Name, input.Description, input.ParamsJSON)
	if err != nil {
		return nil, err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins
		SET name = ?, description = ?, params_json = ?, updated_at = ?
		WHERE id = ?
	`, name, description, paramsJSON, time.Now().UnixMilli(), id)
	if err != nil {
		return nil, fmt.Errorf("update python plugin %q: %w", id, err)
	}
	if err := requireAffected(result, "python plugin", id); err != nil {
		return nil, err
	}
	return r.getPlugin(ctx, id)
}

func (r *repository) setPluginEnabled(ctx context.Context, id string, enabled bool) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins SET enabled = ?, updated_at = ? WHERE id = ?
	`, boolInt(enabled), time.Now().UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("update python plugin enabled state: %w", err)
	}
	return requireAffected(result, "python plugin", strings.TrimSpace(id))
}

func (r *repository) setPluginActivation(ctx context.Context, id, revision string, status ValidationStatus, diagnostic string) error {
	id = strings.TrimSpace(id)
	revision = strings.TrimSpace(revision)
	diagnostic = strings.TrimSpace(diagnostic)
	if status != ValidationStatusValid || revision == "" {
		return errors.New("a valid non-empty revision is required for activation")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins
		SET active_revision = ?, last_good_revision = ?, validation_status = ?,
		    validation_error = ?, updated_at = ?
		WHERE id = ?
	`, revision, revision, status, diagnostic, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("activate python plugin %q: %w", id, err)
	}
	return requireAffected(result, "python plugin", id)
}

func (r *repository) activatePluginPackage(ctx context.Context, id string, manifest Manifest, revision string) error {
	id = strings.TrimSpace(id)
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return errors.New("revision cannot be empty")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins
		SET name = ?, description = ?, active_revision = ?, last_good_revision = ?,
		    validation_status = ?, validation_error = '', updated_at = ?
		WHERE id = ?
	`, manifest.Name, manifest.Description, revision, revision, ValidationStatusValid, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("activate python plugin package %q: %w", id, err)
	}
	return requireAffected(result, "python plugin", id)
}

func (r *repository) setPluginValidationFailure(ctx context.Context, id string, status ValidationStatus, diagnostic string) error {
	if status != ValidationStatusInvalid && status != ValidationStatusUnavailable {
		return fmt.Errorf("invalid failure validation status %q", status)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins
		SET validation_status = ?, validation_error = ?, updated_at = ?
		WHERE id = ?
	`, status, strings.TrimSpace(diagnostic), time.Now().UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("record python plugin validation failure: %w", err)
	}
	return requireAffected(result, "python plugin", strings.TrimSpace(id))
}

func (r *repository) disableMissingPlugin(ctx context.Context, id, diagnostic string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugins
		SET enabled = 0, validation_status = ?, validation_error = ?, updated_at = ?
		WHERE id = ?
	`, ValidationStatusUnavailable, strings.TrimSpace(diagnostic), time.Now().UnixMilli(), strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("mark missing python plugin unavailable: %w", err)
	}
	return requireAffected(result, "python plugin", strings.TrimSpace(id))
}

func (r *repository) deletePlugin(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete python plugin: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM python_plugins WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete python plugin %q: %w", id, err)
	}
	if err := requireAffected(result, "python plugin", id); err != nil {
		return err
	}
	if err := compactPluginSortOrderTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete python plugin: %w", err)
	}
	return nil
}

func (r *repository) reorderPlugins(ctx context.Context, ids []string) error {
	return reorderRows(ctx, r.db, "python_plugins", "", "", ids)
}

func (r *repository) createRule(ctx context.Context, pluginID string, input CreateRuleInput) (*Rule, error) {
	pluginID = strings.TrimSpace(pluginID)
	if err := validateUUID(pluginID); err != nil {
		return nil, fmt.Errorf("invalid plugin ID: %w", err)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if err := validateUUID(id); err != nil {
		return nil, fmt.Errorf("invalid rule ID: %w", err)
	}
	method, pattern, err := normalizeRule(input.Method, input.URLPattern)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create python plugin rule: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM python_plugins WHERE id = ?`, pluginID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("python plugin %q not found", pluginID)
	} else if err != nil {
		return nil, fmt.Errorf("query python plugin %q: %w", pluginID, err)
	}
	var sortOrder int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sort_order), -1) + 1 FROM python_plugin_rules WHERE plugin_id = ?
	`, pluginID).Scan(&sortOrder); err != nil {
		return nil, fmt.Errorf("query next python plugin rule sort order: %w", err)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO python_plugin_rules(
			id, plugin_id, enabled, method, url_pattern, sort_order, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, pluginID, boolInt(input.Enabled), method, pattern, sortOrder, now, now); err != nil {
		return nil, fmt.Errorf("insert python plugin rule %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create python plugin rule: %w", err)
	}
	return &Rule{
		ID: id, PluginID: pluginID, Enabled: input.Enabled, Method: method,
		URLPattern: pattern, SortOrder: sortOrder, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (r *repository) listRules(ctx context.Context, pluginID string) ([]*Rule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plugin_id, enabled, method, url_pattern, sort_order, created_at, updated_at
		FROM python_plugin_rules WHERE plugin_id = ?
		ORDER BY sort_order, created_at, id
	`, strings.TrimSpace(pluginID))
	if err != nil {
		return nil, fmt.Errorf("query python plugin rules: %w", err)
	}
	defer rows.Close()
	rules := make([]*Rule, 0)
	for rows.Next() {
		rule := new(Rule)
		var enabled int
		if err := rows.Scan(
			&rule.ID, &rule.PluginID, &enabled, &rule.Method, &rule.URLPattern,
			&rule.SortOrder, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan python plugin rule: %w", err)
		}
		rule.Enabled = enabled != 0
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate python plugin rules: %w", err)
	}
	return rules, nil
}

func (r *repository) updateRule(ctx context.Context, pluginID, id string, input UpdateRuleInput) (*Rule, error) {
	pluginID, id = strings.TrimSpace(pluginID), strings.TrimSpace(id)
	method, pattern, err := normalizeRule(input.Method, input.URLPattern)
	if err != nil {
		return nil, err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE python_plugin_rules
		SET enabled = ?, method = ?, url_pattern = ?, updated_at = ?
		WHERE id = ? AND plugin_id = ?
	`, boolInt(input.Enabled), method, pattern, time.Now().UnixMilli(), id, pluginID)
	if err != nil {
		return nil, fmt.Errorf("update python plugin rule %q: %w", id, err)
	}
	if err := requireAffected(result, "python plugin rule", id); err != nil {
		return nil, err
	}
	rules, err := r.listRules(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return nil, fmt.Errorf("python plugin rule %q not found", id)
}

func (r *repository) deleteRule(ctx context.Context, pluginID, id string) error {
	pluginID, id = strings.TrimSpace(pluginID), strings.TrimSpace(id)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete python plugin rule: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM python_plugin_rules WHERE id = ? AND plugin_id = ?`, id, pluginID)
	if err != nil {
		return fmt.Errorf("delete python plugin rule %q: %w", id, err)
	}
	if err := requireAffected(result, "python plugin rule", id); err != nil {
		return err
	}
	if err := compactRuleSortOrderTx(ctx, tx, pluginID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete python plugin rule: %w", err)
	}
	return nil
}

func (r *repository) reorderRules(ctx context.Context, pluginID string, ids []string) error {
	return reorderRows(ctx, r.db, "python_plugin_rules", "plugin_id", strings.TrimSpace(pluginID), ids)
}

func reorderRows(ctx context.Context, db *sql.DB, table, scopeColumn, scopeValue string, ids []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder %s: %w", table, err)
	}
	defer tx.Rollback()
	query := "SELECT id FROM " + table
	args := make([]any, 0, 1)
	if scopeColumn != "" {
		query += " WHERE " + scopeColumn + " = ?"
		args = append(args, scopeValue)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query reorder members: %w", err)
	}
	existing := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan reorder member: %w", err)
		}
		existing[id] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close reorder rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate reorder members: %w", err)
	}
	if len(ids) != len(existing) {
		return fmt.Errorf("reorder requires exactly %d IDs", len(existing))
	}
	seen := make(map[string]struct{}, len(ids))
	for index, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if _, ok := existing[id]; !ok {
			return fmt.Errorf("reorder member %q not found", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("reorder member %q is duplicated", id)
		}
		seen[id] = struct{}{}
		update := "UPDATE " + table + " SET sort_order = ?, updated_at = ? WHERE id = ?"
		updateArgs := []any{index, time.Now().UnixMilli(), id}
		if scopeColumn != "" {
			update += " AND " + scopeColumn + " = ?"
			updateArgs = append(updateArgs, scopeValue)
		}
		result, err := tx.ExecContext(ctx, update, updateArgs...)
		if err != nil {
			return fmt.Errorf("update reorder member %q: %w", id, err)
		}
		if err := requireAffected(result, "reorder member", id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder %s: %w", table, err)
	}
	return nil
}

func nextPluginSortOrderTx(ctx context.Context, tx *sql.Tx) (int, error) {
	var sortOrder int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM python_plugins`).Scan(&sortOrder); err != nil {
		return 0, fmt.Errorf("query next python plugin sort order: %w", err)
	}
	return sortOrder, nil
}

func compactPluginSortOrderTx(ctx context.Context, tx *sql.Tx) error {
	return compactSortOrderTx(ctx, tx, "python_plugins", "", "")
}

func compactRuleSortOrderTx(ctx context.Context, tx *sql.Tx, pluginID string) error {
	return compactSortOrderTx(ctx, tx, "python_plugin_rules", "plugin_id", pluginID)
}

func compactSortOrderTx(ctx context.Context, tx *sql.Tx, table, scopeColumn, scopeValue string) error {
	query := "SELECT id FROM " + table
	args := make([]any, 0, 1)
	if scopeColumn != "" {
		query += " WHERE " + scopeColumn + " = ?"
		args = append(args, scopeValue)
	}
	query += " ORDER BY sort_order, created_at, id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query compact sort order: %w", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan compact sort order: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close compact sort rows: %w", err)
	}
	for index, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET sort_order = ? WHERE id = ?", index, id); err != nil {
			return fmt.Errorf("compact sort order for %q: %w", id, err)
		}
	}
	return nil
}

func normalizePluginInput(name, description, paramsJSON string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", errors.New("plugin name cannot be empty")
	}
	if len([]rune(name)) > maxPluginNameLength {
		return "", "", "", fmt.Errorf("plugin name exceeds %d characters", maxPluginNameLength)
	}
	description = strings.TrimSpace(description)
	if len([]rune(description)) > maxPluginDescriptionLength {
		return "", "", "", fmt.Errorf("plugin description exceeds %d characters", maxPluginDescriptionLength)
	}
	normalizedParams, err := normalizeParamsJSON(paramsJSON)
	if err != nil {
		return "", "", "", err
	}
	return name, description, normalizedParams, nil
}

func normalizeParamsJSON(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("plugin params must be a JSON object")
	}
	if len(value) > maxParamsJSONBytes {
		return "", fmt.Errorf("plugin params exceed %d bytes", maxParamsJSONBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode plugin params: %w", err)
	}
	if _, ok := decoded.(map[string]any); !ok {
		return "", errors.New("plugin params must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return "", errors.New("plugin params contain multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("decode plugin params: %w", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(value)); err != nil {
		return "", fmt.Errorf("compact plugin params: %w", err)
	}
	return compact.String(), nil
}

func normalizeRule(method, pattern string) (string, string, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return "", "", errors.New("rule method cannot be empty")
	}
	if method != "*" {
		if method != strings.ToUpper(method) {
			return "", "", errors.New("rule method must be uppercase or *")
		}
		for _, char := range method {
			if !isHTTPTokenRune(char) {
				return "", "", errors.New("rule method must be an HTTP token or *")
			}
		}
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", errors.New("rule URL pattern cannot be empty")
	}
	if len(pattern) > maxRuleURLPatternLength {
		return "", "", fmt.Errorf("rule URL pattern exceeds %d bytes", maxRuleURLPatternLength)
	}
	for _, char := range pattern {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return "", "", errors.New("rule URL pattern cannot contain whitespace or control characters")
		}
	}
	return method, pattern, nil
}

func isHTTPTokenRune(char rune) bool {
	if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-.^_`|~", char)
}

func validateUUID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != strings.ToLower(value) {
		return fmt.Errorf("%q is not a canonical UUID", value)
	}
	return nil
}

func requireAffected(result sql.Result, kind, id string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s update count: %w", kind, err)
	}
	if count != 1 {
		return fmt.Errorf("%s %q not found", kind, id)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
