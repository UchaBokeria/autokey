package automate

import (
	"context"
	"testing"

	"github.com/uchabokeria/autokey/internal/browserflow"
	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/provider"
)

func TestWithOverridesRoundTrip(t *testing.T) {
	ctx := WithOverrides(context.Background(), "http://proxy:8080", "abort-quarantine")
	if got := proxyOf(ctx, "direct"); got != "http://proxy:8080" {
		t.Fatalf("proxy: got %q", got)
	}
	if got := captchaOf(ctx, browserflow.CaptchaPause); got != browserflow.CaptchaAbort {
		t.Fatalf("captcha: got %q", got)
	}
	// Empty overrides fall back.
	ctx2 := WithOverrides(context.Background(), "", "")
	if got := proxyOf(ctx2, "direct"); got != "direct" {
		t.Fatalf("proxy fallback: got %q", got)
	}
	if got := captchaOf(ctx2, browserflow.CaptchaPause); got != browserflow.CaptchaPause {
		t.Fatalf("captcha fallback: got %q", got)
	}
	// Bad captcha string falls back instead of erroring mid-run.
	ctx3 := WithOverrides(context.Background(), "", "solve-it")
	if got := captchaOf(ctx3, browserflow.CaptchaPause); got != browserflow.CaptchaPause {
		t.Fatalf("bad captcha fallback: got %q", got)
	}
}

func TestGenerateUnknownEmail(t *testing.T) {
	sqldb, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqldb.Close() }()
	a := &Adapter{DB: sqldb}
	_, err = a.Generate(context.Background(), "nobody@example.com", provider.Secrets{}, 1)
	if err == nil {
		t.Fatal("want resolve error for unknown email")
	}
}
