package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/custom"
	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/provider"
	"github.com/uchabokeria/autokey/internal/ui"
)

var cardsCmd = &cobra.Command{
	Use:   "cards",
	Short: "Manage the virtual-card pool",
}

var cardsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pool cards with per-service stats",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		service, _ := cmd.Flags().GetString("service")
		filter, _ := cmd.Flags().GetString("provider")
		if filter != "" {
			if _, ok := a.providers()[filter]; !ok {
				return fmt.Errorf("unknown provider %q (available: %s)", filter, a.providerNames())
			}
		}
		cards, err := pool.ListPool(a.ctx, a.sqldb, filter, service)
		if err != nil {
			return err
		}
		if jsonOut {
			raw, _ := json.MarshalIndent(cards, "", "  ")
			fmt.Println(string(raw))
			return nil
		}
		if len(cards) == 0 {
			ui.Info("pool is empty for service %q", service)
			return nil
		}
		for _, c := range cards {
			ui.Info("%s [%s] last4=%s bin=%s ok=%d fail=%d", c.ID, c.Provider, c.Last4, c.BIN, c.OK, c.Fail)
		}
		return nil
	},
}

var (
	cardBIN         string
	cardAmount      float64
	cardName        string
	cardEmail       string
	cardDOB         string
	cardID          string
	cardProvider    string
	cardProduct     string
	cardTicker      string
	cardPayPalEmail string
	customLabel     string
	customNumber    string
	customExpiry    string
	customCVV       string
	customName      string
)

// promptIfEmpty asks on the TTY (masked for secrets) when a flag was omitted.
func promptIfEmpty(val, prompt string, secret bool) (string, error) {
	if val != "" {
		return val, nil
	}
	if secret {
		return ui.PromptSecret(prompt)
	}
	fmt.Fprint(os.Stderr, prompt)
	var line string
	if _, err := fmt.Scanln(&line); err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// runCardsAddCustom implements `cards create --provider custom` and
// `cards add`: label + masked number/expiry/CVV entry, Luhn-checked,
// AES-256-GCM sealed with CUSTOM_CARD_KEY, metadata in pool.
func runCardsAddCustom(cmd *cobra.Command, a *app) error {
	sec := secretsFromFile(a.paths.Secrets)
	if sec["CUSTOM_CARD_KEY"] == "" {
		return fmt.Errorf("CUSTOM_CARD_KEY is not set in secrets.env — run: autokey setup (or add it manually, 0600)")
	}
	var err error
	label, err := promptIfEmpty(customLabel, "Label: ", false)
	if err != nil {
		return err
	}
	number, err := promptIfEmpty(customNumber, "Card number: ", true)
	if err != nil {
		return err
	}
	expiry, err := promptIfEmpty(customExpiry, "Expiry MM/YY: ", true)
	if err != nil {
		return err
	}
	cvv, err := promptIfEmpty(customCVV, "CVV: ", true)
	if err != nil {
		return err
	}
	name := customName
	if name == "" {
		name = label
	}
	p, ok := a.providers()["custom"]
	if !ok {
		return fmt.Errorf("custom provider not registered")
	}
	cp, ok := p.(*custom.Provider)
	if !ok {
		return fmt.Errorf("custom provider miswired")
	}
	if dryRun {
		in := custom.CardInput{Label: label, Number: number, Expiry: expiry, CVV: cvv}
		if err := custom.Validate(in); err != nil {
			return err
		}
		ui.Info("dry-run: custom card %q validates", label)
		return nil
	}
	ref, err := cp.Add(a.ctx, custom.CardInput{
		Label: label, Number: number, Expiry: expiry, CVV: cvv, NameOnCard: name,
	})
	if err != nil {
		return err
	}
	_ = cmd
	ui.Ok("custom card %q registered as %s (last4=%s)", label, ref.ID, ref.Last4)
	return nil
}

var cardsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Mint a card via provider and register it in the pool (custom = your own card)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		prov, name, err := a.resolveProvider(cardProvider)
		if err != nil {
			return err
		}
		mp := provider.MintParams{
			AmountUSD: cardAmount, Name: cardName, Email: cardEmail,
			Product: cardProduct, BIN: cardBIN, DOB: cardDOB,
			DepositTicker: cardTicker, PayPalEmail: cardPayPalEmail,
		}
		if name == "kripi" {
			if mp.BIN == "" {
				mp.BIN = a.cfg.Kripi.DefaultBIN
			}
			if mp.AmountUSD == 0 {
				mp.AmountUSD = a.cfg.Kripi.DefaultAmt
			}
		} else {
			if mp.Product == "" {
				mp.Product = a.cfg.Onramp.Product
			}
			if mp.AmountUSD == 0 {
				mp.AmountUSD = a.cfg.Onramp.AmountUSD
			}
			if mp.DepositTicker == "" {
				mp.DepositTicker = a.cfg.Onramp.DepositTicker
			}
		}
		if name == "custom" {
			return runCardsAddCustom(cmd, a)
		}
		if dryRun {
			ui.Info("dry-run: would mint via %s amount=%.2f", name, mp.AmountUSD)
			return nil
		}
		ref, err := prov.Mint(a.ctx, mp)
		if err != nil {
			return err
		}
		if err := pool.Register(a.ctx, a.sqldb, name, ref.ID, ref.Last4, ref.BIN, ref.Name); err != nil {
			return err
		}
		ui.Ok("minted %s (%s) — registered in pool", ref.ID, name)
		if name == "onramp" {
			ui.Info("pay the exact crypto amount, then: autokey onramp status --redeem-id %s", ref.ID)
		}
		return nil
	},
}

var cardsFundCmd = &cobra.Command{
	Use:   "fund",
	Short: "Fund a KripiCard (fee $1.00 + 4%)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "" && p != "kripi" {
			return fmt.Errorf("card %s belongs to provider %q — fund is kripi-only", cardID, p)
		}
		if dryRun {
			ui.Info("dry-run: would fund %s amount=%.2f", cardID, cardAmount)
			return nil
		}
		if err := a.kripiClient().Fund(a.ctx, cardID, cardAmount); err != nil {
			return err
		}
		ui.Ok("funded %s", cardID)
		return nil
	},
}

var cardsDetailsCmd = &cobra.Command{
	Use:   "details",
	Short: "Show live KripiCard details (balance, status)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "" && p != "kripi" {
			if p == "custom" {
				return fmt.Errorf("card %s is your own card — no live balance API (fund/freeze/delete are kripi-only)", cardID)
			}
			return fmt.Errorf("card %s belongs to provider %q — use: autokey onramp status --redeem-id %s", cardID, p, cardID)
		}
		// NOTE: PAN/CVV deliberately never printed.
		_, balance, status, err := a.kripiClient().Details(a.ctx, cardID)
		if err != nil {
			return err
		}
		ui.Info("%s balance=%.2f status=%s", cardID, balance, status)
		return nil
	},
}

var freezeAction string

var cardsFreezeCmd = &cobra.Command{
	Use:   "freeze",
	Short: "Freeze or unfreeze a KripiCard",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "" && p != "kripi" {
			return fmt.Errorf("card %s belongs to provider %q — freeze is kripi-only", cardID, p)
		}
		freeze := freezeAction != "unfreeze"
		if dryRun {
			ui.Info("dry-run: would set %s frozen=%v", cardID, freeze)
			return nil
		}
		if err := a.kripiClient().Freeze(a.ctx, cardID, freeze); err != nil {
			return err
		}
		ui.Ok("%s frozen=%v", cardID, freeze)
		return nil
	},
}

var cardsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a KripiCard (cashes balance minus $2 fee)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "" && p != "kripi" {
			return fmt.Errorf("card %s belongs to provider %q — delete is kripi-only", cardID, p)
		}
		if !assumeYes {
			return fmt.Errorf("refusing without -y/--yes (irreversible)")
		}
		if dryRun {
			ui.Info("dry-run: would delete %s", cardID)
			return nil
		}
		refunded, fee, err := a.kripiClient().Delete(a.ctx, cardID)
		if err != nil {
			return err
		}
		ui.Ok("deleted %s refunded=%.2f fee=%.2f", cardID, refunded, fee)
		return nil
	},
}

var cardsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add your own card (masked entry, encrypted at rest)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		return runCardsAddCustom(cmd, a)
	},
}

var cardsRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a custom card from the pool (deletes secrets)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if cardID == "" {
			return fmt.Errorf("need --id")
		}
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "custom" {
			return fmt.Errorf("card %s belongs to provider %q — remove is custom-only (kripi: cards delete)", cardID, p)
		}
		if !assumeYes {
			return fmt.Errorf("refusing without -y/--yes (deletes secrets)")
		}
		if dryRun {
			ui.Info("dry-run: would remove custom card %s", cardID)
			return nil
		}
		if _, err := a.sqldb.ExecContext(a.ctx, `DELETE FROM custom_cards WHERE card_id=?`, cardID); err != nil {
			return err
		}
		if _, err := a.sqldb.ExecContext(a.ctx, `DELETE FROM cards WHERE card_id=?`, cardID); err != nil {
			return err
		}
		ui.Ok("removed custom card %s", cardID)
		return nil
	},
}

var cardsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show a custom card's number (decrypts to terminal, never logged)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if cardID == "" {
			return fmt.Errorf("need --id")
		}
		if p := pool.ProviderOf(a.ctx, a.sqldb, cardID); p != "custom" {
			return fmt.Errorf("card %s belongs to provider %q — show is custom-only", cardID, p)
		}
		p, ok := a.providers()["custom"]
		if !ok {
			return fmt.Errorf("custom provider not registered")
		}
		cp, ok := p.(*custom.Provider)
		if !ok {
			return fmt.Errorf("custom provider miswired")
		}
		s, err := cp.Secrets(a.ctx, cardID)
		if err != nil {
			return err
		}
		// Deliberately plain print: the user asked to see their own card.
		// Never goes through the JSONL hook log (no request involved).
		fmt.Printf("number: %s\nexpiry: %s\ncvv: %s\n", s.Number, s.Expiry, s.CVV)
		return nil
	},
}

func init() {
	cardsListCmd.Flags().StringP("service", "s", "x", "service for per-service stats")
	cardsListCmd.Flags().String("provider", "", "filter by provider: kripi|onramp|custom (empty = all)")
	cardsCreateCmd.Flags().StringVar(&cardProvider, "provider", "", "kripi|onramp|custom (required)")
	cardsCreateCmd.Flags().StringVar(&customLabel, "label", "", "custom card label")
	cardsCreateCmd.Flags().StringVar(&customNumber, "number", "", "custom PAN (prompted masked if omitted)")
	cardsCreateCmd.Flags().StringVar(&customExpiry, "expiry", "", "custom expiry MM/YY (prompted masked if omitted)")
	cardsCreateCmd.Flags().StringVar(&customCVV, "cvv", "", "custom CVV (prompted masked if omitted)")
	cardsCreateCmd.Flags().StringVar(&customName, "card-name", "", "custom cardholder name (default label)")
	cardsCreateCmd.Flags().StringVar(&cardProduct, "product", "", "onramp product: visa|mastercard|paypal")
	cardsCreateCmd.Flags().StringVar(&cardTicker, "ticker", "", "onramp deposit coin (default polygon/usdt)")
	cardsCreateCmd.Flags().StringVar(&cardPayPalEmail, "paypal-email", "", "onramp paypal product email")
	cardsCreateCmd.Flags().StringVar(&cardBIN, "bin", "", "kripi BIN (default config)")
	cardsCreateCmd.Flags().Float64Var(&cardAmount, "amount", 0, "USD amount (kripi min 10, onramp min 5)")
	cardsCreateCmd.Flags().StringVar(&cardName, "name", "autokey", "cardholder name")
	cardsCreateCmd.Flags().StringVar(&cardEmail, "email", "", "cardholder email")
	cardsCreateCmd.Flags().StringVar(&cardDOB, "dob", "", "YYYY-MM-DD (US/SG/UK BINs)")
	cardsFundCmd.Flags().StringVar(&cardID, "id", "", "card ID")
	cardsFundCmd.Flags().Float64Var(&cardAmount, "amount", 10, "USD amount (min 10)")
	cardsDetailsCmd.Flags().StringVar(&cardID, "id", "", "card ID")
	cardsFreezeCmd.Flags().StringVar(&cardID, "id", "", "card ID")
	cardsFreezeCmd.Flags().StringVar(&freezeAction, "action", "freeze", "freeze|unfreeze")
	cardsDeleteCmd.Flags().StringVar(&cardID, "id", "", "card ID")
	cardsAddCmd.Flags().StringVar(&customLabel, "label", "", "custom card label")
	cardsAddCmd.Flags().StringVar(&customNumber, "number", "", "custom PAN (prompted masked if omitted)")
	cardsAddCmd.Flags().StringVar(&customExpiry, "expiry", "", "custom expiry MM/YY (prompted masked if omitted)")
	cardsAddCmd.Flags().StringVar(&customCVV, "cvv", "", "custom CVV (prompted masked if omitted)")
	cardsAddCmd.Flags().StringVar(&customName, "card-name", "", "custom cardholder name (default label)")
	cardsRemoveCmd.Flags().StringVar(&cardID, "id", "", "custom card ID")
	cardsShowCmd.Flags().StringVar(&cardID, "id", "", "custom card ID")
	cardsCmd.AddCommand(cardsListCmd, cardsCreateCmd, cardsAddCmd, cardsRemoveCmd, cardsShowCmd, cardsFundCmd, cardsDetailsCmd, cardsFreezeCmd, cardsDeleteCmd)
	rootCmd.AddCommand(cardsCmd)
}
