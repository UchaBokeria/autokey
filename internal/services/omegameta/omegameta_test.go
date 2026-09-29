package omegameta

import (
	"context"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/browser"
	"github.com/uchabokeria/autokey/internal/browserflow"
	"github.com/uchabokeria/autokey/internal/provider"
)

// fakeSession records calls without a browser (no live hits).
type fakeSession struct {
	fills  []string
	clicks []string
	closed bool
	exist  map[string]bool
	texts  map[string]string
}

func (f *fakeSession) Goto(_ context.Context, _ string) error { return nil }
func (f *fakeSession) Fill(_ context.Context, locs []browser.Locator, text string) error {
	f.fills = append(f.fills, locs[0].Value+":"+text)
	return nil
}
func (f *fakeSession) Click(_ context.Context, locs []browser.Locator) error {
	f.clicks = append(f.clicks, locs[0].Value)
	return nil
}
func (f *fakeSession) Tick(_ context.Context, _ []browser.Locator) error { return nil }
func (f *fakeSession) Select(_ context.Context, _ []browser.Locator, _ string) error {
	return nil
}
func (f *fakeSession) WaitFor(_ context.Context, _ []browser.Locator, _ time.Duration) error {
	return nil
}
func (f *fakeSession) Exists(_ context.Context, locs []browser.Locator) (bool, error) {
	return f.exist[locs[0].Value], nil
}
func (f *fakeSession) Text(_ context.Context, locs []browser.Locator) (string, error) {
	if t, ok := f.texts[locs[0].Value]; ok {
		return t, nil
	}
	return "", nil
}
func (f *fakeSession) Screenshot(_ context.Context, _ string) (string, error) {
	return "/tmp/fake.png", nil
}
func (f *fakeSession) Close() error { f.closed = true; return nil }

func testRun() *browserflow.Run {
	return &browserflow.Run{
		RequestID: "req-1",
		Email:     "jun01032026-a3f9@my.com",
		Card:      provider.Secrets{},
		Qty:       1,
		Now:       func() time.Time { return time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC) },
	}
}

func testCfg(sess *fakeSession) Config {
	return Config{
		BaseURL:    "https://example.invalid",
		DOBMode:    DOBSelects,
		OTPTimeout: time.Minute,
		Open: func(_ context.Context, _ *browserflow.Run) (browser.Session, error) {
			return sess, nil
		},
		Mail: func(_ context.Context, _ *browserflow.Run) (string, error) {
			return "482917", nil
		},
	}
}

func TestPrepare(t *testing.T) {
	r := testRun()
	if err := Prepare(context.Background(), r, testCfg(&fakeSession{})); err != nil {
		t.Fatal(err)
	}
	if r.Slug != "jun01032026-a3f9" {
		t.Fatalf("bad slug %q", r.Slug)
	}
	if r.DOB != "2006-09-27" {
		t.Fatalf("bad dob %q", r.DOB)
	}
	if len(r.Password) != 20 {
		t.Fatalf("bad password len %d", len(r.Password))
	}
	if r.Country != "US" {
		t.Fatalf("bad country default %q", r.Country)
	}
	// Slug mismatch must fail (pitfall #1).
	r2 := testRun()
	r2.Slug = "someone-else"
	if err := Prepare(context.Background(), r2, testCfg(&fakeSession{})); err == nil {
		t.Fatal("slug mismatch accepted")
	}
}

func TestSignupPasswordFills(t *testing.T) {
	sess := &fakeSession{exist: map[string]bool{}}
	r := testRun()
	if err := Prepare(context.Background(), r, testCfg(sess)); err != nil {
		t.Fatal(err)
	}
	if err := SignupPassword(context.Background(), r, testCfg(sess)); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, f := range sess.fills {
		joined += f + "\n"
	}
	for _, want := range []string{"Email:" + r.Email, "Password:" + r.Password} {
		found := false
		for _, f := range sess.fills {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing fill %q in %q", want, joined)
		}
	}
}

func TestSignupOTPBuffersResult(t *testing.T) {
	sess := &fakeSession{}
	r := testRun()
	if err := Prepare(context.Background(), r, testCfg(sess)); err != nil {
		t.Fatal(err)
	}
	if err := SignupOTP(context.Background(), r, testCfg(sess)); err != nil {
		t.Fatal(err)
	}
	if !r.OTPUsed {
		t.Fatal("OTPUsed not set")
	}
	found := false
	for _, f := range sess.fills {
		if f == "Verification code:482917" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("OTP not filled: %v", sess.fills)
	}
}

func TestKeysCollectsInOrder(t *testing.T) {
	sess := &fakeSession{texts: map[string]string{"API key": "sk-test-1"}}
	r := testRun()
	r.Collected = nil
	cfg := testCfg(sess)
	if err := Keys(context.Background(), r, cfg); err != nil {
		t.Fatal(err)
	}
	if len(r.Collected) != 1 || r.Collected[0] != "sk-test-1" {
		t.Fatalf("bad collected: %v", r.Collected)
	}
}

func TestStepsRegistered(t *testing.T) {
	r := testRun()
	steps := Steps(r, testCfg(&fakeSession{}))
	want := []string{"prepare", "signup-password", "signup-otp-fallback", "billing", "keys"}
	if len(steps) != len(want) {
		t.Fatalf("want %d steps, got %d", len(want), len(steps))
	}
	for i, w := range want {
		if steps[i].Name != w {
			t.Fatalf("step %d: want %q got %q", i, w, steps[i].Name)
		}
	}
}
