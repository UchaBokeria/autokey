package browser_test

import (
	"context"
	"os"
	"testing"
	"time"

	browser "github.com/uchabokeria/autokey/internal/browser"
)

func TestSpikeLaunch(t *testing.T) {
	if os.Getenv("PW_SPIKE") == "" {
		t.Skip("set PW_SPIKE=1 to run live browser spike")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d := &browser.PlaywrightDriver{
		CachedChromium: os.Getenv("HOME") + "/.cache/ms-playwright/chromium-1223/chrome-linux64/chrome",
	}
	dir, err := os.MkdirTemp("", "pwspike-prof-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	sess, err := d.Launch(ctx, browser.Options{ProfileDir: dir, Headless: true, RunDir: dir})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Goto(ctx, "data:text/html,<title>spike</title><button>Click me</button>"); err != nil {
		t.Fatalf("goto: %v", err)
	}
	title, err := sess.Text(ctx, []browser.Locator{{Kind: "css", Value: "title"}})
	if err != nil {
		t.Fatalf("text: %v", err)
	}
	t.Logf("title=%q", title)
	if err := sess.Click(ctx, []browser.Locator{{Kind: "text", Value: "Click me"}}); err != nil {
		t.Fatalf("click: %v", err)
	}
	if _, err := sess.Screenshot(ctx, "spike"); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
}
