package mail

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// OTPResult is a code found for one recipient.
type OTPResult struct {
	Recipient  string
	Code       string
	Subject    string
	Sender     string
	ReceivedAt string
}

// LatestOTP returns the newest extractable code for a recipient,
// considering only mail received at or after since (empty since = any).
// Returns ("", false, nil) when mail exists but no code extracts,
// and ("", false, err) on DB errors.
func LatestOTP(ctx context.Context, sqldb *sql.DB, recipient, since string) (OTPResult, bool, error) {
	q := `SELECT recipient,subject,sender,body_text,body_html,received_at
	      FROM inbound_emails WHERE recipient=?`
	args := []any{recipient}
	if since != "" {
		q += ` AND received_at >= ?`
		args = append(args, since)
	}
	q += ` ORDER BY received_at DESC`
	rows, err := sqldb.QueryContext(ctx, q, args...)
	if err != nil {
		return OTPResult{}, false, fmt.Errorf("query inbound: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r OTPResult
		var text, html string
		if err := rows.Scan(&r.Recipient, &r.Subject, &r.Sender, &text, &html, &r.ReceivedAt); err != nil {
			return OTPResult{}, false, err
		}
		if code, ok := ExtractOTP(r.Subject, text, html); ok {
			r.Code = code
			return r, true, nil
		}
	}
	return OTPResult{}, false, rows.Err()
}

// WaitForOTP polls LatestOTP until a code appears for recipient or the
// timeout elapses. Only mail received after start is considered, so stale
// codes from earlier runs never match. Polls every 5s.
func WaitForOTP(ctx context.Context, sqldb *sql.DB, recipient string, timeout time.Duration) (OTPResult, error) {
	start := time.Now().UTC().Format(time.RFC3339)
	deadline := time.Now().Add(timeout)
	for {
		r, ok, err := LatestOTP(ctx, sqldb, recipient, start)
		if err != nil {
			return OTPResult{}, err
		}
		if ok {
			return r, nil
		}
		if time.Now().After(deadline) {
			return OTPResult{}, fmt.Errorf("timed out after %s waiting for OTP to %s", timeout, recipient)
		}
		select {
		case <-ctx.Done():
			return OTPResult{}, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
