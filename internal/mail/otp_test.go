package mail

import (
	"context"
	"database/sql"
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

func TestExtractOTPBasic(t *testing.T) {
	code, ok := ExtractOTP("Your code", "Your verification code is 482917. It expires in 10 minutes.", "")
	if !ok || code != "482917" {
		t.Fatalf("got %q,%v want 482917,true", code, ok)
	}
}

func TestExtractOTPSubject(t *testing.T) {
	code, ok := ExtractOTP("123456 is your login code", "Hi there, welcome!", "")
	if !ok || code != "123456" {
		t.Fatalf("got %q,%v want 123456,true", code, ok)
	}
}

func TestExtractOTPHTML(t *testing.T) {
	html := `<html><body><p>Your confirmation code: <b>739201</b>. Enter it to sign in.</p></body></html>`
	code, ok := ExtractOTP("", "", html)
	if !ok || code != "739201" {
		t.Fatalf("got %q,%v want 739201,true", code, ok)
	}
}

func TestExtractOTPRejectsNoise(t *testing.T) {
	// No OTP context: order numbers and years must not match.
	if _, ok := ExtractOTP("Receipt", "Order 847213 confirmed. Total $120. Shipped 2024.", ""); ok {
		t.Fatal("matched non-OTP digits")
	}
	if _, ok := ExtractOTP("", "See you in 2026!", ""); ok {
		t.Fatal("matched a year")
	}
	// Repeated runs are pins people shouldn't reuse; still require context.
	if _, ok := ExtractOTP("", "1111", ""); ok {
		t.Fatal("matched context-free repeated run")
	}
}

func TestExtractOTPAmexCID(t *testing.T) {
	// 4-digit codes need context; with it they match.
	code, ok := ExtractOTP("", "Your Amex verification code: 5917", "")
	if !ok || code != "5917" {
		t.Fatalf("got %q,%v want 5917,true", code, ok)
	}
}

func TestLatestOTPNewestWins(t *testing.T) {
	sqldb := openTestDB(t)
	ctx := context.Background()
	old := Inbound{MessageID: "m1", Recipient: "u@x.com", Subject: "code", Text: "old code 111222 valid 5 min", ReceivedAt: time.Now().Add(-time.Hour)}
	newer := Inbound{MessageID: "m2", Recipient: "u@x.com", Subject: "code", Text: "new code 999888 valid 5 min", ReceivedAt: time.Now()}
	if _, err := Store(ctx, sqldb, old); err != nil {
		t.Fatal(err)
	}
	if _, err := Store(ctx, sqldb, newer); err != nil {
		t.Fatal(err)
	}
	r, ok, err := LatestOTP(ctx, sqldb, "u@x.com", "")
	if err != nil || !ok || r.Code != "999888" {
		t.Fatalf("got %+v,%v,%v want 999888", r, ok, err)
	}
}

func TestLatestOTPNoCode(t *testing.T) {
	sqldb := openTestDB(t)
	ctx := context.Background()
	m := Inbound{MessageID: "m3", Recipient: "v@x.com", Subject: "hi", Text: "just saying hello", ReceivedAt: time.Now()}
	if _, err := Store(ctx, sqldb, m); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LatestOTP(ctx, sqldb, "v@x.com", ""); err != nil || ok {
		t.Fatalf("got ok=%v err=%v want false,nil", ok, err)
	}
}

func TestWaitForOTPArrives(t *testing.T) {
	sqldb := openTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		time.Sleep(500 * time.Millisecond)
		_, _ = Store(context.Background(), sqldb, Inbound{
			MessageID: "m4", Recipient: "w@x.com", Subject: "verify",
			Text: "code 246810 expires soon", ReceivedAt: time.Now(),
		})
	}()
	r, err := WaitForOTP(ctx, sqldb, "w@x.com", 8*time.Second)
	if err != nil || r.Code != "246810" {
		t.Fatalf("got %+v,%v want 246810", r, err)
	}
}

func TestWaitForOTPTimeout(t *testing.T) {
	sqldb := openTestDB(t)
	ctx := context.Background()
	start := time.Now()
	_, err := WaitForOTP(ctx, sqldb, "nobody@x.com", 6*time.Second)
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) < 5*time.Second {
		t.Fatal("returned too early")
	}
}

func TestWaitForOTPIgnoresStale(t *testing.T) {
	sqldb := openTestDB(t)
	ctx := context.Background()
	_, err := Store(ctx, sqldb, Inbound{
		MessageID: "m5", Recipient: "s@x.com", Subject: "verify",
		Text: "old code 121212 valid", ReceivedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = WaitForOTP(ctx, sqldb, "s@x.com", 6*time.Second)
	if err == nil {
		t.Fatal("stale code must not match")
	}
}
