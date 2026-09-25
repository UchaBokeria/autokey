package pool

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// months maps month number to lowercase 3-letter abbreviation.
var months = []string{"", "jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec"}

// Clock is injectable for tests.
var Clock = time.Now

// UUID4 is injectable for tests.
var UUID4 = func() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func rand4() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// CreateUniqueEmailUser returns mmmDDMMYYYY-rand4@domain, e.g. jun01032026-a3f9@my.com.
// Retries while the address already exists in requests.
func CreateUniqueEmailUser(ctx context.Context, sqldb *sql.DB, domain string) (string, error) {
	now := Clock().UTC()
	prefix := fmt.Sprintf("%s%02d%02d%d", months[int(now.Month())], now.Day(), int(now.Month()), now.Year())
	for i := 0; i < 10; i++ {
		email := fmt.Sprintf("%s-%s@%s", prefix, rand4(), strings.ToLower(domain))
		var n int
		if err := sqldb.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM requests WHERE email=?`, email).Scan(&n); err != nil {
			return "", fmt.Errorf("check email uniqueness: %w", err)
		}
		if n == 0 {
			return email, nil
		}
	}
	return "", fmt.Errorf("could not mint unique email after 10 tries")
}

// Card is a pool row joined with per-service stats.
type Card struct {
	ID         string
	Last4      string
	BIN        string
	NameOnCard string
	OK         int
	Fail       int
}

// ListPool returns cards ordered by per-service success rate (healthy first).
func ListPool(ctx context.Context, sqldb *sql.DB, service string) ([]Card, error) {
	rows, err := sqldb.QueryContext(ctx, `
SELECT c.card_id, c.last4, c.bin, c.name_on_card,
       COALESCE(s.ok_count,0), COALESCE(s.fail_count,0)
FROM cards c LEFT JOIN card_service_stats s
  ON s.card_id=c.card_id AND s.service=?
WHERE c.status='active' AND c.claimed=0
ORDER BY (COALESCE(s.ok_count,0)+1.0)/(COALESCE(s.ok_count,0)+COALESCE(s.fail_count,0)+2.0) DESC,
         c.created_at ASC`, service)
	if err != nil {
		return nil, fmt.Errorf("list pool: %w", err)
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.Last4, &c.BIN, &c.NameOnCard, &c.OK, &c.Fail); err != nil {
			return nil, fmt.Errorf("scan pool: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Claim marks a card claimed inside a BEGIN IMMEDIATE transaction.
// Returns a release func; caller must call it.
func Claim(ctx context.Context, sqldb *sql.DB, cardID string) (release func(context.Context) error, err error) {
	tx, err := sqldb.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	var claimed int
	if err := tx.QueryRowContext(ctx, `SELECT claimed FROM cards WHERE card_id=?`, cardID).Scan(&claimed); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("read claim: %w", err)
	}
	if claimed != 0 {
		_ = tx.Rollback()
		return nil, fmt.Errorf("card %s already claimed", cardID)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET claimed=1 WHERE card_id=?`, cardID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}
	return func(ctx context.Context) error {
		_, err := sqldb.ExecContext(ctx, `UPDATE cards SET claimed=0 WHERE card_id=?`, cardID)
		return err
	}, nil
}

// RecordResult updates per-service stats.
func RecordResult(ctx context.Context, sqldb *sql.DB, cardID, service string, ok bool, errMsg string) error {
	now := Clock().UTC().Format(time.RFC3339)
	if ok {
		_, err := sqldb.ExecContext(ctx, `
INSERT INTO card_service_stats(card_id,service,ok_count,fail_count,last_error,updated_at)
VALUES(?,?,1,0,NULL,?)
ON CONFLICT(card_id,service) DO UPDATE SET ok_count=ok_count+1, last_error=NULL, updated_at=?`,
			cardID, service, now, now)
		return err
	}
	_, err := sqldb.ExecContext(ctx, `
INSERT INTO card_service_stats(card_id,service,ok_count,fail_count,last_error,updated_at)
VALUES(?,?,0,1,?,?)
ON CONFLICT(card_id,service) DO UPDATE SET fail_count=fail_count+1, last_error=?, updated_at=?`,
		cardID, service, errMsg, now, errMsg, now)
	return err
}

// Register adds a minted card to the pool.
func Register(ctx context.Context, sqldb *sql.DB, id, last4, bin, name string) error {
	_, err := sqldb.ExecContext(ctx, `
INSERT OR IGNORE INTO cards(card_id,last4,bin,name_on_card,status,claimed,created_at)
VALUES(?,?,?,?, 'active',0,?)`,
		id, last4, bin, name, Clock().UTC().Format(time.RFC3339))
	return err
}
