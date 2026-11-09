package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"comeet/server/migrations"
	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = initialize(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func initialize(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)"} {
		if _, e := db.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	entries, e := migrations.Files.ReadDir(".")
	if e != nil {
		return e
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, f := range entries {
		if filepath.Ext(f.Name()) != ".sql" {
			continue
		}
		var count int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=?", f.Name()).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			continue
		}
		body, e := migrations.Files.ReadFile(f.Name())
		if e != nil {
			return e
		}
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, string(body)); e == nil {
			_, e = tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES (?,?)", f.Name(), time.Now().UnixMilli())
		}
		if e != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f.Name(), e)
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}
