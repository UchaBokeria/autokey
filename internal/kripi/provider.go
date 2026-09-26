package kripi

import (
	"context"

	"github.com/uchabokeria/autokey/internal/provider"
)

// Provider adapts the KripiCard client to provider.CardProvider.
type Provider struct {
	C *Client
}

// NewProvider wraps an existing client.
func NewProvider(c *Client) *Provider { return &Provider{C: c} }

func (p *Provider) Name() string { return "kripi" }

// Mint issues a funded virtual card (wallet debited atomically).
func (p *Provider) Mint(ctx context.Context, mp provider.MintParams) (provider.CardRef, error) {
	bin := mp.BIN
	if bin == "" {
		bin = "539502"
	}
	card, err := p.C.CreateCard(ctx, bin, mp.AmountUSD, mp.Name, mp.Email, mp.DOB)
	if err != nil {
		return provider.CardRef{}, err
	}
	return provider.CardRef{
		ID: card.ID, Last4: card.Last4, BIN: card.BIN, Name: card.NameOnCard,
	}, nil
}

// Secrets fetches live PAN material.
func (p *Provider) Secrets(ctx context.Context, id string) (provider.Secrets, error) {
	s, _, _, err := p.C.Details(ctx, id)
	if err != nil {
		return provider.Secrets{}, err
	}
	return provider.Secrets{Number: s.Number, Expiry: s.Expiry, CVV: s.CVV}, nil
}

// Quarantine freezes issuer-declined cards; caller decides when.
func (p *Provider) Quarantine(ctx context.Context, id string) error {
	return p.C.Freeze(ctx, id, true)
}

// Classify maps CardError to provider policy.
func (p *Provider) Classify(err error) *provider.ProviderError {
	if err == nil {
		return nil
	}
	if cerr, ok := err.(*CardError); ok {
		class := "clean-fail"
		switch cerr.Class {
		case PendingRefunded:
			class = "pending-refunded"
		case RefundPending:
			class = "refund-pending"
		case RateLimited:
			class = "rate-limited"
		case OperationInFlight:
			class = "operation-in-flight"
		}
		return &provider.ProviderError{
			Provider: "kripi", Class: class, Message: cerr.Message,
			Retryable: cerr.Retryable(),
		}
	}
	return &provider.ProviderError{Provider: "kripi", Class: "transport", Message: err.Error(), Retryable: true}
}
