package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the SQLite database at path, enables WAL, and runs migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	sqldb.SetMaxOpenConns(1) // single writer (spec §16)
	if _, err := sqldb.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if err := migrate(ctx, sqldb); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return sqldb, nil
}

func migrate(ctx context.Context, sqldb *sql.DB) error {
	raw, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	if _, err := sqldb.ExecContext(ctx, string(raw)); err != nil {
		return fmt.Errorf("apply migration 001: %w", err)
	}
	_, _ = sqldb.ExecContext(ctx,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`,
		time.Now().UTC().Format(time.RFC3339))
	return nil
}
