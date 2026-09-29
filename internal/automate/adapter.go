package automate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uchabokeria/autokey/internal/browser"
	"github.com/uchabokeria/autokey/internal/browserflow"
	"github.com/uchabokeria/autokey/internal/mail"
	"github.com/uchabokeria/autokey/internal/provider"
	"github.com/uchabokeria/autokey/internal/services/omegameta"
)

// Adapter implements flow.KeyProducer against request-scoped runs.
// Email is unique per request, so reqID resolves via lookup — the
// flow.KeyProducer interface stays untouched.
type Adapter struct {
	DB      *sql.DB
	Driver  browser.Driver
	BaseURL string
	Proxy   string
	Captcha browserflow.CaptchaStrategy
	// OTPTimeout for the hardened WaitForOTP path.
	OTPTimeout time.Duration
	Key        func() string
	Now        func() time.Time
	Log        func(requestID, step string, ms int64, ok bool, detail string)
	RunDir     func(requestID string) string
	ProfileDir func(requestID string) string
	// MailWait allows tests to stub OTP retrieval.
	MailWait func(ctx context.Context, recipient string, timeout time.Duration) (mail.OTPResult, error)
}

func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// ctxKey carries per-request overrides through context (hook body fields).
// Construction fields remain the defaults; flags/body beat config.
type ctxKey string

const (
	ctxProxyKey   ctxKey = "browserflow.proxy"
	ctxCaptchaKey ctxKey = "browserflow.captcha"
)

// WithOverrides returns a ctx carrying per-request proxy/captcha.
func WithOverrides(ctx context.Context, proxy, captcha string) context.Context {
	if proxy != "" {
		ctx = context.WithValue(ctx, ctxProxyKey, proxy)
	}
	if captcha != "" {
		ctx = context.WithValue(ctx, ctxCaptchaKey, captcha)
	}
	return ctx
}

func proxyOf(ctx context.Context, fallback string) string {
	if v, ok := ctx.Value(ctxProxyKey).(string); ok && v != "" {
		return v
	}
	return fallback
}

func captchaOf(ctx context.Context, fallback browserflow.CaptchaStrategy) browserflow.CaptchaStrategy {
	if v, ok := ctx.Value(ctxCaptchaKey).(string); ok && v != "" {
		if s, err := browserflow.ParseCaptchaStrategy(v); err == nil {
			return s
		}
	}
	return fallback
}

func (a *Adapter) mailWait(ctx context.Context, recipient string, timeout time.Duration) (mail.OTPResult, error) {
	if a.MailWait != nil {
		return a.MailWait(ctx, recipient, timeout)
	}
	return mail.WaitForOTP(ctx, a.DB, recipient, timeout)
}

// Generate resolves the request by email, then runs the profile.
func (a *Adapter) Generate(ctx context.Context, email string, card provider.Secrets, qty int) ([]string, error) {
	var reqID, cardID, cardLast4, cardName string
	err := a.DB.QueryRowContext(ctx,
		`SELECT r.id,COALESCE(r.card_id,''),COALESCE(c.last4,''),COALESCE(c.name_on_card,'')
		 FROM requests r LEFT JOIN cards c ON c.card_id=r.card_id
		 WHERE r.email=? ORDER BY r.created_at DESC LIMIT 1`,
		email).Scan(&reqID, &cardID, &cardLast4, &cardName)
	if err != nil {
		return nil, fmt.Errorf("automate: resolve request for %s: %w", email, err)
	}
	r := &browserflow.Run{
		RequestID: reqID,
		Email:     email,
		Card:      card,
		CardLast4: cardLast4,
		CardName:  cardName,
		Qty:       qty,
		DB:        a.DB,
		CustomKey: a.Key,
		Now:       a.now,
		Log:       func(step string, ms int64, ok bool, detail string) {},
	}
	_ = cardID
	return a.GenerateFor(ctx, r)
}

// GenerateFor runs the omegameta profile for a prepared Run.
func (a *Adapter) GenerateFor(ctx context.Context, r *browserflow.Run) ([]string, error) {
	otpTimeout := a.OTPTimeout
	if otpTimeout <= 0 {
		otpTimeout = 5 * time.Minute
	}
	cfg := omegameta.Config{
		BaseURL:    a.BaseURL,
		DOBMode:    omegameta.DOBSelects,
		OTPTimeout: otpTimeout,
		Open: func(ctx context.Context, r *browserflow.Run) (browser.Session, error) {
			profileDir := ""
			if a.ProfileDir != nil {
				profileDir = a.ProfileDir(r.RequestID)
			}
			runDir := ""
			if a.RunDir != nil {
				runDir = a.RunDir(r.RequestID)
			}
			opts := browser.Options{
				ProfileDir: profileDir,
				Headless:   true,
				ProxyURL:   proxyOf(ctx, a.Proxy),
				RunDir:     runDir,
			}
			opts.Defaults()
			return a.Driver.Launch(ctx, opts)
		},
		Mail: func(ctx context.Context, r *browserflow.Run) (string, error) {
			res, err := a.mailWait(ctx, r.Email, otpTimeout)
			if err != nil {
				return "", err
			}
			return res.Code, nil
		},
	}
	// Country comes from the card record (custom_cards.country).
	if c, err := countryOf(ctx, a.DB, r); err == nil && c != "" {
		r.Country = c
	}
	runner := &browserflow.Runner{
		DB:      a.DB,
		Driver:  a.Driver,
		Now:     a.now,
		Log:     a.Log,
		Captcha: captchaOf(ctx, a.Captcha),
	}
	defs := make([]browserflow.StepDef, 0, len(omegameta.Steps(r, cfg)))
	for _, st := range omegameta.Steps(r, cfg) {
		st := st
		defs = append(defs, browserflow.StepDef{
			Name: st.Name,
			Run: func(ctx context.Context, r *browserflow.Run, _ browser.Session) error {
				return st.Run(ctx, r)
			},
		})
	}
	// Prepare persists the encrypted account row first.
	if err := omegameta.Prepare(ctx, r, cfg); err != nil {
		return nil, err
	}
	if err := browserflow.AccountSecrets(ctx, a.DB, a.Key, a.now,
		r.RequestID, r.Slug, r.Password, r.DOB, r.Country, false); err != nil {
		return nil, err
	}
	// Runner handles resume-skip, per-step logs, and captcha dispatch.
	// Profile steps open their own sessions; shared sess is nil.
	if err := runner.RunSteps(ctx, r, defs, nil); err != nil {
		return nil, err
	}
	// Persist OTP usage once known.
	if r.OTPUsed {
		_, _ = a.DB.ExecContext(ctx,
			`UPDATE request_accounts SET otp_used=1 WHERE request_id=?`, r.RequestID)
	}
	return r.Collected, nil
}

func countryOf(ctx context.Context, sqldb *sql.DB, r *browserflow.Run) (string, error) {
	var cardID string
	if err := sqldb.QueryRowContext(ctx,
		`SELECT card_id FROM requests WHERE id=?`, r.RequestID).Scan(&cardID); err != nil {
		return "", err
	}
	var country string
	if err := sqldb.QueryRowContext(ctx,
		`SELECT country FROM custom_cards WHERE card_id=?`, cardID).Scan(&country); err != nil {
		return "", err
	}
	return country, nil
}
