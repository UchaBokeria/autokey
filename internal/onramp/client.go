package onramp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// BaseURL for the public Onramp Pay API (no key required).
const BaseURL = "https://api.onramp-pay.com"

// 3% platform fee; issuer/load/network costs are folded into amount due.
const PlatformFee = 0.03

// Client talks to the Onramp one-time-card endpoints. HTTP injectable.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

// New returns a Client with defaults.
func New() *Client {
	return &Client{
		BaseURL: BaseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Now:     time.Now,
	}
}

// Product mirrors provider-status entries.
type Product struct {
	Brand    string   `json:"brand"`
	Provider string   `json:"provider"`
	Status   string   `json:"status"`
	Min      float64  `json:"-"`
	Max      float64  `json:"-"`
	Tickers  []string `json:"-"`
}

// Stock returns available one-time card products.
func (c *Client) Stock(ctx context.Context) (map[string]Product, error) {
	var raw struct {
		Cards map[string]struct {
			Brand    string `json:"brand"`
			Provider string `json:"provider"`
			Status   string `json:"status"`
			Amount   struct {
				Min float64 `json:"min"`
				Max float64 `json:"max"`
			} `json:"amount"`
			Optional struct {
				Ticker struct {
					Default   string   `json:"default"`
					Supported []string `json:"supported"`
				} `json:"ticker"`
			} `json:"optional_parameters"`
		} `json:"cards"`
	}
	if err := c.get(ctx, "/crypto/cards/provider-status/", nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Product, len(raw.Cards))
	for k, v := range raw.Cards {
		out[k] = Product{
			Brand: v.Brand, Provider: v.Provider, Status: v.Status,
			Min: v.Amount.Min, Max: v.Amount.Max,
			Tickers: v.Optional.Ticker.Supported,
		}
	}
	return out, nil
}

// Order is a created one-time card order awaiting crypto payment.
type Order struct {
	RedeemID     string  `json:"redeem_id"`
	AmountDue    float64 `json:"-"`
	PaymentCoin  string  `json:"payment_coin"`
	Instructions string  `json:"payment_instructions"`
	Network      string  `json:"network"`
	CardValue    string  `json:"card_value"`
	CardCurrency string  `json:"card_currency"`
	CardType     string  `json:"card_type"`
	AddressIn    string  `json:"address_in"`
	TimestampTok string  `json:"timestamp_token"`
	QRCode       string  `json:"qr_code"`
}

// CreateOrder opens a one-time card order. No funds move; payment is a
// separate on-chain transfer with a ~10 minute window.
func (c *Client) CreateOrder(ctx context.Context, provider string, amountUSD float64, ticker, paypalEmail string) (Order, error) {
	q := url.Values{}
	q.Set("provider", provider)
	q.Set("amount", strconv.FormatFloat(amountUSD, 'f', 2, 64))
	if ticker != "" {
		q.Set("ticker", ticker)
	}
	if paypalEmail != "" {
		q.Set("paypal_email", paypalEmail)
	}
	var raw struct {
		Order
		Amount any `json:"amount"`
	}
	if err := c.get(ctx, "/crypto/cards/wallet.php", q, &raw); err != nil {
		return Order{}, err
	}
	raw.Order.AmountDue = toFloat(raw.Amount)
	return raw.Order, nil
}

// Status checks payment + issuer state for a redeem_id.
type Status struct {
	PaymentStatus string `json:"payment_status"`
	IssuerStatus  string `json:"card_issuer_status"`
	RedeemLink    string `json:"redeem_link"`
}

// CheckStatus polls an order. Completed => secrets ready.
func (c *Client) CheckStatus(ctx context.Context, redeemID string) (Status, error) {
	q := url.Values{}
	q.Set("redeem_id", redeemID)
	var s Status
	if err := c.get(ctx, "/crypto/cards/status.php", q, &s); err != nil {
		return Status{}, err
	}
	return s, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("onramp new request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("onramp GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("onramp read %s: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return &Error{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(data))}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("onramp decode %s: %w", path, err)
		}
	}
	return nil
}

// Error is an Onramp failure. Order creation is free (no charge until the
// on-chain payment), so failures are retryable unless the product is gone.
type Error struct {
	Message    string
	OutOfStock bool
	BadRequest bool
}

func (e *Error) Error() string { return "onramp: " + e.Message }

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
