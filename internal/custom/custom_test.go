package custom

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/uchabokeria/autokey/internal/provider"
	_ "modernc.org/sqlite"
)

func testProvider(t *testing.T, db *sql.DB) *Provider {
	t.Helper()
	return &Provider{
		DB:     db,
		Key:    func() string { return "test-passphrase-123" },
		Now:    func() string { return "2026-09-26T00:00:00Z" },
		CardID: func(label string) string { return "custom_test_" + strings.ReplaceAll(label, " ", "_") },
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE cards(card_id TEXT PRIMARY KEY,last4 TEXT NOT NULL,bin TEXT NOT NULL,
		  name_on_card TEXT NOT NULL,provider TEXT NOT NULL DEFAULT '',
		  status TEXT NOT NULL DEFAULT 'active',claimed INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL)`,
		`CREATE TABLE custom_cards(card_id TEXT PRIMARY KEY,enc_blob TEXT NOT NULL,
		  salt TEXT NOT NULL,iterations INTEGER NOT NULL,country TEXT NOT NULL DEFAULT 'US',created_at TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestValidate(t *testing.T) {
	good := CardInput{Label: "mine", Number: "4111 1111 1111 1111", Expiry: "12/28", CVV: "123"}
	if err := Validate(good); err != nil {
		t.Fatalf("good card rejected: %v", err)
	}
	bad := []CardInput{
		{Label: "x", Number: "123", Expiry: "12/28", CVV: "123"},              // too short
		{Label: "x", Number: "4111111111111112", Expiry: "12/28", CVV: "123"}, // Luhn fail
		{Label: "x", Number: "4111111111111111", Expiry: "13/28", CVV: "123"}, // bad month
		{Label: "x", Number: "4111111111111111", Expiry: "12/28", CVV: "12"},  // bad cvv
		{Label: "", Number: "4111111111111111", Expiry: "12/28", CVV: "123"},  // no label
	}
	for i, in := range bad {
		if err := Validate(in); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestDetectBrand(t *testing.T) {
	cases := map[Brand][]string{
		BrandVisa:     {"4222222222222", "4111111111111111", "4111111111111111110"},
		BrandMaster:   {"5555555555554444", "2221000000000009", "2720990000000007"},
		BrandAmex:     {"378282246310005", "371449635398431"},
		BrandDiscover: {"6011111111111117", "6500000000000002", "6440000000000005"},
		BrandDiners:   {"3056930009020004", "36111111111111"},
		BrandJCB:      {"3530111333300000"},
		BrandUnknown:  {"9000000000000001"},
	}
	for want, pans := range cases {
		for _, pan := range pans {
			if got := DetectBrand(pan); got != want {
				t.Fatalf("DetectBrand(%s) = %s, want %s", pan, got, want)
			}
		}
	}
}

func TestValidateBrands(t *testing.T) {
	// Luhn-verified vectors per network (docs test PANs + derived).
	good := []CardInput{
		{Label: "v13", Number: "4222222222222", Expiry: "12/28", CVV: "123"},
		{Label: "v16", Number: "4111111111111111", Expiry: "12/28", CVV: "123"},
		{Label: "v19", Number: "4111111111111111110", Expiry: "12/28", CVV: "123"},
		{Label: "mc51", Number: "5555555555554444", Expiry: "12/28", CVV: "123"},
		{Label: "mc2221", Number: "2221000000000009", Expiry: "12/28", CVV: "123"},
		{Label: "mc2720", Number: "2720990000000007", Expiry: "12/28", CVV: "123"},
		{Label: "amex", Number: "378282246310005", Expiry: "11/29", CVV: "1234"},
		{Label: "amex2", Number: "371449635398431", Expiry: "01/30", CVV: "5678"},
		{Label: "disc6011", Number: "6011111111111117", Expiry: "12/28", CVV: "123"},
		{Label: "disc65", Number: "6500000000000002", Expiry: "12/28", CVV: "123"},
		{Label: "disc644", Number: "6440000000000005", Expiry: "12/28", CVV: "123"},
		{Label: "diners", Number: "3056930009020004", Expiry: "12/28", CVV: "123"},
		{Label: "diners36", Number: "36111111111111", Expiry: "12/28", CVV: "123"},
		{Label: "jcb", Number: "3530111333300000", Expiry: "12/28", CVV: "123"},
	}
	for _, in := range good {
		if err := Validate(in); err != nil {
			t.Fatalf("%s rejected: %v", in.Label, err)
		}
	}
	bad := []CardInput{
		{Label: "amex16", Number: "3782822463100051", Expiry: "11/29", CVV: "1234"},   // amex must be 15
		{Label: "amex3", Number: "378282246310005", Expiry: "11/29", CVV: "123"},      // amex needs 4-digit CID
		{Label: "mc15", Number: "555555555555444", Expiry: "12/28", CVV: "123"},       // mc must be 16
		{Label: "diners16", Number: "30569300090200043", Expiry: "12/28", CVV: "123"}, // diners is 14-only
		{Label: "visa12", Number: "422222222222", Expiry: "12/28", CVV: "123"},        // too short
	}
	for _, in := range bad {
		if err := Validate(in); err == nil {
			t.Fatalf("%s accepted", in.Label)
		}
	}
}

func TestNormalizeExpiry(t *testing.T) {
	cases := map[string]string{
		"12/28": "12/28", "12-28": "12/28", "12.28": "12/28",
		"1228": "12/28", "12/2028": "12/28", "122028": "12/28",
		" 01/30 ": "01/30",
	}
	for in, want := range cases {
		got, err := normalizeExpiry(in)
		if err != nil || got != want {
			t.Fatalf("normalizeExpiry(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "13/28", "122", "12/2", "ab/cd", "00/28", "12/280"} {
		if _, err := normalizeExpiry(in); err == nil {
			t.Fatalf("normalizeExpiry(%q) accepted", in)
		}
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	blob, salt, iters, err := EncryptSecrets("pw", "4111111111111111", "12/28", "123")
	if err != nil {
		t.Fatal(err)
	}
	s, err := DecryptSecrets("pw", blob, salt, iters)
	if err != nil {
		t.Fatal(err)
	}
	if s.Number != "4111111111111111" || s.Expiry != "12/28" || s.CVV != "123" {
		t.Fatalf("round trip mismatch: %+v", s)
	}
	// Wrong key must fail.
	if _, err := DecryptSecrets("wrong", blob, salt, iters); err == nil {
		t.Fatal("wrong key accepted")
	}
	// Empty passphrase must fail.
	if _, _, _, err := EncryptSecrets("", "4111111111111111", "12/28", "123"); err == nil {
		t.Fatal("empty passphrase accepted")
	}
}

func TestAddAndSecrets(t *testing.T) {
	ctx := context.Background()
	p := testProvider(t, openTestDB(t))
	ref, err := p.Add(ctx, CardInput{
		Label: "mine", Number: "4111-1111-1111-1111", Expiry: "12/28", CVV: "123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "custom_test_mine" || ref.Last4 != "1111" || ref.BIN != "411111" {
		t.Fatalf("bad ref: %+v", ref)
	}
	// PAN must not be stored anywhere in plaintext.
	var n int
	if err := p.DB.QueryRow(`SELECT COUNT(*) FROM custom_cards WHERE enc_blob LIKE '%4111%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("PAN leaked into blob column")
	}
	s, err := p.Secrets(ctx, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Number != "4111111111111111" || s.Expiry != "12/28" || s.CVV != "123" {
		t.Fatalf("bad secrets: %+v", s)
	}
	// Unknown id.
	if _, err := p.Secrets(ctx, "custom_nope"); err == nil {
		t.Fatal("unknown id accepted")
	}
	// Quarantine.
	if err := p.Quarantine(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = p.DB.QueryRow(`SELECT status FROM cards WHERE card_id=?`, ref.ID).Scan(&status)
	if status != "quarantined" {
		t.Fatalf("want quarantined, got %q", status)
	}
}

func TestMintRejectsNonRetryable(t *testing.T) {
	p := testProvider(t, openTestDB(t))
	// Bad Luhn via Mint path.
	_, err := p.Mint(context.Background(), provider.MintParams{
		Name: "x", Product: "4111111111111112|12/28|123",
	})
	if err == nil {
		t.Fatal("bad mint accepted")
	}
	if pe := p.Classify(err); pe == nil || pe.Retryable || pe.Provider != "custom" {
		t.Fatalf("want non-retryable custom error, got %+v", pe)
	}
	// Good mint via Mint path.
	ref, err := p.Mint(context.Background(), provider.MintParams{
		Name: "y", Product: "4111111111111111|12/28|123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID == "" {
		t.Fatal("empty ref id")
	}
}

func TestName(t *testing.T) {
	p := testProvider(t, openTestDB(t))
	if p.Name() != "custom" {
		t.Fatalf("want custom, got %q", p.Name())
	}
}
