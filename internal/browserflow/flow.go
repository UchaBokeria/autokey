package browserflow

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/custom"
	"github.com/uchabokeria/autokey/internal/provider"
)

// CaptchaStrategy selects blocker recovery behavior. No solvers, ever.
type CaptchaStrategy string

const (
	// CaptchaPause holds the job for manual solve (default).
	CaptchaPause CaptchaStrategy = "pause-manual"
	// CaptchaAbort screenshots, marks blocked-captcha, releases the card.
	CaptchaAbort CaptchaStrategy = "abort-quarantine"
	// CaptchaRetry reruns with a fresh profile/IP, capped attempts.
	CaptchaRetry CaptchaStrategy = "backoff-retry"
)

// ParseCaptchaStrategy validates user input from flag/config/setup.
func ParseCaptchaStrategy(s string) (CaptchaStrategy, error) {
	switch CaptchaStrategy(strings.TrimSpace(s)) {
	case "", CaptchaPause:
		return CaptchaPause, nil
	case CaptchaAbort:
		return CaptchaAbort, nil
	case CaptchaRetry:
		return CaptchaRetry, nil
	default:
		return "", fmt.Errorf("unknown captcha strategy %q (pause-manual|abort-quarantine|backoff-retry)", s)
	}
}

// Profile is one service automation target (omegameta first).
type Profile struct {
	Name    string
	BaseURL string
}

// Step is one persisted, resumable unit of a run.
type Step struct {
	Name      string
	Run       func(ctx context.Context, r *Run) error
	Skippable func(ctx context.Context, r *Run) (bool, error)
}

// Run carries per-request automation state. Secrets live in memory only.
type Run struct {
	RequestID string
	Email     string
	Slug      string
	Card      provider.Secrets
	CardLast4 string
	// CardName is the pool card's NameOnCard (billing cardholder).
	CardName string
	Qty      int
	Profile  Profile
	// Collected accumulates generated keys in order.
	Collected []string

	Password string // random per account, persisted encrypted
	DOB      string // today minus 20y, YYYY-MM-DD
	Country  string // billing country, default US

	DB        *sql.DB
	CustomKey func() string
	Now       func() time.Time

	OTPUsed bool
	Log     func(step string, ms int64, ok bool, detail string)
}

// SlugOf returns the local-part link key for an email. The caller must
// have asserted it matches the request email (pitfall #1).
func SlugOf(email string) (string, error) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(email)), "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("browserflow: bad email %q", email)
	}
	return parts[0], nil
}

// RandomPassword generates a 20-char upper/lower/digit/symbol secret.
func RandomPassword() (string, error) {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%&*"
	out := make([]byte, 20)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", fmt.Errorf("browserflow: rand: %w", err)
		}
		out[i] = chars[n.Int64()]
	}
	return string(out), nil
}

// DOB20Y returns today minus 20 years as YYYY-MM-DD (injectable now).
func DOB20Y(now time.Time) string {
	d := now.UTC().AddDate(-20, 0, 0)
	return d.Format("2006-01-02")
}

// AccountSecrets persists the per-request password encrypted with the
// custom envelope + CUSTOM_CARD_KEY. DecryptAccount mirrors cards show.
func AccountSecrets(ctx context.Context, sqldb *sql.DB, key func() string, now func() time.Time, requestID, slug, password, dob, country string, otpUsed bool) error {
	pass := ""
	if key != nil {
		pass = key()
	}
	blob, salt, iters, err := custom.EncryptBlob(pass, password, dob, country)
	if err != nil {
		return err
	}
	ts := now().UTC().Format(time.RFC3339)
	otp := 0
	if otpUsed {
		otp = 1
	}
	_, err = sqldb.ExecContext(ctx, `
INSERT INTO request_accounts(request_id,slug,password_enc,salt,iterations,dob,country,otp_used,created_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(request_id) DO UPDATE SET
  slug=excluded.slug, password_enc=excluded.password_enc, salt=excluded.salt,
  iterations=excluded.iterations, dob=excluded.dob, country=excluded.country,
  otp_used=excluded.otp_used`,
		requestID, slug, blob, salt, iters, dob, country, otp, ts)
	return err
}

// DecryptAccount opens a request's stored password bundle for debugging.
func DecryptAccount(ctx context.Context, sqldb *sql.DB, key func() string, requestID string) (password, dob, country string, otpUsed bool, err error) {
	var blob, salt, dobOut, countryOut string
	var iters, otp int
	if err := sqldb.QueryRowContext(ctx,
		`SELECT password_enc,salt,iterations,dob,country,otp_used FROM request_accounts WHERE request_id=?`,
		requestID).Scan(&blob, &salt, &iters, &dobOut, &countryOut, &otp); err != nil {
		return "", "", "", false, fmt.Errorf("browserflow: no account for %s", requestID)
	}
	pass := ""
	if key != nil {
		pass = key()
	}
	parts, err := custom.DecryptBlob(pass, blob, salt, iters)
	if err != nil {
		return "", "", "", false, err
	}
	if len(parts) != 3 {
		return "", "", "", false, fmt.Errorf("browserflow: corrupt account bundle")
	}
	return parts[0], parts[1], parts[2], otp != 0, nil
}

// LogStep persists one structured step row (dashboard-visible) and
// returns the elapsed writer for the orchestrator log callback.
func LogStep(ctx context.Context, sqldb *sql.DB, now func() time.Time, requestID, step string, ms int64, ok bool, detail string) error {
	okInt := 0
	if ok {
		okInt = 1
	}
	_, err := sqldb.ExecContext(ctx, `
INSERT INTO request_steps(request_id,step,ms,ok,detail,created_at)
VALUES(?,?,?,?,?,?)`, requestID, step, ms, okInt, detail, now().UTC().Format(time.RFC3339))
	return err
}

// ScrubPAN asserts no 13–19 digit runs leak into log detail lines (test).
func ScrubPAN(detail string) bool {
	run := 0
	for _, r := range detail {
		if r >= '0' && r <= '9' {
			run++
			if run >= 13 {
				return false
			}
		} else {
			run = 0
		}
	}
	return true
}

// safeDetail replaces 13+ digit runs with [redacted-pan] before a
// string reaches logs. Shorter runs (OTPs, amounts, years) pass through.
func safeDetail(s string) string {
	var b strings.Builder
	var digits []rune
	run := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			run++
			digits = append(digits, r)
			continue
		}
		if run >= 13 {
			b.WriteString("[redacted-pan]")
		} else {
			b.WriteString(string(digits))
		}
		run = 0
		digits = digits[:0]
		b.WriteRune(r)
	}
	if run >= 13 {
		b.WriteString("[redacted-pan]")
	} else {
		b.WriteString(string(digits))
	}
	return b.String()
}
