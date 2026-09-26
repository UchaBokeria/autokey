package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/kripi"
	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/provider"
)

type fakeProducer struct {
	failFor map[string]bool
	calls   int
}

func (f *fakeProducer) Generate(_ context.Context, email string, _ provider.Secrets, qty int) ([]string, error) {
	f.calls++
	if f.failFor[email] {
		return nil, fmt.Errorf("x service declined")
	}
	keys := make([]string, qty)
	for i := range keys {
		keys[i] = fmt.Sprintf("KEY-%d", i)
	}
	return keys, nil
}

// fakeKripi serves create + details with scripted behavior.
func fakeKripi(t *testing.T, failDetailFor map[string]bool) *kripi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/api/external/cards/createcard":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "card_id": "MR_NEW", "last_4": "9999",
				"bin": body["bin"], "amount": body["amount"],
			})
		case "/api/external/cards/carddetails":
			id, _ := body["card_id"].(string)
			if failDetailFor[id] {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "issuer declined"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "card_number": "5395020000009999",
				"expiry": "12/27", "cvv": "123", "balance": 20.0, "status": "active",
			})
		case "/api/external/premium/Freeze_Unfreeze":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "nope"})
		}
	}))
	t.Cleanup(srv.Close)
	return kripi.New(srv.URL, "test")
}

func testDeps(sqldb *sql.DB, k *kripi.Client, p KeyProducer) Deps {
	n := 0
	return Deps{
		DB: sqldb,
		Providers: map[string]provider.CardProvider{
			"kripi": kripi.NewProvider(k),
		},
		Default:  "kripi",
		Producer: p,
		Domain:   "my.com", Service: "x",
		Mint: provider.MintParams{
			AmountUSD: 20, Name: "autokey", BIN: "539502",
		},
		MintCap: 2, Now: time.Now,
		RequestID: func() string { n++; return fmt.Sprintf("req-%d", n) },
	}
}

func TestGenerateDetailsPoolHit(t *testing.T) {
	ctx := context.Background()
	sqldb, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	if err := pool.Register(ctx, sqldb, "MR_POOL", "1111", "539502", "autokey"); err != nil {
		t.Fatal(err)
	}
	p := &fakeProducer{}
	detail, err := GenerateDetails(ctx, testDeps(sqldb, fakeKripi(t, nil), p), "kripi", 2)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CardID != "MR_POOL" || detail.Last4 != "1111" || detail.Provider != "kripi" {
		t.Fatalf("want pool card, got %+v", detail)
	}
	if p.calls != 1 {
		t.Fatalf("want 1 producer call, got %d", p.calls)
	}
	var n int
	_ = sqldb.QueryRow(`SELECT COUNT(*) FROM keys`).Scan(&n)
	if n != 2 {
		t.Fatalf("want 2 keys persisted, got %d", n)
	}
}

func TestGenerateDetailsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	sqldb, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	p := &fakeProducer{}
	if _, err := GenerateDetails(ctx, testDeps(sqldb, fakeKripi(t, nil), p), "nope", 1); err == nil {
		t.Fatal("want unknown-provider error")
	}
}

func TestGenerateDetailsPoolFailThenMint(t *testing.T) {
	ctx := context.Background()
	sqldb, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	if err := pool.Register(ctx, sqldb, "MR_DEAD", "2222", "539502", "autokey"); err != nil {
		t.Fatal(err)
	}
	// Details fails for the pool card -> marked fail -> mint MR_NEW tried.
	p := &fakeProducer{}
	detail, err := GenerateDetails(ctx, testDeps(sqldb, fakeKripi(t, map[string]bool{"MR_DEAD": true}), p), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CardID != "MR_NEW" {
		t.Fatalf("want minted card, got %+v", detail)
	}
	var fails int
	_ = sqldb.QueryRow(`SELECT fail_count FROM card_service_stats WHERE card_id='MR_DEAD' AND service='x'`).Scan(&fails)
	if fails != 1 {
		t.Fatalf("want dead card marked fail=1, got %d", fails)
	}
	var okc int
	_ = sqldb.QueryRow(`SELECT ok_count FROM card_service_stats WHERE card_id='MR_NEW' AND service='x'`).Scan(&okc)
	if okc != 1 {
		t.Fatalf("want minted card marked ok=1, got %d", okc)
	}
}

func TestGenerateDetailsAllFail(t *testing.T) {
	ctx := context.Background()
	sqldb, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	p := &fakeProducer{failFor: map[string]bool{}}
	_ = p
	// Producer always fails: wrap to fail everything.
	pf := &failAllProducer{}
	_, err = GenerateDetails(ctx, testDeps(sqldb, fakeKripi(t, nil), pf), "kripi", 1)
	if err == nil {
		t.Fatal("want failure when everything fails")
	}
	var status string
	_ = sqldb.QueryRow(`SELECT status FROM requests LIMIT 1`).Scan(&status)
	if status != "failed" {
		t.Fatalf("want request failed, got %q", status)
	}
}

type failAllProducer struct{}

func (failAllProducer) Generate(_ context.Context, _ string, _ provider.Secrets, _ int) ([]string, error) {
	return nil, fmt.Errorf("x service down")
}
