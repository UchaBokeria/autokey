package worker

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

//go:embed template.js
var templateJS string

// Template returns the email() worker source.
func Template() string { return templateJS }

// Deps wires Cloudflare REST deployment.
type Deps struct {
	APIToken   string
	AccountID  string
	ZoneID     string
	ScriptName string
	HTTP       *http.Client
	// Base overrides the API root (tests point at a mock server).
	Base string
}

// New returns Deps with defaults.
func New(token, accountID, zoneID, script string) Deps {
	if script == "" {
		script = "autokey-inbox"
	}
	return Deps{
		APIToken: token, AccountID: accountID, ZoneID: zoneID,
		ScriptName: script, HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

type cfResp struct {
	Success bool            `json:"success"`
	Errors  []cfMsg         `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfMsg struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (d Deps) base() string {
	if d.Base != "" {
		return d.Base
	}
	return "https://api.cloudflare.com/client/v4"
}

func (d Deps) do(ctx context.Context, method, path string, body io.Reader, contentType string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.APIToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out cfResp
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("cloudflare %s %s status %d: %w", method, path, resp.StatusCode, err)
	}
	if !out.Success {
		msg := ""
		for _, e := range out.Errors {
			msg += fmt.Sprintf("[%d %s] ", e.Code, e.Message)
		}
		return nil, fmt.Errorf("cloudflare %s %s: %s", method, path, msg)
	}
	return out.Result, nil
}

func (d Deps) doJSON(ctx context.Context, method, path string, v any) (json.RawMessage, error) {
	var body io.Reader
	if v != nil {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	return d.do(ctx, method, path, body, "application/json")
}

// UploadScript PUTs the email() worker module with vars bindings.
func (d Deps) UploadScript(ctx context.Context, inboxURL, bearer, fallback string) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	meta := map[string]any{
		"main_module": "worker.js",
		"bindings": []any{
			map[string]any{"type": "plain_text", "name": "AUTOKEY_INBOX_URL", "text": inboxURL},
			map[string]any{"type": "secret_text", "name": "AUTOKEY_BEARER", "text": bearer},
			map[string]any{"type": "plain_text", "name": "FALLBACK_FORWARD", "text": fallback},
		},
		"compatibility_date": time.Now().UTC().Format("2006-01-02"),
	}
	rawMeta, _ := json.Marshal(meta)
	_ = mw.WriteField("metadata", string(rawMeta))
	// ES module part MUST be application/javascript+module (else
	// "Main module must be an ES module").
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="worker.js"; filename="worker.js"`}
	h["Content-Type"] = []string{"application/javascript+module"}
	fw, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(fw, templateJS); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	_, err = d.do(ctx, http.MethodPut,
		fmt.Sprintf("/accounts/%s/workers/scripts/%s", d.AccountID, d.ScriptName),
		&buf, mw.FormDataContentType())
	return err
}

// EnableRoutingDNS POSTs the MX/SPF/DKIM/DMARC records Cloudflare requires.
func (d Deps) EnableRoutingDNS(ctx context.Context) error {
	_, err := d.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/zones/%s/email/routing/dns", d.ZoneID), map[string]any{})
	return err
}

// CreateDestination adds a verified-target address (user must click verify email).
func (d Deps) CreateDestination(ctx context.Context, email string) error {
	_, err := d.doJSON(ctx, http.MethodPost,
		fmt.Sprintf("/accounts/%s/email/routing/addresses", d.AccountID),
		map[string]any{"email": email})
	return err
}

// SetCatchAllWorker points the zone catch-all at the worker.
func (d Deps) SetCatchAllWorker(ctx context.Context) error {
	_, err := d.doJSON(ctx, http.MethodPut,
		fmt.Sprintf("/zones/%s/email/routing/rules/catch_all", d.ZoneID),
		map[string]any{
			"name":     "autokey catch-all",
			"enabled":  true,
			"matchers": []any{map[string]any{"type": "all"}},
			"actions":  []any{map[string]any{"type": "worker", "value": []string{d.ScriptName}}},
		})
	return err
}

// Status fetches routing settings + catch-all for `worker status`.
func (d Deps) Status(ctx context.Context) (routingStatus, catchAll string, err error) {
	raw, err := d.doJSON(ctx, http.MethodGet,
		fmt.Sprintf("/zones/%s/email/routing", d.ZoneID), nil)
	if err != nil {
		return "", "", err
	}
	var s struct {
		Enabled bool   `json:"enabled"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(raw, &s)
	routingStatus = fmt.Sprintf("enabled=%v status=%s", s.Enabled, s.Status)
	raw2, err := d.doJSON(ctx, http.MethodGet,
		fmt.Sprintf("/zones/%s/email/routing/rules/catch_all", d.ZoneID), nil)
	if err != nil {
		return routingStatus, "", err
	}
	var c struct {
		Enabled bool `json:"enabled"`
		Actions []struct {
			Type  string   `json:"type"`
			Value []string `json:"value"`
		} `json:"actions"`
	}
	_ = json.Unmarshal(raw2, &c)
	catchAll = fmt.Sprintf("enabled=%v actions=%+v", c.Enabled, c.Actions)
	return routingStatus, catchAll, nil
}
