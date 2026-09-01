package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
	_ "modernc.org/sqlite"
)

const databaseFileName = "flowlens.db"

func Path() (string, error) {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, databaseFileName), nil
}

func Open() (*sql.DB, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

func OpenAt(path string) (_ *sql.DB, err error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite database path: %w", err)
	}
	if err := fs.EnsurePrivateDir(filepath.Dir(absPath)); err != nil {
		return nil, fmt.Errorf("create sqlite database directory: %w", err)
	}

	dsn := databaseDSN(absPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx := context.Background()
	if err = db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if chmodErr := fs.EnsurePrivateFile(absPath); chmodErr != nil {
		return nil, fmt.Errorf("set sqlite database permissions: %w", chmodErr)
	}
	if err = quickCheck(ctx, db); err != nil {
		return nil, err
	}
	if err = createCurrentSchema(ctx, db); err != nil {
		return nil, err
	}
	if err = quickCheck(ctx, db); err != nil {
		return nil, err
	}
	return db, nil
}

func databaseDSN(path string) string {
	query := make(url.Values)
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(DELETE)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Set("_txlock", "immediate")
	return filepath.ToSlash(path) + "?" + query.Encode()
}

func quickCheck(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return fmt.Errorf("run sqlite quick_check: %w", err)
	}
	defer rows.Close()

	results := make([]string, 0, 1)
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("read sqlite quick_check result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite quick_check results: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("sqlite quick_check failed: %v", results)
	}
	return nil
}
