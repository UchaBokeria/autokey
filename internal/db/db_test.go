package db

import (
	"context"
	"testing"
)

// Fresh DBs must land on the latest schema with provider columns.
func TestMigrationsFresh(t *testing.T) {
	sqldb, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	var v int
	if err := sqldb.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 3 {
		t.Fatalf("want schema version 3, got %d", v)
	}
	for _, col := range [][2]string{{"cards", "provider"}, {"requests", "provider"}} {
		var n int
		if err := sqldb.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='provider'`, col[0]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("want provider column on %s", col[0])
		}
	}
	var cc int
	if err := sqldb.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='custom_cards'`).Scan(&cc); err != nil {
		t.Fatal(err)
	}
	if cc != 1 {
		t.Fatal("want custom_cards table")
	}
}

// Legacy v1 DBs (no provider columns) must migrate cleanly.
func TestMigrationsUpgradeV1(t *testing.T) {
	ctx := context.Background()
	sqldb, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a v1 database: drop provider columns, reset version.
	for _, q := range []string{
		`DELETE FROM schema_migrations WHERE version>=2`,
		`ALTER TABLE cards DROP COLUMN provider`,
		`ALTER TABLE requests DROP COLUMN provider`,
	} {
		if _, err := sqldb.ExecContext(ctx, q); err != nil {
			t.Skipf("sqlite too old for DROP COLUMN: %v", err)
		}
	}
	_ = sqldb.Close()
	// Reopen is not possible on :memory:, so emulate by running migrate
	// against a temp file DB instead.
	dir := t.TempDir()
	path := dir + "/up.db"
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DELETE FROM schema_migrations WHERE version>=2`,
		`INSERT INTO cards(card_id,last4,bin,name_on_card,status,claimed,created_at)
		 VALUES('chkr_old','','','','active',0,'t')`,
		`INSERT INTO cards(card_id,last4,bin,name_on_card,status,claimed,created_at)
		 VALUES('MR_old','1111','539502','a','active',0,'t')`,
		`ALTER TABLE cards DROP COLUMN provider`,
		`ALTER TABLE requests DROP COLUMN provider`,
	} {
		if _, err := first.ExecContext(ctx, q); err != nil {
			t.Skipf("sqlite too old for DROP COLUMN: %v", err)
		}
	}
	_ = first.Close()
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var kripi, onramp string
	_ = second.QueryRow(`SELECT provider FROM cards WHERE card_id='MR_old'`).Scan(&kripi)
	_ = second.QueryRow(`SELECT provider FROM cards WHERE card_id='chkr_old'`).Scan(&onramp)
	if kripi != "kripi" || onramp != "onramp" {
		t.Fatalf("want backfill kripi/onramp, got %q/%q", kripi, onramp)
	}
}
