package browserflow

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqldb, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return sqldb
}

func TestSlugOf(t *testing.T) {
	slug, err := SlugOf("jun01032026-a3f9@my.com")
	if err != nil || slug != "jun01032026-a3f9" {
		t.Fatalf("got %q,%v", slug, err)
	}
	for _, bad := range []string{"", "no-at-sign", "@nodomain", "user@"} {
		if _, err := SlugOf(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDOB20Y(t *testing.T) {
	got := DOB20Y(time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC))
	if got != "2006-09-27" {
		t.Fatalf("got %q want 2006-09-27", got)
	}
}

func TestParseCaptchaStrategy(t *testing.T) {
	for in, want := range map[string]CaptchaStrategy{
		"":                 CaptchaPause,
		"pause-manual":     CaptchaPause,
		"abort-quarantine": CaptchaAbort,
		"backoff-retry":    CaptchaRetry,
	} {
		got, err := ParseCaptchaStrategy(in)
		if err != nil || got != want {
			t.Fatalf("input %q: got %q,%v", in, got, err)
		}
	}
	if _, err := ParseCaptchaStrategy("solve-it"); err == nil {
		t.Fatal("solver strategy accepted")
	}
}

func TestScrubPAN(t *testing.T) {
	if !ScrubPAN("step signup ok ms=120") {
		t.Fatal("clean line flagged")
	}
	if ScrubPAN("card 4111111111111111 saved") {
		t.Fatal("13+ digit run not flagged")
	}
	if !ScrubPAN("code 482917 ok") {
		t.Fatal("6-digit OTP flagged (only 13+ must flag)")
	}
}

func TestAccountRoundTrip(t *testing.T) {
	ctx := context.Background()
	sqldb := openTestDB(t)
	key := func() string { return "test-passphrase-123" }
	now := func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	// requests row must exist for the FK.
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES('req-1','u@x.com','x',1,'custom','pending','t','t')`); err != nil {
		t.Fatal(err)
	}
	if err := AccountSecrets(ctx, sqldb, key, now, "req-1", "u", "Pw!1234567890123456", "2006-09-27", "US", false); err != nil {
		t.Fatal(err)
	}
	pw, dob, country, otpUsed, err := DecryptAccount(ctx, sqldb, key, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if pw != "Pw!1234567890123456" || dob != "2006-09-27" || country != "US" || otpUsed {
		t.Fatalf("mismatch: %q %q %q %v", pw, dob, country, otpUsed)
	}
	// Wrong key must fail, never return plaintext.
	if _, _, _, _, err := DecryptAccount(ctx, sqldb, func() string { return "wrong" }, "req-1"); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestLogStepPersists(t *testing.T) {
	ctx := context.Background()
	sqldb := openTestDB(t)
	now := func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES('req-2','v@x.com','x',1,'custom','pending','t','t')`); err != nil {
		t.Fatal(err)
	}
	if err := LogStep(ctx, sqldb, now, "req-2", "signup-password", 120, true, "submitted"); err != nil {
		t.Fatal(err)
	}
	var step string
	var ok int
	if err := sqldb.QueryRowContext(ctx,
		`SELECT step,ok FROM request_steps WHERE request_id='req-2'`).Scan(&step, &ok); err != nil {
		t.Fatal(err)
	}
	if step != "signup-password" || ok != 1 {
		t.Fatalf("got %q,%d", step, ok)
	}
}

func TestOTPUsedFlag(t *testing.T) {
	ctx := context.Background()
	sqldb := openTestDB(t)
	key := func() string { return "test-passphrase-123" }
	now := func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES('req-3','w@x.com','x',1,'custom','pending','t','t')`); err != nil {
		t.Fatal(err)
	}
	if err := AccountSecrets(ctx, sqldb, key, now, "req-3", "w", "Pw!1234567890123456", "2006-09-27", "US", true); err != nil {
		t.Fatal(err)
	}
	_, _, _, otpUsed, err := DecryptAccount(ctx, sqldb, key, "req-3")
	if err != nil || !otpUsed {
		t.Fatalf("want otp_used=true, got %v,%v", otpUsed, err)
	}
}

func TestCountryNormalization(t *testing.T) {
	// Country flows through the envelope untouched (validated at CLI).
	ctx := context.Background()
	sqldb := openTestDB(t)
	key := func() string { return "test-passphrase-123" }
	now := func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) }
	if _, err := sqldb.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES('req-4','z@x.com','x',1,'custom','pending','t','t')`); err != nil {
		t.Fatal(err)
	}
	if err := AccountSecrets(ctx, sqldb, key, now, "req-4", "z", "Pw!1234567890123456", "2006-09-27", "GB", false); err != nil {
		t.Fatal(err)
	}
	_, _, country, _, err := DecryptAccount(ctx, sqldb, key, "req-4")
	if err != nil || country != "GB" {
		t.Fatalf("want GB, got %q,%v", country, err)
	}
	if strings.ToUpper("us") != "US" {
		t.Fatal("sanity")
	}
}
