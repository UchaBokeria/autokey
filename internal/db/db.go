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
	ordered := []struct {
		version int
		file    string
	}{
		{1, "migrations/001_init.sql"},
		{2, "migrations/002_provider.sql"},
		{3, "migrations/003_custom.sql"},
	}
	var current int
	// Fresh DBs have no schema_migrations table yet; treat as version 0.
	_ = sqldb.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current)
	for _, m := range ordered {
		if m.version <= current {
			continue
		}
		raw, err := migrations.ReadFile(m.file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.file, err)
		}
		if _, err := sqldb.ExecContext(ctx, string(raw)); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err := sqldb.ExecContext(ctx,
			`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	return nil
}
