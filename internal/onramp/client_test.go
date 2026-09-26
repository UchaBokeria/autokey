package onramp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uchabokeria/autokey/internal/provider"
)

func mockAPI(t *testing.T, stockStatus string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crypto/cards/provider-status/":
			_, _ = w.Write([]byte(`{"cards":{
				"mastercard":{"brand":"Swype Mastercard","provider":"mastercard","status":"` + stockStatus + `","currency":"USD",
					"amount":{"min":5,"max":499},
					"optional_parameters":{"ticker":{"default":"polygon/usdt","supported":["polygon/usdt","eth"]}}},
				"paypal":{"brand":"PayPal","provider":"paypal","status":"available","currency":"USD",
					"amount":{"min":5,"max":1000},
					"optional_parameters":{"ticker":{"default":"polygon/usdt","supported":["polygon/usdt"]}}}
			}}`))
		case "/crypto/cards/wallet.php":
			q := r.URL.Query()
			if q.Get("provider") == "paypal" && q.Get("paypal_email") == "" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"paypal_email required"}`))
				return
			}
			_, _ = w.Write([]byte(`{"redeem_id":"test_r1","amount":5.86,"payment_coin":"USDT",
				"payment_instructions":"USDT Polygon only","network":"Polygon",
				"card_value":"5.00","card_currency":"USD","card_type":"` + q.Get("provider") + `",
				"address_in":"0xabc","timestamp_token":"t","qr_code":"q","ipn_token":"i"}`))
		case "/crypto/cards/status.php":
			if r.URL.Query().Get("redeem_id") == "done_r" {
				_, _ = w.Write([]byte(`{"payment_status":"paid","card_issuer_status":"completed","redeem_link":"https://r.example/x"}`))
			} else {
				_, _ = w.Write([]byte(`{"payment_status":"unpaid","card_issuer_status":"N/A","redeem_link":"N/A"}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	c := New()
	c.BaseURL = srv.URL
	return c
}

func TestStock(t *testing.T) {
	stock, err := mockAPI(t, "available").Stock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mc := stock["mastercard"]
	if mc.Min != 5 || mc.Max != 499 || len(mc.Tickers) != 2 {
		t.Fatalf("bad mastercard %+v", mc)
	}
}

func TestMintAndClassify(t *testing.T) {
	p := &Provider{C: mockAPI(t, "available")}
	ref, err := p.Mint(context.Background(), provider.MintParams{AmountUSD: 5, Product: "mastercard"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "test_r1" {
		t.Fatalf("bad ref %+v", ref)
	}
	// Unpaid order => secrets fail, retryable.
	if _, err := p.Secrets(context.Background(), "test_r1"); err == nil {
		t.Fatal("want unpaid error")
	}
	s, err := p.C.CheckStatus(context.Background(), "done_r")
	if err != nil {
		t.Fatal(err)
	}
	if s.RedeemLink == "" || strings.ToLower(s.PaymentStatus) != "paid" {
		t.Fatalf("bad status %+v", s)
	}
	if err := p.Quarantine(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if pe := p.Classify(&Error{Message: "oos", OutOfStock: true}); pe.Class != "out-of-stock" || !pe.Retryable {
		t.Fatalf("bad classify %+v", pe)
	}
}

func TestMintOutOfStock(t *testing.T) {
	p := &Provider{C: mockAPI(t, "out of stock")}
	if _, err := p.Mint(context.Background(), provider.MintParams{AmountUSD: 5, Product: "mastercard"}); err == nil {
		t.Fatal("want out-of-stock")
	}
}

func TestMintBadAmount(t *testing.T) {
	p := &Provider{C: mockAPI(t, "available")}
	if _, err := p.Mint(context.Background(), provider.MintParams{AmountUSD: 5000, Product: "mastercard"}); err == nil {
		t.Fatal("want amount-range error")
	}
}
