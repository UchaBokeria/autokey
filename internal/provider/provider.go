package provider

import (
	"context"
	"fmt"
)

// CardRef is a provider-agnostic handle to a card/order in the pool.
type CardRef struct {
	// ID is the provider's identifier (kripi card_id, onramp redeem_id,
	// custom custom_<rand>).
	ID string
	// Last4 is display only; empty when unknown (onramp pre-redeem).
	Last4 string
	// BIN/brand hint; empty when unknown.
	BIN string
	// Name is the cardholder/product label.
	Name string
}

// Secrets carries spend credentials in memory only. Never log or persist.
// KripiCard fills PAN fields; Onramp fills RedeemLink/Code.
type Secrets struct {
	Number     string
	Expiry     string
	CVV        string
	RedeemLink string
	RedeemCode string
}

// MintParams carries provider-agnostic mint arguments.
type MintParams struct {
	AmountUSD float64
	Name      string
	Email     string
	// Product selects the onramp product (visa/mastercard/paypal);
	// for kripi this is ignored in favor of BIN.
	Product string
	// BIN selects the kripi BIN; ignored by onramp.
	BIN string
	// DOB is kripi-only (US/SG/UK BINs), YYYY-MM-DD.
	DOB string
	// DepositTicker is onramp-only (default polygon/usdt).
	DepositTicker string
	// PayPalEmail is onramp-only, required for paypal product.
	PayPalEmail string
}

// ProviderError classifies a provider failure for retry policy.
type ProviderError struct {
	Provider  string
	Class     string // clean-fail | non-retryable | rate-limited | pending
	Message   string
	Retryable bool
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Provider, e.Class, e.Message)
}

// CardProvider abstracts virtual-card backends (kripi, onramp, custom, ...).
type CardProvider interface {
	// Name returns the provider id: "kripi" | "onramp" | "custom".
	Name() string
	// CanMint reports whether Mint can create new cards/orders.
	// Pool-only providers (custom) return false: generate flows use
	// pool cards only and never enter the mint loop.
	CanMint() bool
	// Mint creates a card/order. May return a pending order (onramp unpaid).
	Mint(ctx context.Context, p MintParams) (CardRef, error)
	// Secrets fetches spend credentials. May fail when not ready (onramp unpaid).
	Secrets(ctx context.Context, id string) (Secrets, error)
	// Quarantine suspends a failing card. No-op when unsupported (onramp).
	Quarantine(ctx context.Context, id string) error
	// Classify maps a raw error to retry policy.
	Classify(err error) *ProviderError
}
