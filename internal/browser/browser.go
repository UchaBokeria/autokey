package browser

import (
	"context"
	"fmt"
	"time"
)

// Locator is one ordered step in the label-first strategy.
// Kind ranks: label → text → role → css → xpath. IDs/classes live
// at css/xpath level (last resort — they churn fastest).
type Locator struct {
	Kind  string // label|text|role|css|xpath
	Value string
}

// Options configures a headed behavior contract per request.
type Options struct {
	// ProfileDir is a fresh per-request user-data dir (required).
	ProfileDir string
	// Headless uses headless=new (real Chrome, no headless tells).
	Headless bool
	// ExecutablePath overrides browser discovery (spike: cached chromium).
	ExecutablePath string
	// Channel prefers system Chrome ("chrome") when present.
	Channel string
	// ProxyURL is optional residential egress (browser traffic only).
	ProxyURL string
	// Viewport, Locale, Timezone for fingerprint consistency.
	Viewport string // e.g. "1366x768"
	Locale   string // e.g. "en-US"
	Timezone string // e.g. "America/New_York"
	// KeyJitter bounds human keystroke delays; StepPause step gaps.
	KeyJitter [2]time.Duration
	StepPause [2]time.Duration
	// RunDir collects failure screenshots (never PAN — masked pre-capture).
	RunDir string
}

// Defaults fills stealth-safe option values.
func (o *Options) Defaults() {
	if o.Viewport == "" {
		o.Viewport = "1366x768"
	}
	if o.Locale == "" {
		o.Locale = "en-US"
	}
	if o.Timezone == "" {
		o.Timezone = "America/New_York"
	}
	if o.KeyJitter == [2]time.Duration{} {
		o.KeyJitter = [2]time.Duration{80 * time.Millisecond, 250 * time.Millisecond}
	}
	if o.StepPause == [2]time.Duration{} {
		o.StepPause = [2]time.Duration{300 * time.Millisecond, 900 * time.Millisecond}
	}
}

// Driver is the automation backend contract. Backend 1 is playwright-go;
// Backend 2 (CDP-direct) implements the same interface later.
type Driver interface {
	// Launch starts the browser for one request context.
	Launch(ctx context.Context, opts Options) (Session, error)
}

// Session is one request-scoped browser session.
type Session interface {
	// Goto navigates and waits for DOM content.
	Goto(ctx context.Context, url string) error
	// Fill types text with human jitter (keyboard only, never evaluate).
	Fill(ctx context.Context, locs []Locator, text string) error
	// Click scrolls into view, moves the mouse, then clicks.
	Click(ctx context.Context, locs []Locator) error
	// Tick toggles a checkbox/radio if not already set.
	Tick(ctx context.Context, locs []Locator) error
	// Select chooses a <select> option by visible label.
	Select(ctx context.Context, locs []Locator, label string) error
	// WaitFor waits for the first matching locator (DOM, not sleep).
	WaitFor(ctx context.Context, locs []Locator, timeout time.Duration) error
	// Exists reports whether any locator matches right now.
	Exists(ctx context.Context, locs []Locator) (bool, error)
	// Text returns visible text of the first match.
	Text(ctx context.Context, locs []Locator) (string, error)
	// Screenshot captures to run dir with secrets masked pre-capture.
	Screenshot(ctx context.Context, name string) (string, error)
	// Close shuts down context and browser.
	Close() error
}

// CaptchaSignal classifies blocker detection results.
type CaptchaSignal int

const (
	// CaptchaNone means no blocker indicators found.
	CaptchaNone CaptchaSignal = iota
	// CaptchaChallenge means an interactive challenge is present.
	CaptchaChallenge
	// CaptchaBlocked means a hard block page (403/denied/rate-limit).
	CaptchaBlocked
)

// CheckBlocker scans for CAPTCHA/block indicators. Never solves.
func CheckBlocker(_ context.Context, _ Session) (CaptchaSignal, error) {
	return CaptchaNone, fmt.Errorf("browser: CheckBlocker needs a live session (spike)")
}
