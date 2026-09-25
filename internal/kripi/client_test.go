package kripi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		env    envelope
		want   FailureClass
		retry  bool
	}{
		{"clean 4xx", 400, envelope{Success: false, Message: "Insufficient balance"}, CleanFail, true},
		{"202 refunded", 202, envelope{Success: false, Pending: true, Message: "refunded"}, PendingRefunded, false},
		{"202 refund pending", 202, envelope{Success: false, Pending: true, Code: "REFUND_PENDING"}, RefundPending, false},
		{"429", 429, envelope{Success: false, Scope: "burst", RetrySecs: 39}, RateLimited, false},
		{"in flight", 200, envelope{Success: false, Code: "OPERATION_IN_FLIGHT"}, OperationInFlight, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.status, tc.env)
			if got.Class != tc.want {
				t.Fatalf("class=%v want %v", got.Class, tc.want)
			}
			if got.Retryable() != tc.retry {
				t.Fatalf("retryable=%v want %v", got.Retryable(), tc.retry)
			}
		})
	}
}

func TestCreateCardSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["api_key"] != "k" || body["bin"] != "539502" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "bad"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "card_id": "MR_X", "last_4": "4321",
			"bin": "539502", "amount": 20, "fee": 1.8, "total_charged": 21.8,
		})
	}))
	defer srv.Close()
	c := New(srv.URL, "k")
	card, err := c.CreateCard(t.Context(), "539502", 20, "autokey", "a@b.c", "")
	if err != nil {
		t.Fatal(err)
	}
	if card.ID != "MR_X" || card.Last4 != "4321" {
		t.Fatalf("unexpected card %+v", card)
	}
}

func TestCreateCard429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false, "message": "too fast", "scope": "burst", "retry_after_seconds": 5,
		})
	}))
	defer srv.Close()
	var waited time.Duration
	c := New(srv.URL, "k")
	c.OnRateWait = func(scope string, wait time.Duration) { waited = wait }
	_, err := c.CreateCard(t.Context(), "539502", 20, "autokey", "", "")
	cerr, ok := err.(*CardError)
	if !ok || cerr.Class != RateLimited || cerr.Scope != "burst" {
		t.Fatalf("want rate-limited burst, got %v", err)
	}
	if waited != 5*time.Second {
		t.Fatalf("want 5s wait, got %v", waited)
	}
}

func TestCreateCardNeedsDOB(t *testing.T) {
	c := New("http://127.0.0.1:1", "k")
	_, err := c.CreateCard(t.Context(), "537872", 20, "autokey", "", "")
	if err == nil {
		t.Fatal("want DOB error")
	}
}
