package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/custom"
	"github.com/uchabokeria/autokey/internal/mail"
	"github.com/uchabokeria/autokey/internal/onramp"
	"github.com/uchabokeria/autokey/internal/pool"
)

// API exposes read-only JSON for the dashboard SPA, plus a small set of
// safe mutations. Auth is enforced by the caller (hook bearer).
type API struct {
	DB      *sql.DB
	Onramp  *onramp.Client
	Now     func() time.Time
	Version string
	// CustomKey resolves CUSTOM_CARD_KEY for account decrypt (debugging).
	CustomKey func() string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"success": false, "message": msg})
}

// Overview aggregates pool/request/inbox counts for the dashboard header.
func (a *API) Overview(ctx context.Context) (map[string]any, error) {
	out := map[string]any{"version": a.Version}
	queries := map[string]string{
		"cards_total":    `SELECT COUNT(*) FROM cards`,
		"cards_active":   `SELECT COUNT(*) FROM cards WHERE status='active' AND claimed=0`,
		"cards_claimed":  `SELECT COUNT(*) FROM cards WHERE claimed!=0`,
		"requests_total": `SELECT COUNT(*) FROM requests`,
		"requests_done":  `SELECT COUNT(*) FROM requests WHERE status='done'`,
		"keys_total":     `SELECT COUNT(*) FROM keys`,
		"inbox_total":    `SELECT COUNT(*) FROM inbound_emails`,
	}
	for k, q := range queries {
		var n int
		if err := a.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, fmt.Errorf("overview %s: %w", k, err)
		}
		out[k] = n
	}
	var byProvider []map[string]any
	rows, err := a.DB.QueryContext(ctx,
		`SELECT provider,COUNT(*) FROM cards GROUP BY provider ORDER BY provider`)
	if err != nil {
		return nil, fmt.Errorf("overview providers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		byProvider = append(byProvider, map[string]any{"provider": name, "count": n})
	}
	out["by_provider"] = byProvider
	return out, rows.Err()
}

// Cards lists pool cards with per-service stats (all services).
func (a *API) Cards(ctx context.Context, providerFilter string) ([]map[string]any, error) {
	base := `
SELECT c.card_id,c.last4,c.bin,c.name_on_card,c.provider,c.status,c.claimed,c.created_at,
       COALESCE(SUM(s.ok_count),0),COALESCE(SUM(s.fail_count),0),MAX(s.last_error)
FROM cards c LEFT JOIN card_service_stats s ON s.card_id=c.card_id`
	args := []any{}
	if providerFilter != "" {
		base += ` WHERE c.provider=?`
		args = append(args, providerFilter)
	}
	base += ` GROUP BY c.card_id ORDER BY c.created_at DESC`
	rows, err := a.DB.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("cards: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var id, last4, bin, name, prov, status, created string
		var claimed, okc, failc int
		var lastErr sql.NullString
		if err := rows.Scan(&id, &last4, &bin, &name, &prov, &status, &claimed,
			&created, &okc, &failc, &lastErr); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "last4": last4, "bin": bin, "name": name,
			"provider": prov, "status": status, "claimed": claimed != 0,
			"created_at": created, "ok": okc, "fail": failc,
			"last_error": lastErr.String,
		})
	}
	return out, rows.Err()
}

// CardStats returns per-service rows for one card.
func (a *API) CardStats(ctx context.Context, cardID string) ([]map[string]any, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT service,ok_count,fail_count,last_error,updated_at
		 FROM card_service_stats WHERE card_id=? ORDER BY service`, cardID)
	if err != nil {
		return nil, fmt.Errorf("card stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var svc string
		var okc, failc int
		var lastErr sql.NullString
		var updated string
		if err := rows.Scan(&svc, &okc, &failc, &lastErr, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"service": svc, "ok": okc, "fail": failc,
			"last_error": lastErr.String, "updated_at": updated,
		})
	}
	return out, rows.Err()
}

// Requests lists key requests newest-first with their card + key count.
func (a *API) Requests(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.email,r.service,r.key_quantity,r.provider,r.status,r.card_id,r.error,
       r.created_at,r.updated_at,(SELECT COUNT(*) FROM keys k WHERE k.request_id=r.id)
FROM requests r ORDER BY r.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var id, email, svc, prov, status, created, updated string
		var qty, nkeys int
		var cardID, errMsg sql.NullString
		if err := rows.Scan(&id, &email, &svc, &qty, &prov, &status,
			&cardID, &errMsg, &created, &updated, &nkeys); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "email": email, "service": svc, "key_quantity": qty,
			"provider": prov, "status": status, "card_id": cardID.String,
			"error": errMsg.String, "created_at": created,
			"updated_at": updated, "keys": nkeys,
		})
	}
	return out, rows.Err()
}

// RequestKeys returns key values for one request.
func (a *API) RequestKeys(ctx context.Context, requestID string) ([]string, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT key_value FROM keys WHERE request_id=? ORDER BY created_at`, requestID)
	if err != nil {
		return nil, fmt.Errorf("request keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Inbox lists inbound emails newest-first.
func (a *API) Inbox(ctx context.Context, limit int, recipient string) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT message_id,recipient,sender,subject,
	             substr(body_text,1,280) AS preview,received_at
	      FROM inbound_emails`
	args := []any{}
	if recipient != "" {
		q += ` WHERE recipient=?`
		args = append(args, strings.ToLower(recipient))
	}
	q += ` ORDER BY received_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := a.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("inbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var id, rcpt, from, subj, preview, at string
		if err := rows.Scan(&id, &rcpt, &from, &subj, &preview, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"message_id": id, "recipient": rcpt, "sender": from,
			"subject": subj, "preview": preview, "received_at": at,
		})
	}
	return out, rows.Err()
}

// InboxMessage returns one full email.
func (a *API) InboxMessage(ctx context.Context, messageID string) (map[string]any, error) {
	var id, rcpt, from, subj, text, html, at string
	err := a.DB.QueryRowContext(ctx, `
SELECT message_id,recipient,sender,subject,body_text,body_html,received_at
FROM inbound_emails WHERE message_id=?`, messageID).
		Scan(&id, &rcpt, &from, &subj, &text, &html, &at)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("message not found")
	}
	if err != nil {
		return nil, fmt.Errorf("inbox message: %w", err)
	}
	return map[string]any{
		"message_id": id, "recipient": rcpt, "sender": from,
		"subject": subj, "body_text": text, "body_html": html,
		"received_at": at,
	}, nil
}

// OTPFor reads the newest stored code for an email user.
func (a *API) OTPFor(ctx context.Context, email string) (mail.OTPResult, bool, error) {
	return mail.LatestOTP(ctx, a.DB, email, "")
}

// EmailForRequest returns the email linked to a request id (OTP drill-down).
func (a *API) EmailForRequest(ctx context.Context, requestID string) (string, error) {
	var email string
	err := a.DB.QueryRowContext(ctx,
		`SELECT email FROM requests WHERE id=?`, requestID).Scan(&email)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("request not found")
	}
	return email, err
}

// OnrampStock proxies live provider stock.
func (a *API) OnrampStock(ctx context.Context) (map[string]onramp.Product, error) {
	return a.Onramp.Stock(ctx)
}

// RemoveCustom deletes a custom card + its secrets. Kripi/onramp ids rejected.
func (a *API) RemoveCustom(ctx context.Context, cardID string) error {
	if p := pool.ProviderOf(ctx, a.DB, cardID); p != "custom" {
		return fmt.Errorf("card %s belongs to provider %q — remove is custom-only", cardID, p)
	}
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM custom_cards WHERE card_id=?`, cardID); err != nil {
		return fmt.Errorf("remove secrets: %w", err)
	}
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM cards WHERE card_id=?`, cardID); err != nil {
		return fmt.Errorf("remove card: %w", err)
	}
	return nil
}

// RequestSteps returns the persisted per-step run log for a request.
func (a *API) RequestSteps(ctx context.Context, requestID string) ([]map[string]any, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT step,ok,detail,created_at FROM request_steps WHERE request_id=? ORDER BY id`, requestID)
	if err != nil {
		return nil, fmt.Errorf("request steps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var step, detail, at string
		var ok int
		if err := rows.Scan(&step, &ok, &detail, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"step": step, "ok": ok != 0, "detail": detail, "at": at})
	}
	return out, rows.Err()
}

// RequestAccount decrypts a request's account bundle for debugging.
// Auth-guarded like everything else; never cached client-side.
func (a *API) RequestAccount(ctx context.Context, requestID string) (map[string]any, error) {
	key := ""
	if a.CustomKey != nil {
		key = a.CustomKey()
	}
	var blob, salt, dob, country string
	var iters, otp int
	if err := a.DB.QueryRowContext(ctx,
		`SELECT password_enc,salt,iterations,dob,country,otp_used FROM request_accounts WHERE request_id=?`,
		requestID).Scan(&blob, &salt, &iters, &dob, &country, &otp); err != nil {
		return nil, fmt.Errorf("no account for request")
	}
	parts, err := custom.DecryptBlob(key, blob, salt, iters)
	if err != nil {
		return nil, err
	}
	if len(parts) != 3 {
		return nil, fmt.Errorf("corrupt account bundle")
	}
	return map[string]any{
		"password": parts[0], "dob": dob, "country": country,
		"otp_used": otp != 0,
	}, nil
}
