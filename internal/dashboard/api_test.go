package dashboard

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/mail"
	"github.com/uchabokeria/autokey/internal/pool"
)

func openTestAPI(t *testing.T) (*API, *sql.DB) {
	t.Helper()
	sqldb, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	return &API{DB: sqldb, Now: time.Now, Version: "test"}, sqldb
}

func seed(t *testing.T, sqldb *sql.DB) {
	t.Helper()
	ctx := context.Background()
	if err := pool.Register(ctx, sqldb, "kripi", "MR_1", "1111", "539502", "Test"); err != nil {
		t.Fatal(err)
	}
	if err := pool.RecordResult(ctx, sqldb, "MR_1", "x", true, ""); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := sqldb.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,card_id,created_at,updated_at)
VALUES('r1','u@x.com','x',2,'kripi','done','MR_1',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqldb.ExecContext(ctx,
		`INSERT INTO keys(id,request_id,key_value,created_at) VALUES('k1','r1','KEY-0',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mail.Store(ctx, sqldb, mail.Inbound{
		MessageID: "m1", Recipient: "u@x.com", Sender: "n@s.com",
		Subject: "code", Text: "your verification code is 482917",
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOverview(t *testing.T) {
	api, sqldb := openTestAPI(t)
	seed(t, sqldb)
	out, err := api.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out["cards_total"] != 1 || out["requests_done"] != 1 || out["keys_total"] != 1 || out["inbox_total"] != 1 {
		t.Fatalf("bad overview: %+v", out)
	}
}

func TestCardsAndStats(t *testing.T) {
	api, sqldb := openTestAPI(t)
	seed(t, sqldb)
	cards, err := api.Cards(context.Background(), "")
	if err != nil || len(cards) != 1 || cards[0]["id"] != "MR_1" {
		t.Fatalf("bad cards: %+v %v", cards, err)
	}
	filtered, err := api.Cards(context.Background(), "onramp")
	if err != nil || len(filtered) != 0 {
		t.Fatalf("want empty onramp filter, got %+v %v", filtered, err)
	}
	stats, err := api.CardStats(context.Background(), "MR_1")
	if err != nil || len(stats) != 1 || stats[0]["ok"] != 1 {
		t.Fatalf("bad stats: %+v %v", stats, err)
	}
}

func TestRequestsAndKeys(t *testing.T) {
	api, sqldb := openTestAPI(t)
	seed(t, sqldb)
	reqs, err := api.Requests(context.Background(), 10)
	if err != nil || len(reqs) != 1 || reqs[0]["keys"] != 1 {
		t.Fatalf("bad requests: %+v %v", reqs, err)
	}
	keys, err := api.RequestKeys(context.Background(), "r1")
	if err != nil || len(keys) != 1 || keys[0] != "KEY-0" {
		t.Fatalf("bad keys: %+v %v", keys, err)
	}
	email, err := api.EmailForRequest(context.Background(), "r1")
	if err != nil || email != "u@x.com" {
		t.Fatalf("bad email: %q %v", email, err)
	}
}

func TestInboxAndOTP(t *testing.T) {
	api, sqldb := openTestAPI(t)
	seed(t, sqldb)
	mails, err := api.Inbox(context.Background(), 10, "")
	if err != nil || len(mails) != 1 {
		t.Fatalf("bad inbox: %+v %v", mails, err)
	}
	full, err := api.InboxMessage(context.Background(), "m1")
	if err != nil || full["subject"] != "code" {
		t.Fatalf("bad message: %+v %v", full, err)
	}
	res, found, err := api.OTPFor(context.Background(), "u@x.com")
	if err != nil || !found || res.Code != "482917" {
		t.Fatalf("bad otp: %+v %v %v", res, found, err)
	}
}

func TestRemoveCustomGuards(t *testing.T) {
	api, sqldb := openTestAPI(t)
	seed(t, sqldb)
	if err := api.RemoveCustom(context.Background(), "MR_1"); err == nil {
		t.Fatal("kripi card must be rejected")
	}
	if err := api.RemoveCustom(context.Background(), "nope"); err == nil {
		t.Fatal("unknown card must be rejected")
	}
}

func TestTailFileMissing(t *testing.T) {
	lines, err := tailFile("/nonexistent/autokey.jsonl", 10)
	if err != nil || len(lines) != 0 {
		t.Fatalf("want empty, got %+v %v", lines, err)
	}
}
