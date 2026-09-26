package flow

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/provider"
)

// KeyProducer generates keys for an email+card. Playwright impl is a stub in v1.
type KeyProducer interface {
	Generate(ctx context.Context, email string, card provider.Secrets, qty int) ([]string, error)
}

// StubProducer is the v1 placeholder until the X service is provided.
type StubProducer struct{}

func (StubProducer) Generate(_ context.Context, email string, _ provider.Secrets, qty int) ([]string, error) {
	return nil, fmt.Errorf("key producer not implemented (x service pending) for %s qty=%d", email, qty)
}

// Details is the result of generateDetails.
type Details struct {
	Email    string
	CardID   string
	Last4    string
	Provider string
}

// Deps wires generateDetails collaborators (all injectable).
// Providers maps provider name -> implementation. There is no implicit
// default: an empty provider name is an error unless exactly one
// provider is registered (single-provider setups stay ergonomic).
type Deps struct {
	DB        *sql.DB
	Providers map[string]provider.CardProvider
	Producer  KeyProducer
	Domain    string
	Service   string
	Mint      provider.MintParams
	MintCap   int // cap per run; unlimited is open (spec §18)
	Now       func() time.Time
	RequestID func() string
}

// resolveProvider requires an explicit provider unless exactly one exists.
func resolveProvider(d Deps, name string) (provider.CardProvider, string, error) {
	if name != "" {
		prov, ok := d.Providers[name]
		if !ok {
			return nil, "", fmt.Errorf("unknown provider %q", name)
		}
		return prov, name, nil
	}
	if len(d.Providers) == 1 {
		for pname, prov := range d.Providers {
			return prov, pname, nil
		}
	}
	return nil, "", fmt.Errorf("no provider given (available: %s)", providerNames(d))
}

func providerNames(d Deps) string {
	names := make([]string, 0, len(d.Providers))
	for n := range d.Providers {
		names = append(names, n)
	}
	return strings.Join(names, "|")
}

// GenerateDetails implements the hardened user pseudocode:
// unique email -> try full provider pool (claim + mark) -> mint up to MintCap fresh.
func GenerateDetails(ctx context.Context, d Deps, providerName string, keyQuantity int) (Details, error) {
	prov, providerName, err := resolveProvider(d, providerName)
	if err != nil {
		return Details{}, err
	}
	now := d.Now().UTC().Format(time.RFC3339)
	email, err := pool.CreateUniqueEmailUser(ctx, d.DB, d.Domain)
	if err != nil {
		return Details{}, err
	}
	reqID := d.RequestID()
	if _, err := d.DB.ExecContext(ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES(?,?,?,?,?, 'pending',?,?)`, reqID, email, d.Service, keyQuantity, providerName, now, now); err != nil {
		return Details{}, fmt.Errorf("insert request: %w", err)
	}
	fail := func(msg string, err error) (Details, error) {
		_, _ = d.DB.ExecContext(ctx,
			`UPDATE requests SET status='failed', error=?, updated_at=? WHERE id=?`,
			fmt.Sprintf("%s: %v", msg, err), d.Now().UTC().Format(time.RFC3339), reqID)
		return Details{}, fmt.Errorf("%s: %w", msg, err)
	}

	// 1. Try the provider pool, healthy-first, with claim + per-service marking.
	cards, err := pool.ListPool(ctx, d.DB, providerName, d.Service)
	if err != nil {
		return fail("list pool", err)
	}
	for _, c := range cards {
		release, err := pool.Claim(ctx, d.DB, c.ID)
		if err != nil {
			continue // grabbed concurrently; try next
		}
		detail, ok := tryCard(ctx, d, prov, providerName, reqID, email, c.ID, keyQuantity)
		_ = release(ctx)
		if ok {
			return detail, nil
		}
	}

	// 2. Mint fresh up to cap (skipped for pool-only providers).
	if !prov.CanMint() {
		return fail("no working card", fmt.Errorf("pool exhausted (custom cards are pool-only: add more via cards add)"))
	}
	mintCap := d.MintCap
	if mintCap <= 0 {
		mintCap = 3
	}
	mp := d.Mint
	mp.Email = email
	for i := 0; i < mintCap; i++ {
		ref, err := prov.Mint(ctx, mp)
		if err != nil {
			if pe := prov.Classify(err); pe != nil && !pe.Retryable {
				// Non-retryable (e.g. kripi 202/refund-pending): stop, never double-charge.
				return fail("mint non-retryable "+pe.Class, err)
			}
			continue
		}
		if err := pool.Register(ctx, d.DB, providerName, ref.ID, ref.Last4, ref.BIN, ref.Name); err != nil {
			return fail("register minted card", err)
		}
		detail, ok := tryCard(ctx, d, prov, providerName, reqID, email, ref.ID, keyQuantity)
		if ok {
			return detail, nil
		}
	}
	return fail("no working card", fmt.Errorf("pool exhausted and %d mints failed", mintCap))
}

// tryCard fetches live secrets, runs the producer, records stats, persists request+keys.
func tryCard(ctx context.Context, d Deps, prov provider.CardProvider, providerName, reqID, email, cardID string, qty int) (Details, bool) {
	var last4 string
	_ = d.DB.QueryRowContext(ctx, `SELECT last4 FROM cards WHERE card_id=?`, cardID).Scan(&last4)
	secrets, err := prov.Secrets(ctx, cardID)
	if err != nil {
		_ = pool.RecordResult(ctx, d.DB, cardID, d.Service, false, err.Error())
		if pe := prov.Classify(err); pe != nil && pe.Class == "clean-fail" {
			_ = prov.Quarantine(ctx, cardID) // quarantine declined cards only
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
	return Details{Email: email, CardID: cardID, Last4: last4, Provider: providerName}, true
}
