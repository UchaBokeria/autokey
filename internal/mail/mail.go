package mail

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Inbound is a normalized inbound email from either ingress.
type Inbound struct {
	MessageID   string
	Recipient   string
	Sender      string
	Subject     string
	Text        string
	HTML        string
	ReceivedAt  time.Time
}

// Store dedupes on message_id and stores the email.
func Store(ctx context.Context, sqldb *sql.DB, m Inbound) (bool, error) {
	if m.MessageID == "" || m.Recipient == "" {
		return false, fmt.Errorf("message_id and recipient required")
	}
	res, err := sqldb.ExecContext(ctx, `
INSERT OR IGNORE INTO inbound_emails(message_id,recipient,sender,subject,body_text,body_html,received_at)
VALUES(?,?,?,?,?,?,?)`,
		m.MessageID, strings.ToLower(m.Recipient), m.Sender, m.Subject,
		m.Text, m.HTML, m.ReceivedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("store inbound: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// PendingFor returns pending request emails matching this recipient.
func PendingFor(ctx context.Context, sqldb *sql.DB, recipient string) ([]string, error) {
	rows, err := sqldb.QueryContext(ctx,
		`SELECT email FROM requests WHERE email=? AND status='pending'`, strings.ToLower(recipient))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyKripiWebhook verifies HMAC-SHA256 over "timestamp.body" with 5-min replay guard.
func VerifyKripiWebhook(secret, timestamp, sigHeader string, body []byte, now time.Time) error {
	var unix int64
	if _, err := fmt.Sscanf(timestamp, "%d", &unix); err != nil {
		return fmt.Errorf("bad timestamp: %w", err)
	}
	if abs(now.Unix()-unix) > 300 {
		return fmt.Errorf("timestamp too old (replay guard)")
	}
	// sig header: "t=<ts>,v1=<hex>"
	v1 := ""
	for _, part := range strings.Split(sigHeader, ",") {
		if strings.HasPrefix(strings.TrimSpace(part), "v1=") {
			v1 = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "v1="))
		}
	}
	if v1 == "" {
		return fmt.Errorf("missing v1 signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", timestamp, string(body))
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(v1)
	if err != nil {
		return fmt.Errorf("bad v1 hex: %w", err)
	}
	if !hmac.Equal(got, expected) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
