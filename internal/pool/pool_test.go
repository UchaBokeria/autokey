package pool

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/db"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	sqldb, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return sqldb
}

func TestEmailFormat(t *testing.T) {
	old := Clock
	Clock = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { Clock = old }()
	sqldb := openTest(t)
	email, err := CreateUniqueEmailUser(context.Background(), sqldb, "My.COM")
	if err != nil {
		t.Fatal(err)
	}
	if len(email) < len("jun01032026-aaaa@my.com") {
		t.Fatalf("bad email %q", email)
	}
	if email[:11] != "mar01032026" {
		t.Fatalf("bad prefix %q", email)
	}
	if email[len(email)-6:] != "my.com" {
		t.Fatalf("domain not lowercased: %q", email)
	}
}

func TestPoolOrderingAndClaim(t *testing.T) {
	ctx := context.Background()
	sqldb := openTest(t)
	for _, c := range [][4]string{
		{"c-good", "1111", "539502", "a"},
		{"c-bad", "2222", "539502", "a"},
	} {
		if err := Register(ctx, sqldb, c[0], c[1], c[2], c[3]); err != nil {
			t.Fatal(err)
		}
	}
	// c-bad fails a lot for service x, c-good succeeds.
	for i := 0; i < 5; i++ {
		_ = RecordResult(ctx, sqldb, "c-bad", "x", false, "declined")
	}
	_ = RecordResult(ctx, sqldb, "c-good", "x", true, "")
	// Same card failing for service y must not affect x ordering.
	_ = RecordResult(ctx, sqldb, "c-good", "y", false, "other-svc")

	cards, err := ListPool(ctx, sqldb, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].ID != "c-good" {
		t.Fatalf("want c-good first, got %+v", cards)
	}

	release, err := Claim(ctx, sqldb, "c-good")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Claim(ctx, sqldb, "c-good"); err == nil {
		t.Fatal("want double-claim to fail")
	}
	// Claimed card hidden from pool.
	cards, _ = ListPool(ctx, sqldb, "x")
	if len(cards) != 1 || cards[0].ID != "c-bad" {
		t.Fatalf("want only c-bad visible, got %+v", cards)
	}
	if err := release(ctx); err != nil {
		t.Fatal(err)
	}
}
