package omegameta

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/browser"
	"github.com/uchabokeria/autokey/internal/browserflow"
	"github.com/uchabokeria/autokey/internal/mail"
)

// Name is the service profile id. The display/backend domain lives in
// config (services.omegameta.base_url), never here.
const Name = "omegameta"

// DOBMode selects the date-of-birth widget variant.
type DOBMode string

const (
	// DOBSelects tries three <select> elements first (day/month/year).
	DOBSelects DOBMode = "selects"
	// DOBInput tries a single text/date input.
	DOBInput DOBMode = "input"
)

// L lacin the label-first locator strategy: ordered fallbacks per
// element. IDs/classes are last resort (they churn fastest).
func L(kind, value string) browser.Locator { return browser.Locator{Kind: kind, Value: value} }

// Steps returns the profile step list for one run. Steps are
// idempotent-by-check: resume skips completed work.
func Steps(r *browserflow.Run, cfg Config) []browserflow.Step {
	return []browserflow.Step{
		{Name: "prepare", Run: func(ctx context.Context, r *browserflow.Run) error {
			return Prepare(ctx, r, cfg)
		}},
		{Name: "signup-password", Run: func(ctx context.Context, r *browserflow.Run) error {
			return SignupPassword(ctx, r, cfg)
		}},
		{Name: "signup-otp-fallback", Run: func(ctx context.Context, r *browserflow.Run) error {
			return SignupOTP(ctx, r, cfg)
		}},
		{Name: "billing", Run: func(ctx context.Context, r *browserflow.Run) error {
			return Billing(ctx, r, cfg)
		}},
		{Name: "keys", Run: func(ctx context.Context, r *browserflow.Run) error {
			return Keys(ctx, r, cfg)
		}},
	}
}

// Config carries per-run profile inputs (domain from service config).
type Config struct {
	BaseURL    string
	DOBMode    DOBMode
	OTPTimeout time.Duration
	// Sess abstracts the live page for testability; production wires
	// the real browser.Session. Kept minimal on purpose.
	Open func(ctx context.Context, r *browserflow.Run) (browser.Session, error)
	Mail func(ctx context.Context, r *browserflow.Run) (string, error)
}

// Prepare asserts slug↔email linkage (pitfall #1), mints password+DOB,
// and persists the encrypted account row.
func Prepare(ctx context.Context, r *browserflow.Run, cfg Config) error {
	slug, err := browserflow.SlugOf(r.Email)
	if err != nil {
		return err
	}
	if r.Slug != "" && r.Slug != slug {
		return fmt.Errorf("omegameta: slug %q != email local-part %q", r.Slug, slug)
	}
	r.Slug = slug
	pw, err := browserflow.RandomPassword()
	if err != nil {
		return err
	}
	r.Password = pw
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	r.DOB = browserflow.DOB20Y(now())
	if r.Country == "" {
		r.Country = "US"
	}
	_ = cfg
	return nil
}

// SignupPassword is the default path: email → continue → password →
// remember-me → submit.
func SignupPassword(ctx context.Context, r *browserflow.Run, cfg Config) error {
	sess, err := cfg.Open(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	base := strings.TrimRight(cfg.BaseURL, "/")
	if err := sess.Goto(ctx, base+"/signup"); err != nil {
		return err
	}
	if err := sess.Fill(ctx, []browser.Locator{
		L("label", "Email"), L("text", "Email"),
		L("css", "input[type=email]"), L("css", "input[name=email]"),
	}, r.Email); err != nil {
		return fmt.Errorf("omegameta: email field: %w", err)
	}
	if err := sess.Click(ctx, []browser.Locator{
		L("role", "button[Continue]"), L("text", "Continue"),
		L("text", "Sign up"), L("css", "button[type=submit]"),
	}); err != nil {
		return fmt.Errorf("omegameta: continue: %w", err)
	}
	if err := sess.Fill(ctx, []browser.Locator{
		L("label", "Password"), L("text", "Password"),
		L("css", "input[type=password]"),
	}, r.Password); err != nil {
		return fmt.Errorf("omegameta: password field: %w", err)
	}
	// Confirm field only if present (some forms have one).
	if ok, _ := sess.Exists(ctx, []browser.Locator{
		L("label", "Confirm password"), L("text", "Confirm"),
	}); ok {
		if err := sess.Fill(ctx, []browser.Locator{
			L("label", "Confirm password"), L("text", "Confirm"),
		}, r.Password); err != nil {
			return fmt.Errorf("omegameta: confirm password: %w", err)
		}
	}
	// Remember-me tick if present (optional, never fatal).
	_ = sess.Tick(ctx, []browser.Locator{
		L("label", "Remember me"), L("text", "Remember me"),
		L("text", "Save login info"),
	})
	// DOB selects-first, input fallback.
	if err := fillDOB(ctx, sess, r.DOB, cfg.DOBMode); err != nil {
		return fmt.Errorf("omegameta: dob: %w", err)
	}
	if err := sess.Click(ctx, []browser.Locator{
		L("role", "button[Create account]"), L("text", "Create account"),
		L("text", "Sign up"), L("css", "button[type=submit]"),
	}); err != nil {
		return fmt.Errorf("omegameta: submit: %w", err)
	}
	return nil
}

func fillDOB(ctx context.Context, sess browser.Session, dob string, mode DOBMode) error {
	parts := strings.Split(dob, "-") // YYYY-MM-DD
	if len(parts) != 3 {
		return fmt.Errorf("bad dob %q", dob)
	}
	trySelects := func() error {
		day, month, year := parts[2], parts[1], parts[0]
		if err := sess.Select(ctx, []browser.Locator{
			L("label", "Day"), L("css", "select[name*=day]"),
		}, strings.TrimLeft(day, "0")); err != nil {
			return err
		}
		if err := sess.Select(ctx, []browser.Locator{
			L("label", "Month"), L("css", "select[name*=month]"),
		}, strings.TrimLeft(month, "0")); err != nil {
			return err
		}
		return sess.Select(ctx, []browser.Locator{
			L("label", "Year"), L("css", "select[name*=year]"),
		}, year)
	}
	tryInput := func() error {
		return sess.Fill(ctx, []browser.Locator{
			L("label", "Date of birth"), L("text", "Birthday"),
			L("css", "input[type=date]"), L("css", "input[name*=birth]"),
		}, dob)
	}
	if mode == DOBInput {
		if err := tryInput(); err == nil {
			return nil
		}
		return trySelects()
	}
	if err := trySelects(); err == nil {
		return nil
	}
	return tryInput()
}

// SignupOTP is the fallback: only when the password path rejects.
// Starts the waiter concurrently with page load (pitfall #2 race).
// Blind-built: backend unfinished, user tests manually.
func SignupOTP(ctx context.Context, r *browserflow.Run, cfg Config) error {
	type otpRes struct {
		code string
		err  error
	}
	ch := make(chan otpRes, 1)
	go func() {
		code, err := cfg.Mail(ctx, r)
		ch <- otpRes{code, err}
	}()
	sess, err := cfg.Open(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	base := strings.TrimRight(cfg.BaseURL, "/")
	if err := sess.Goto(ctx, base+"/signup"); err != nil {
		return err
	}
	if err := sess.Fill(ctx, []browser.Locator{
		L("label", "Email"), L("css", "input[type=email]"),
	}, r.Email); err != nil {
		return fmt.Errorf("omegameta: otp email field: %w", err)
	}
	if err := sess.Click(ctx, []browser.Locator{
		L("role", "button[Continue with code]"), L("text", "Continue with code"),
		L("text", "Use OTP instead"), L("text", "Send code"),
	}); err != nil {
		return fmt.Errorf("omegameta: otp request: %w", err)
	}
	var code string
	select {
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("omegameta: otp wait: %w", res.err)
		}
		code = res.code
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := sess.Fill(ctx, []browser.Locator{
		L("label", "Verification code"), L("text", "Enter code"),
		L("css", "input[name*=otp],input[name*=code]"),
	}, code); err != nil {
		return fmt.Errorf("omegameta: otp fill: %w", err)
	}
	r.OTPUsed = true
	return sess.Click(ctx, []browser.Locator{
		L("role", "button[Verify]"), L("text", "Verify"),
		L("css", "button[type=submit]"),
	})
}

// Billing adds the card: billing page → add method → country → card
// fields → save → wait confirmation on DOM (never fixed sleeps).
func Billing(ctx context.Context, r *browserflow.Run, cfg Config) error {
	sess, err := cfg.Open(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	base := strings.TrimRight(cfg.BaseURL, "/")
	if err := sess.Goto(ctx, base+"/billing"); err != nil {
		return err
	}
	if err := sess.Click(ctx, []browser.Locator{
		L("role", "button[Add payment method]"), L("text", "Add payment method"),
		L("text", "Add card"),
	}); err != nil {
		return fmt.Errorf("omegameta: add method: %w", err)
	}
	if err := sess.Select(ctx, []browser.Locator{
		L("label", "Country"), L("css", "select[name*=country]"),
	}, r.Country); err != nil {
		return fmt.Errorf("omegameta: country: %w", err)
	}
	steps := []struct {
		locs []browser.Locator
		val  string
		what string
	}{
		{[]browser.Locator{
			L("label", "Card number"), L("text", "Card number"),
			L("css", "input[name*=number]"),
		}, r.Card.Number, "pan"},
		{[]browser.Locator{
			L("label", "Expiry"), L("text", "Expiry"),
			L("css", "input[name*=exp]"),
		}, r.Card.Expiry, "expiry"},
		{[]browser.Locator{
			L("label", "CVC"), L("label", "CVV"), L("text", "Security code"),
			L("css", "input[name*=cvc],input[name*=cvv]"),
		}, r.Card.CVV, "cvv"},
		{[]browser.Locator{
			L("label", "Name on card"), L("text", "Cardholder"),
			L("css", "input[name*=name]"),
		}, r.CardName, "name"},
	}
	for _, s := range steps {
		if s.val == "" {
			continue
		}
		if err := sess.Fill(ctx, s.locs, s.val); err != nil {
			return fmt.Errorf("omegameta: card %s: %w", s.what, err)
		}
	}
	if err := sess.Click(ctx, []browser.Locator{
		L("role", "button[Save]"), L("text", "Save"),
		L("css", "button[type=submit]"),
	}); err != nil {
		return fmt.Errorf("omegameta: save card: %w", err)
	}
	// Confirmation token on DOM: saved indicator or cards list entry.
	return sess.WaitFor(ctx, []browser.Locator{
		L("text", "saved"), L("text", "ending in"),
		L("text", "Payment method added"),
	}, 30_000_000_000)
}

// Keys creates qty API keys and returns them in order.
func Keys(ctx context.Context, r *browserflow.Run, cfg Config) error {
	sess, err := cfg.Open(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	base := strings.TrimRight(cfg.BaseURL, "/")
	if err := sess.Goto(ctx, base+"/api-keys"); err != nil {
		return err
	}
	for i := 0; i < r.Qty; i++ {
		if err := sess.Click(ctx, []browser.Locator{
			L("role", "button[Create key]"), L("text", "Create key"),
			L("text", "New key"), L("text", "Generate"),
		}); err != nil {
			return fmt.Errorf("omegameta: create key %d: %w", i+1, err)
		}
		key, err := sess.Text(ctx, []browser.Locator{
			L("label", "API key"), L("text", "sk-"),
			L("css", "code,.api-key,[data-key]"),
		})
		if err != nil || key == "" {
			return fmt.Errorf("omegameta: read key %d: %w", i+1, err)
		}
		r.Collected = append(r.Collected, key)
	}
	return nil
}

// WaitCode is the cfg.Mail implementation over mail.WaitForOTP.
func WaitCode(
	wait func(ctx context.Context, recipient string, timeout time.Duration) (mail.OTPResult, error),
	timeout time.Duration,
) func(ctx context.Context, r *browserflow.Run) (string, error) {
	return func(ctx context.Context, r *browserflow.Run) (string, error) {
		res, err := wait(ctx, r.Email, timeout)
		if err != nil {
			return "", err
		}
		return res.Code, nil
	}
}
