package omegameta

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/browser"
	"github.com/uchabokeria/autokey/internal/browserflow"
	"github.com/uchabokeria/autokey/internal/provider"
)

// TestFixtureFullFlow drives the real headless driver against the local
// fixture: signup-password → billing → keys. No live hits; gated like
// the spike test (PW_SPIKE=1).
func TestFixtureFullFlow(t *testing.T) {
	if os.Getenv("PW_SPIKE") == "" {
		t.Skip("set PW_SPIKE=1 to run live browser fixture")
	}
	base := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	drv := &browser.PlaywrightDriver{
		CachedChromium: os.Getenv("HOME") + "/.cache/ms-playwright/chromium-1223/chrome-linux64/chrome",
	}
	profDir, err := os.MkdirTemp("", "omegameta-prof-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(profDir) }()
	runDir, err := os.MkdirTemp("", "omegameta-run-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	r := &browserflow.Run{
		RequestID: "req-fixture-1",
		Email:     "jun01032026-a3f9@example.invalid",
		Card:      provider.Secrets{Number: "4111111111111111", Expiry: "12/28", CVV: "123"},
		CardLast4: "1111",
		CardName:  "Fixture User",
		Qty:       1,
		Country:   "US",
		Now:       func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) },
	}
	cfg := Config{
		BaseURL:    base,
		DOBMode:    DOBSelects,
		OTPTimeout: 10 * time.Second,
		Open: func(ctx context.Context, r *browserflow.Run) (browser.Session, error) {
			opts := browser.Options{
				ProfileDir: profDir,
				Headless:   true,
				RunDir:     runDir,
			}
			opts.Defaults()
			return drv.Launch(ctx, opts)
		},
		Mail: func(_ context.Context, _ *browserflow.Run) (string, error) {
			return "482917", nil
		},
	}
	if err := Prepare(ctx, r, cfg); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if r.Slug != "jun01032026-a3f9" || r.DOB != "2006-09-27" || len(r.Password) != 20 {
		t.Fatalf("bad prepared run: %+v", r)
	}
	if err := SignupPassword(ctx, r, cfg); err != nil {
		t.Fatalf("signup: %v", err)
	}
	if err := Billing(ctx, r, cfg); err != nil {
		t.Fatalf("billing: %v", err)
	}
	if err := Keys(ctx, r, cfg); err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(r.Collected) != 1 || r.Collected[0] != "sk-test-KEY" {
		t.Fatalf("bad keys: %v", r.Collected)
	}
}
