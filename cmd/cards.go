package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
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
		cards, err := pool.ListPool(a.ctx, a.sqldb, service)
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
			ui.Info("%s  last4=%s bin=%s ok=%d fail=%d", c.ID, c.Last4, c.BIN, c.OK, c.Fail)
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
)

var cardsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Mint a card via provider and register it in the pool",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		name := cardProvider
		if name == "" {
			name = a.cfg.Provider
		}
		prov, ok := a.providers()[name]
		if !ok {
			return fmt.Errorf("unknown provider %q (kripi|onramp)", name)
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
		if dryRun {
			ui.Info("dry-run: would mint via %s amount=%.2f", name, mp.AmountUSD)
			return nil
		}
		ref, err := prov.Mint(a.ctx, mp)
		if err != nil {
			return err
		}
		if err := pool.Register(a.ctx, a.sqldb, ref.ID, ref.Last4, ref.BIN, ref.Name); err != nil {
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
	Short: "Fund a card (fee $1.00 + 4%)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
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
	Short: "Show live card details (balance, status)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
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
	Short: "Freeze or unfreeze a card",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
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
	Short: "Delete a card (cashes balance minus $2 fee)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
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

func init() {
	cardsListCmd.Flags().StringP("service", "s", "x", "service for per-service stats")
	cardsCreateCmd.Flags().StringVar(&cardProvider, "provider", "", "kripi|onramp (default config)")
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
	cardsCmd.AddCommand(cardsListCmd, cardsCreateCmd, cardsFundCmd, cardsDetailsCmd, cardsFreezeCmd, cardsDeleteCmd)
	rootCmd.AddCommand(cardsCmd)
}
