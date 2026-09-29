package custom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/uchabokeria/autokey/internal/provider"
)

// ProviderName is the registry key for user-supplied cards.
const ProviderName = "custom"

// Card holds a user-supplied card: non-sensitive metadata plus the
// encrypted PAN blob. The blob is opaque here — only DecryptSecrets
// (with the key from CUSTOM_CARD_KEY) can open it.
type Card struct {
	Label      string
	Last4      string
	BIN        string
	NameOnCard string
	EncBlob    string
	Salt       string
	Iterations int
}

// CardInput is the plaintext entry supplied by the user at add time.
type CardInput struct {
	Label      string
	Number     string
	Expiry     string
	CVV        string
	NameOnCard string
	// Country is the billing country (ISO-2, default US). Stored
	// plaintext in custom_cards.country for the billing step.
	Country string
}

// Error is a custom-provider failure. Adds are local, so failures are
// validation errors; nothing external can be retried.
type Error struct {
	Message string
}

func (e *Error) Error() string { return "custom: " + e.Message }

// digits strips everything but 0-9 (spaces/dashes in pasted PANs).
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Brand identifies the card network for length/CVV rules.
type Brand string

const (
	BrandVisa     Brand = "visa"
	BrandMaster   Brand = "mastercard"
	BrandAmex     Brand = "amex"
	BrandDiscover Brand = "discover"
	BrandDiners   Brand = "diners"
	BrandJCB      Brand = "jcb"
	BrandUnionPay Brand = "unionpay"
	BrandUnknown  Brand = "unknown"
)

// DetectBrand classifies a PAN by IIN ranges.
func DetectBrand(num string) Brand {
	d := digits(num)
	switch {
	case len(d) >= 1 && d[0] == '4':
		return BrandVisa
	case len(d) >= 2 && (d[:2] == "34" || d[:2] == "37"):
		return BrandAmex
	case len(d) >= 2 && d[:2] >= "51" && d[:2] <= "55":
		return BrandMaster
	case len(d) >= 4 && d[:4] >= "2221" && d[:4] <= "2720":
		return BrandMaster
	case hasAnyPrefix(d, []string{"6011", "65"}) ||
		(len(d) >= 3 && d[:3] >= "644" && d[:3] <= "649"):
		return BrandDiscover
	case hasAnyPrefix(d, []string{"300", "301", "302", "303", "304", "305", "36", "38", "39"}):
		return BrandDiners
	case len(d) >= 4 && d[:4] >= "3528" && d[:4] <= "3589":
		return BrandJCB
	case len(d) >= 2 && d[:2] == "62":
		return BrandUnionPay
	default:
		return BrandUnknown
	}
}

func hasAnyPrefix(d string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

// brandLengths maps brands to accepted PAN lengths.
func brandLengths(b Brand) []int {
	switch b {
	case BrandAmex:
		return []int{15}
	case BrandDiners:
		return []int{14, 16} // 14 native; 16 for network-partnered issues
	case BrandVisa:
		return []int{13, 16, 19}
	case BrandUnionPay:
		return []int{16, 17, 18, 19}
	case BrandMaster, BrandDiscover, BrandJCB:
		return []int{16}
	default:
		return []int{13, 14, 15, 16, 17, 18, 19}
	}
}

// Validate checks a card entry before it is stored. Rules are brand-aware:
// Amex takes 15 digits + 4-digit CID; Diners takes 14; the rest follow
// their network lengths with 3-digit CVV (Amex 4). Expiry accepts
// MM/YY, MM-YY, MM.YY, MMYY, and MM/YYYY.
func Validate(in CardInput) error {
	num := digits(in.Number)
	brand := DetectBrand(num)
	okLen := false
	for _, l := range brandLengths(brand) {
		if len(num) == l {
			okLen = true
			break
		}
	}
	if !okLen {
		return &Error{Message: fmt.Sprintf("card number length %d invalid for %s", len(num), brand)}
	}
	if !luhn(num) {
		return &Error{Message: "card number fails Luhn check"}
	}
	if _, err := normalizeExpiry(in.Expiry); err != nil {
		return err
	}
	cvv := digits(in.CVV)
	wantCVV := 3
	if brand == BrandAmex {
		wantCVV = 4
	}
	if len(cvv) != wantCVV {
		return &Error{Message: fmt.Sprintf("CVV must be %d digits for %s", wantCVV, brand)}
	}
	if strings.TrimSpace(in.Label) == "" {
		return &Error{Message: "label is required"}
	}
	return nil
}

// normalizeCountry uppercases and defaults blank to US.
func normalizeCountry(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if c == "" {
		return "US"
	}
	return c
}

// normalizeExpiry accepts MM/YY, MM-YY, MM.YY, MMYY, MM/YYYY and
// returns canonical MM/YY. Years map 00-99; four-digit years take mod 100.
func normalizeExpiry(exp string) (string, error) {
	e := strings.TrimSpace(exp)
	e = strings.ReplaceAll(e, "-", "/")
	e = strings.ReplaceAll(e, ".", "/")
	e = strings.ReplaceAll(e, " ", "")
	if strings.Contains(e, "/") {
		parts := strings.SplitN(e, "/", 2)
		mm, yy := parts[0], parts[1]
		if len(mm) != 2 || (len(yy) != 2 && len(yy) != 4) {
			return "", &Error{Message: "expiry must be MM/YY"}
		}
		if mm < "01" || mm > "12" {
			return "", &Error{Message: "expiry month must be 01-12"}
		}
		if !allDigits(mm) || !allDigits(yy) {
			return "", &Error{Message: "expiry must be MM/YY"}
		}
		if len(yy) == 4 {
			yy = yy[2:]
		}
		return mm + "/" + yy, nil
	}
	d := digits(e)
	if len(d) == 4 {
		mm := d[:2]
		if mm < "01" || mm > "12" {
			return "", &Error{Message: "expiry month must be 01-12"}
		}
		return mm + "/" + d[2:], nil
	}
	if len(d) == 6 {
		mm := d[:2]
		if mm < "01" || mm > "12" {
			return "", &Error{Message: "expiry month must be 01-12"}
		}
		return mm + "/" + d[4:], nil
	}
	return "", &Error{Message: "expiry must be MM/YY"}
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func luhn(num string) bool {
	sum := 0
	alt := false
	for i := len(num) - 1; i >= 0; i-- {
		d := int(num[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// last4Of returns the last 4 digits of a PAN.
func last4Of(num string) string {
	d := digits(num)
	if len(d) < 4 {
		return d
	}
	return d[len(d)-4:]
}

// binOf returns the first 6 digits (BIN/IIN) of a PAN.
func binOf(num string) string {
	d := digits(num)
	if len(d) < 6 {
		return d
	}
	return d[:6]
}

// deriveKey stretches the CUSTOM_CARD_KEY passphrase with PBKDF2-SHA256.
func deriveKey(passphrase string, salt []byte, iterations int) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, iterations, 32, sha256.New)
}

// EncryptBlob seals verbatim parts joined by "|" with AES-256-GCM.
// Unlike EncryptSecrets it applies no PAN normalization — for secrets
// like passwords that must round-trip exactly.
func EncryptBlob(passphrase string, parts ...string) (blob, salt string, iterations int, err error) {
	if passphrase == "" {
		return "", "", 0, &Error{Message: "CUSTOM_CARD_KEY is not set"}
	}
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", 0, fmt.Errorf("custom: rand: %w", err)
	}
	iterations = 210000
	key := deriveKey(passphrase, saltBytes, iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", 0, fmt.Errorf("custom: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", 0, fmt.Errorf("custom: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", 0, fmt.Errorf("custom: nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(strings.Join(parts, "|")), nil)
	return base64.StdEncoding.EncodeToString(sealed),
		base64.StdEncoding.EncodeToString(saltBytes), iterations, nil
}

// DecryptBlob opens a verbatim blob into its "|" parts.
func DecryptBlob(passphrase, blob, salt string, iterations int) ([]string, error) {
	if passphrase == "" {
		return nil, &Error{Message: "CUSTOM_CARD_KEY is not set"}
	}
	sealed, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, &Error{Message: "corrupt blob"}
	}
	saltBytes, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		return nil, &Error{Message: "corrupt salt"}
	}
	key := deriveKey(passphrase, saltBytes, iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("custom: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("custom: gcm: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, &Error{Message: "corrupt blob"}
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, &Error{Message: "decrypt failed (wrong key?)"}
	}
	return strings.Split(string(plain), "|"), nil
}

// EncryptSecrets seals number|expiry|cvv with AES-256-GCM. Returns the
// base64 blob, base64 salt, and iteration count for storage.
func EncryptSecrets(passphrase string, number, expiry, cvv string) (blob, salt string, iterations int, err error) {
	if passphrase == "" {
		return "", "", 0, &Error{Message: "CUSTOM_CARD_KEY is not set"}
	}
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", 0, fmt.Errorf("custom: rand: %w", err)
	}
	iterations = 210000
	key := deriveKey(passphrase, saltBytes, iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", 0, fmt.Errorf("custom: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", 0, fmt.Errorf("custom: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", 0, fmt.Errorf("custom: nonce: %w", err)
	}
	plain := strings.Join([]string{digits(number), strings.TrimSpace(expiry), digits(cvv)}, "|")
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed),
		base64.StdEncoding.EncodeToString(saltBytes), iterations, nil
}

// DecryptSecrets opens a stored blob. The returned Secrets live in
// memory only — callers must never log or persist them.
func DecryptSecrets(passphrase, blob, salt string, iterations int) (provider.Secrets, error) {
	if passphrase == "" {
		return provider.Secrets{}, &Error{Message: "CUSTOM_CARD_KEY is not set"}
	}
	sealed, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return provider.Secrets{}, &Error{Message: "corrupt blob"}
	}
	saltBytes, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		return provider.Secrets{}, &Error{Message: "corrupt salt"}
	}
	key := deriveKey(passphrase, saltBytes, iterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return provider.Secrets{}, fmt.Errorf("custom: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return provider.Secrets{}, fmt.Errorf("custom: gcm: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return provider.Secrets{}, &Error{Message: "corrupt blob"}
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return provider.Secrets{}, &Error{Message: "decrypt failed (wrong key?)"}
	}
	parts := strings.SplitN(string(plain), "|", 3)
	if len(parts) != 3 {
		return provider.Secrets{}, &Error{Message: "corrupt plaintext"}
	}
	return provider.Secrets{Number: parts[0], Expiry: parts[1], CVV: parts[2]}, nil
}

// Provider adapts user-supplied cards to provider.CardProvider.
// "Mint" means: validate + encrypt + register a card the user typed in.
// There is no external API, so Mint never moves funds.
type Provider struct {
	DB     *sql.DB
	Key    func() string // resolves CUSTOM_CARD_KEY at call time
	Now    func() string // RFC3339 timestamp, injectable for tests
	CardID func(label string) string
}

func (p *Provider) now() string {
	if p.Now != nil {
		return p.Now()
	}
	return ""
}

func (p *Provider) cardID(label string) string {
	if p.CardID != nil {
		return p.CardID(label)
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("custom_%x", b)
}

func (p *Provider) Name() string { return ProviderName }

// CanMint reports false: custom cards are user-supplied via cards add.
// Generate flows use pool cards only and never enter the mint loop.
func (p *Provider) CanMint() bool { return false }

// Mint validates the entry in MintParams and stores it encrypted.
// The card number/expiry/CVV arrive via MintParams extended fields:
// Name=label, Email is unused, Product carries "number|expiry|cvv".
// Callers should use Add() instead; Mint exists for the interface.
func (p *Provider) Mint(ctx context.Context, mp provider.MintParams) (provider.CardRef, error) {
	parts := strings.SplitN(mp.Product, "|", 3)
	if len(parts) != 3 {
		return provider.CardRef{}, &Error{Message: "custom mint needs Product as number|expiry|cvv (use cards add)"}
	}
	in := CardInput{Label: mp.Name, Number: parts[0], Expiry: parts[1], CVV: parts[2]}
	return p.Add(ctx, in)
}

// Add validates, encrypts, and registers a user-supplied card.
func (p *Provider) Add(ctx context.Context, in CardInput) (provider.CardRef, error) {
	if err := Validate(in); err != nil {
		return provider.CardRef{}, err
	}
	exp, err := normalizeExpiry(in.Expiry)
	if err != nil {
		return provider.CardRef{}, err
	}
	in.Expiry = exp
	key := ""
	if p.Key != nil {
		key = p.Key()
	}
	blob, salt, iters, err := EncryptSecrets(key, in.Number, in.Expiry, in.CVV)
	if err != nil {
		return provider.CardRef{}, err
	}
	num := digits(in.Number)
	id := p.cardID(in.Label)
	_, err = p.DB.ExecContext(ctx, `
INSERT INTO cards(card_id,last4,bin,name_on_card,provider,status,claimed,created_at)
VALUES(?,?,?,?, 'custom','active',0,?)`,
		id, last4Of(num), binOf(num), in.Label, p.now())
	if err != nil {
		return provider.CardRef{}, fmt.Errorf("custom: register: %w", err)
	}
	_, err = p.DB.ExecContext(ctx, `
INSERT INTO custom_cards(card_id,enc_blob,salt,iterations,country,created_at)
VALUES(?,?,?,?,?,?)`, id, blob, salt, iters, normalizeCountry(in.Country), p.now())
	if err != nil {
		_, _ = p.DB.ExecContext(ctx, `DELETE FROM cards WHERE card_id=?`, id)
		return provider.CardRef{}, fmt.Errorf("custom: store secrets: %w", err)
	}
	return provider.CardRef{ID: id, Last4: last4Of(num), BIN: binOf(num), Name: in.Label}, nil
}

// Secrets decrypts the stored blob for a custom card id.
func (p *Provider) Secrets(ctx context.Context, id string) (provider.Secrets, error) {
	var blob, salt string
	var iters int
	if err := p.DB.QueryRowContext(ctx,
		`SELECT enc_blob,salt,iterations FROM custom_cards WHERE card_id=?`, id).
		Scan(&blob, &salt, &iters); err != nil {
		return provider.Secrets{}, &Error{Message: "unknown custom card " + id}
	}
	key := ""
	if p.Key != nil {
		key = p.Key()
	}
	return DecryptSecrets(key, blob, salt, iters)
}

// Quarantine marks a custom card failed in the pool (no external API).
func (p *Provider) Quarantine(ctx context.Context, id string) error {
	_, err := p.DB.ExecContext(ctx,
		`UPDATE cards SET status='quarantined' WHERE card_id=? AND provider='custom'`, id)
	return err
}

// Classify maps errors to retry policy. Custom adds are local and
// deterministic: validation failures never retry; transport never happens.
func (p *Provider) Classify(err error) *provider.ProviderError {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return &provider.ProviderError{
			Provider: ProviderName, Class: "clean-fail", Message: err.Error(),
			Retryable: false,
		}
	}
	return &provider.ProviderError{Provider: ProviderName, Class: "transport", Message: err.Error(), Retryable: false}
}
