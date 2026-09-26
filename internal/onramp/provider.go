package onramp

import (
	"context"
	"fmt"
	"strings"

	"github.com/uchabokeria/autokey/internal/provider"
)

// Provider adapts the Onramp one-time-card API to provider.CardProvider.
type Provider struct {
	C *Client
}

// NewProvider returns an Onramp CardProvider.
func NewProvider() *Provider { return &Provider{C: New()} }

func (p *Provider) Name() string { return "onramp" }

// Mint checks stock then creates the order. The order is unpaid until the
// customer sends crypto; Mint never moves funds.
func (p *Provider) Mint(ctx context.Context, mp provider.MintParams) (provider.CardRef, error) {
	product := mp.Product
	if product == "" {
		product = "mastercard"
	}
	stock, err := p.C.Stock(ctx)
	if err != nil {
		return provider.CardRef{}, err
	}
	prod, ok := stock[strings.ToLower(product)]
	if !ok {
		return provider.CardRef{}, &Error{Message: "unknown product " + product, BadRequest: true}
	}
	if strings.ToLower(prod.Status) != "available" {
		return provider.CardRef{}, &Error{
			Message: fmt.Sprintf("product %s %s", product, prod.Status), OutOfStock: true,
		}
	}
	if mp.AmountUSD < prod.Min || mp.AmountUSD > prod.Max {
		return provider.CardRef{}, &Error{
			Message:    fmt.Sprintf("amount %.2f outside %.2f-%.2f", mp.AmountUSD, prod.Min, prod.Max),
			BadRequest: true,
		}
	}
	ticker := mp.DepositTicker
	order, err := p.C.CreateOrder(ctx, strings.ToLower(product), mp.AmountUSD, ticker, mp.PayPalEmail)
	if err != nil {
		return provider.CardRef{}, err
	}
	if order.RedeemID == "" {
		return provider.CardRef{}, &Error{Message: "empty redeem_id from provider"}
	}
	return provider.CardRef{
		ID: order.RedeemID, Name: prod.Brand,
	}, nil
}

// Secrets returns redeem credentials once payment + issuance complete.
func (p *Provider) Secrets(ctx context.Context, id string) (provider.Secrets, error) {
	s, err := p.C.CheckStatus(ctx, id)
	if err != nil {
		return provider.Secrets{}, err
	}
	if strings.ToLower(s.PaymentStatus) != "paid" {
		return provider.Secrets{}, &Error{Message: "order unpaid: " + s.PaymentStatus}
	}
	if strings.ToLower(s.IssuerStatus) != "completed" {
		return provider.Secrets{}, &Error{Message: "issuer pending: " + s.IssuerStatus}
	}
	return provider.Secrets{RedeemLink: s.RedeemLink}, nil
}

// Quarantine is a no-op: one-time orders need no suspension.
func (p *Provider) Quarantine(_ context.Context, _ string) error { return nil }

// Classify maps errors to retry policy.
func (p *Provider) Classify(err error) *provider.ProviderError {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		class := "clean-fail"
		if e.OutOfStock {
			class = "out-of-stock"
		}
		return &provider.ProviderError{
			Provider: "onramp", Class: class, Message: e.Message,
			Retryable: true, // order creation is free; safe to retry/fall through
		}
	}
	return &provider.ProviderError{Provider: "onramp", Class: "transport", Message: err.Error(), Retryable: true}
}
