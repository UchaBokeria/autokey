package kripi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Supported BINs from the KripiCard Overview module.
var SupportedBINs = []string{
	"539502", "525847", "539578", "525797", "235019",
	"223600", "238003", "537872", "533171", "246001",
}

// DOBRequiredBINs need dateOfBirth YYYY-MM-DD (US/SG/UK).
var DOBRequiredBINs = map[string]bool{"537872": true, "533171": true, "246001": true}

// Client is a KripiCard API client. HTTP is injectable for tests.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTP       *http.Client
	Now        func() time.Time
	OnRateWait func(scope string, wait time.Duration)
}

// New returns a Client with sane defaults.
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Now:     time.Now,
	}
}

// Card mirrors create/list fields. Never carries PAN/CVV (details call is separate).
type Card struct {
	ID         string  `json:"card_id"`
	Last4      string  `json:"last_4"`
	Last4Alt   string  `json:"last4"`
	BIN        string  `json:"bin"`
	Brand      string  `json:"card_brand"`
	Status     string  `json:"status"`
	Balance    float64 `json:"balance"`
	CreatedAt  string  `json:"created_at"`
	NameOnCard string  `json:"name_on_card"`
}

// CardSecrets carries PAN material in memory only. Never log or persist.
type CardSecrets struct {
	Number string
	Expiry string
	CVV    string
}

// CreateCard issues a virtual Mastercard. amountUSD >= 10.
func (c *Client) CreateCard(ctx context.Context, bin string, amountUSD float64, name, email, dob string) (Card, error) {
	body := map[string]any{
		"api_key": c.APIKey, "bin": bin,
		"amount": amountUSD, "name_on_card": name,
	}
	if email != "" {
		body["email"] = email
	}
	if DOBRequiredBINs[bin] {
		if dob == "" {
			return Card{}, &CardError{Class: CleanFail, Message: "dateOfBirth required for BIN " + bin}
		}
		body["dateOfBirth"] = dob
	}
	var out struct {
		Card
		Fee          float64 `json:"fee"`
		TotalCharged float64 `json:"total_charged"`
		Last4b       string  `json:"last_4"`
	}
	if err := c.post(ctx, "/api/external/cards/createcard", body, &out); err != nil {
		return Card{}, err
	}
	if out.Last4 == "" {
		out.Last4 = out.Last4b
	}
	return out.Card, nil
}

// Details returns live PAN material. Handle with care.
func (c *Client) Details(ctx context.Context, cardID string) (CardSecrets, float64, string, error) {
	var out struct {
		Success    bool    `json:"success"`
		CardNumber string  `json:"card_number"`
		Expiry     string  `json:"expiry"`
		CVV        string  `json:"cvv"`
		Balance    float64 `json:"balance"`
		Status     string  `json:"status"`
	}
	if err := c.post(ctx, "/api/external/cards/carddetails",
		map[string]any{"api_key": c.APIKey, "card_id": cardID}, &out); err != nil {
		return CardSecrets{}, 0, "", err
	}
	return CardSecrets{Number: out.CardNumber, Expiry: out.Expiry, CVV: out.CVV}, out.Balance, out.Status, nil
}

// List returns all cards under the account.
func (c *Client) List(ctx context.Context) ([]Card, error) {
	var out struct {
		Success bool   `json:"success"`
		Total   int    `json:"total"`
		Data    []Card `json:"data"`
	}
	if err := c.post(ctx, "/api/external/cards/list", map[string]any{"api_key": c.APIKey}, &out); err != nil {
		return nil, err
	}
	for i := range out.Data {
		if out.Data[i].Last4 == "" {
			out.Data[i].Last4 = out.Data[i].Last4Alt
		}
	}
	return out.Data, nil
}

// Fund adds funds. Fee $1.00 + 4%.
func (c *Client) Fund(ctx context.Context, cardID string, amountUSD float64) error {
	var out envelope
	return c.post(ctx, "/api/external/cards/fundcard",
		map[string]any{"api_key": c.APIKey, "card_id": cardID, "amount": amountUSD}, &out)
}

// Freeze suspends (freeze=true) or reactivates a card.
func (c *Client) Freeze(ctx context.Context, cardID string, freeze bool) error {
	action := "unfreeze"
	if freeze {
		action = "freeze"
	}
	var out envelope
	return c.post(ctx, "/api/external/premium/Freeze_Unfreeze",
		map[string]any{"api_key": c.APIKey, "card_id": cardID, "action": action}, &out)
}

// Delete permanently deletes a card, cashing balance minus $2 fee.
func (c *Client) Delete(ctx context.Context, cardID string) (refunded, fee float64, err error) {
	var out struct {
		envelope
		Refunded float64 `json:"refunded"`
		Fee      float64 `json:"fee"`
	}
	if err := c.post(ctx, "/api/external/cards/deletecard",
		map[string]any{"api_key": c.APIKey, "card_id": cardID}, &out); err != nil {
		return 0, 0, err
	}
	return out.Refunded, out.Fee, nil
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("new request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("decode %s status %d: %w", path, resp.StatusCode, err)
	}
	if !env.Success || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusAccepted {
		// Normalize non-success to envelope for classification, then re-decode full body.
		cerr := classify(resp.StatusCode, env)
		if cerr.Class == RateLimited && c.OnRateWait != nil {
			c.OnRateWait(cerr.Scope, cerr.RetryAfter)
		}
		return cerr
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}
