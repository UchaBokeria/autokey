package flow

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uchabokeria/autokey/internal/kripi"
	"github.com/uchabokeria/autokey/internal/pool"
)

// KeyProducer generates keys for an email+card. Playwright impl is a stub in v1.
type KeyProducer interface {
	Generate(ctx context.Context, email string, card kripi.CardSecrets, qty int) ([]string, error)
}

// StubProducer is the v1 placeholder until the X service is provided.
type StubProducer struct{}

func (StubProducer) Generate(_ context.Context, email string, _ kripi.CardSecrets, qty int) ([]string, error) {
	return nil, fmt.Errorf("key producer not implemented (x service pending) for %s qty=%d", email, qty)
}

// Details is the result of generateDetails.
type Details struct {
	Email  string
	CardID string
	Last4  string
}

// Deps wires generateDetails collaborators (all injectable).
type Deps struct {
	DB          *sql.DB
	Kripi       *kripi.Client
	Producer    KeyProducer
	Domain      string
	Service     string
	DefaultBIN  string
	DefaultAmt  float64
	CardName    string
	MintCap     int // PROPOSED cap 3; unlimited is open (spec §18)
	Now         func() time.Time
	RequestID   func() string
}

// GenerateDetails implements the hardened user pseudocode:
// unique email -> try full pool (claim + mark) -> mint up to MintCap fresh.
func GenerateDetails(ctx context.Context, d Deps, keyQuantity int) (Details, error) {
	now := d.Now().UTC().Format(time.RFC3339)
	email, err := pool.CreateUniqueEmailUser(ctx, d.DB, d.Domain)
	if err != nil {
		return Details{}, err
	}
	reqID := d.RequestID()
	if _, err := d.DB.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,status,created_at,updated_at)
VALUES(?,?,?,?, 'pending',?,?)`, reqID, email, d.Service, keyQuantity, now, now); err != nil {
		return Details{}, fmt.Errorf("insert request: %w", err)
	}
	fail := func(msg string, err error) (Details, error) {
		_, _ = d.DB.ExecContext(ctx,
			`UPDATE requests SET status='failed', error=?, updated_at=? WHERE id=?`,
			fmt.Sprintf("%s: %v", msg, err), d.Now().UTC().Format(time.RFC3339), reqID)
		return Details{}, fmt.Errorf("%s: %w", msg, err)
	}

	// 1. Try the full pool, healthy-first, with claim + per-service marking.
	cards, err := pool.ListPool(ctx, d.DB, d.Service)
	if err != nil {
		return fail("list pool", err)
	}
	for _, c := range cards {
		release, err := pool.Claim(ctx, d.DB, c.ID)
		if err != nil {
			continue // grabbed concurrently; try next
		}
		detail, ok := tryCard(ctx, d, reqID, email, c.ID, keyQuantity)
		_ = release(ctx)
		if ok {
			return detail, nil
		}
	}

	// 2. Mint fresh up to cap.
	mintCap := d.MintCap
	if mintCap <= 0 {
		mintCap = 3
	}
	for i := 0; i < mintCap; i++ {
		card, err := d.Kripi.CreateCard(ctx, d.DefaultBIN, d.DefaultAmt, d.CardName, email, "")
		if cerr, isCard := err.(*kripi.CardError); err != nil && isCard {
			if !cerr.Retryable() {
				// 202 / refund-pending / rate-limited at mint: stop, never double-charge.
				return fail("mint card non-retryable "+cerr.Class.String(), err)
			}
			continue
		} else if err != nil {
			continue
		}
		if err := pool.Register(ctx, d.DB, card.ID, card.Last4, card.BIN, card.NameOnCard); err != nil {
			return fail("register minted card", err)
		}
		detail, ok := tryCard(ctx, d, reqID, email, card.ID, keyQuantity)
		if ok {
			return detail, nil
		}
	}
	return fail("no working card", fmt.Errorf("pool exhausted and %d mints failed", mintCap))
}

// tryCard fetches live PAN, runs the producer, records stats, persists request+keys.
func tryCard(ctx context.Context, d Deps, reqID, email, cardID string, qty int) (Details, bool) {
	// Live details needed for the producer.
	var last4 string
	_ = d.DB.QueryRowContext(ctx, `SELECT last4 FROM cards WHERE card_id=?`, cardID).Scan(&last4)
	secrets, _, _, err := d.Kripi.Details(ctx, cardID)
	if err != nil {
		_ = pool.RecordResult(ctx, d.DB, cardID, d.Service, false, err.Error())
		if cerr, ok := err.(*kripi.CardError); ok && cerr.Class == kripi.CleanFail {
			_ = d.Kripi.Freeze(ctx, cardID, true) // quarantine issuer-declined card only
		}
		return Details{}, false
	}
	keys, err := d.Producer.Generate(ctx, email, secrets, qty)
	if err != nil {
		_ = pool.RecordResult(ctx, d.DB, cardID, d.Service, false, err.Error())
		return Details{}, false
	}
	if err := pool.RecordResult(ctx, d.DB, cardID, d.Service, true, ""); err != nil {
		return Details{}, false
	}
	now := d.Now().UTC().Format(time.RFC3339)
	if _, err := d.DB.ExecContext(ctx,
		`UPDATE requests SET status='done', card_id=?, updated_at=? WHERE id=?`, cardID, now, reqID); err != nil {
		return Details{}, false
	}
	for _, k := range keys {
		if _, err := d.DB.ExecContext(ctx,
			`INSERT INTO keys(id,request_id,key_value,created_at) VALUES(?,?,?,?)`,
			d.RequestID(), reqID, k, now); err != nil {
			return Details{}, false
		}
	}
	return Details{Email: email, CardID: cardID, Last4: last4}, true
}
