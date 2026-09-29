package browserflow

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/browser"
	"github.com/uchabokeria/autokey/internal/provider"
)

// StepStatus tracks one step's outcome for resume.
type StepStatus struct {
	Name      string
	Done      bool
	OK        bool
	Detail    string
	Millis    int64
	StartedAt string
}

// Runner executes profile steps with resume, logging, and captcha policy.
type Runner struct {
	DB          *sql.DB
	Driver      browser.Driver
	ProfileDir  func(requestID string) string
	RunDir      func(requestID string) string
	ProxyURL    string
	MaxParallel int
	Captcha     CaptchaStrategy
	OTPTimeout  time.Duration
	MailWait    func(ctx context.Context, recipient string, timeout time.Duration) (OTPCode, error)
	Now         func() time.Time
	Log         func(requestID, step string, ms int64, ok bool, detail string)
}

// OTPCode is the mail code plus metadata the profile needs.
type OTPCode struct {
	Code string
}

// StepDef is one idempotent unit of a run.
type StepDef struct {
	Name string
	// Skip reports whether completed work makes this step unnecessary.
	Skip func(ctx context.Context, r *Run) (bool, error)
	Run  func(ctx context.Context, r *Run, sess browser.Session) error
}

// CompletedSteps returns the set of ok steps for resume.
func (rn *Runner) CompletedSteps(ctx context.Context, requestID string) (map[string]bool, error) {
	rows, err := rn.DB.QueryContext(ctx,
		`SELECT step FROM request_steps WHERE request_id=? AND ok=1`, requestID)
	if err != nil {
		return nil, fmt.Errorf("browserflow: completed steps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}

// RunSteps executes steps in order, skipping completed ones (resume).
// Each step is logged to request_steps + the Log callback. Returns the
// first error; captcha challenges dispatch to the configured strategy.
func (rn *Runner) RunSteps(ctx context.Context, r *Run, steps []StepDef, sess browser.Session) error {
	done, err := rn.CompletedSteps(ctx, r.RequestID)
	if err != nil {
		return err
	}
	now := time.Now
	if rn.Now != nil {
		now = rn.Now
	}
	for _, st := range steps {
		if done[st.Name] {
			rn.emit(r.RequestID, st.Name, 0, true, "skipped (resume)")
			continue
		}
		if st.Skip != nil {
			skip, err := st.Skip(ctx, r)
			if err != nil {
				return fmt.Errorf("browserflow: step %s skip-check: %w", st.Name, err)
			}
			if skip {
				rn.emit(r.RequestID, st.Name, 0, true, "skipped (already done)")
				continue
			}
		}
		t0 := now()
		err = st.Run(ctx, r, sess)
		ms := now().Sub(t0).Milliseconds()
		if err != nil {
			if isCaptcha(err) {
				return rn.onCaptcha(ctx, r, sess, st.Name, err)
			}
			rn.emit(r.RequestID, st.Name, ms, false, safeDetail(err.Error()))
			_ = LogStep(ctx, r.DB, now, r.RequestID, st.Name, ms, false, safeDetail(err.Error()))
			return fmt.Errorf("browserflow: step %s: %w", st.Name, err)
		}
		rn.emit(r.RequestID, st.Name, ms, true, "ok")
		_ = LogStep(ctx, r.DB, now, r.RequestID, st.Name, ms, true, "ok")
	}
	return nil
}

func (rn *Runner) emit(requestID, step string, ms int64, ok bool, detail string) {
	if rn.Log != nil {
		rn.Log(requestID, step, ms, ok, detail)
	}
}

// onCaptcha dispatches to the configured recovery strategy. Never solves.
func (rn *Runner) onCaptcha(ctx context.Context, r *Run, sess browser.Session, step string, err error) error {
	shot := ""
	if sess != nil {
		if p, serr := sess.Screenshot(ctx, "captcha-"+step); serr == nil {
			shot = p
		}
	}
	detail := "captcha/blocker detected"
	if shot != "" {
		detail += " screenshot=" + shot
	}
	now := time.Now
	if rn.Now != nil {
		now = rn.Now
	}
	_ = LogStep(ctx, r.DB, now, r.RequestID, step+":captcha", 0, false, detail)
	rn.emit(r.RequestID, step+":captcha", 0, false, detail)
	switch rn.Captcha {
	case CaptchaAbort:
		return &BlockedError{Step: step, Detail: detail}
	case CaptchaRetry:
		return &RetryableError{Step: step, Detail: detail}
	default: // CaptchaPause
		return &PausedError{Step: step, Detail: detail}
	}
}

// BlockedError aborts the run; the card is quarantined upstream.
type BlockedError struct {
	Step   string
	Detail string
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("browserflow: blocked at %s (%s)", e.Step, e.Detail)
}

// PausedError holds the job for manual solve + resume.
type PausedError struct {
	Step   string
	Detail string
}

func (e *PausedError) Error() string {
	return fmt.Sprintf("browserflow: paused for manual solve at %s (%s)", e.Step, e.Detail)
}

// RetryableError asks the caller to rerun with fresh profile/IP (capped).
type RetryableError struct {
	Step   string
	Detail string
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("browserflow: retryable block at %s (%s)", e.Step, e.Detail)
}

// isCaptchaText matches visible blocker signals: challenge widgets,
// access-denied pages, rate-limit notices. Conservative on purpose —
// false positives pause (default) rather than burn the run.
func isCaptchaText(s string) bool {
	lower := strings.ToLower(s)
	for _, sig := range []string{
		"captcha", "recaptcha", "turnstile", "challenge",
		"access denied", "access-denied", "forbidden",
		"rate limit", "rate-limit", "too many requests",
		"verify you are human", "are you a robot",
		"unusual traffic", "blocked",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}
func isCaptcha(err error) bool {
	if err == nil {
		return false
	}
	var b *BlockedError
	var p *PausedError
	var rt *RetryableError
	return as(err, &b) || as(err, &p) || as(err, &rt) || isCaptchaText(err.Error())
}

// as is a local errors.As to avoid importing errors in hot paths twice.
func as(err error, target any) bool {
	type causer interface{ As(any) bool }
	for err != nil {
		if c, ok := err.(causer); ok && c.As(target) {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Producer adapts the orchestrator to flow.KeyProducer. Generate runs
// the profile steps against one request-scoped browser session and
// returns collected keys.
type Producer struct {
	Runner   *Runner
	Open     func(ctx context.Context, r *Run) (browser.Session, error)
	Profile  func(r *Run) (Profile, []StepDef)
	OTPWait  func(ctx context.Context, recipient string, timeout time.Duration) (OTPCode, error)
	Accounts func(ctx context.Context, r *Run) error
}

func (p *Producer) Generate(ctx context.Context, email string, card provider.Secrets, qty int) ([]string, error) {
	return nil, fmt.Errorf("browserflow: Producer.Generate needs request scope (use GenerateFor)")
}

// GenerateFor runs the full profile against request-scoped state.
func (p *Producer) GenerateFor(ctx context.Context, r *Run) ([]string, error) {
	if err := p.Accounts(ctx, r); err != nil {
		return nil, err
	}
	sess, err := p.Open(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("browserflow: open session: %w", err)
	}
	defer func() { _ = sess.Close() }()
	_, steps := p.Profile(r)
	if err := p.Runner.RunSteps(ctx, r, steps, sess); err != nil {
		return nil, err
	}
	return r.Collected, nil
}
