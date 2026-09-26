package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uchabokeria/autokey/internal/onramp"
	"github.com/uchabokeria/autokey/internal/ui"
)

var onrampCmd = &cobra.Command{
	Use:   "onramp",
	Short: "Onramp Pay one-time cards (default provider)",
}

var onrampStockCmd = &cobra.Command{
	Use:   "stock",
	Short: "Show live product stock + limits",
	RunE: func(cmd *cobra.Command, args []string) error {
		stock, err := onramp.New().Stock(cmd.Context())
		if err != nil {
			return err
		}
		if jsonOut {
			raw, _ := json.MarshalIndent(stock, "", "  ")
			fmt.Println(string(raw))
			return nil
		}
		for _, p := range stock {
			ui.Info("%s (%s): %s min=%.2f max=%.2f tickers=%d",
				p.Provider, p.Brand, p.Status, p.Min, p.Max, len(p.Tickers))
		}
		return nil
	},
}

var onrampRedeemID string

var onrampStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check order payment + issuer status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if onrampRedeemID == "" {
			return fmt.Errorf("need --redeem-id")
		}
		s, err := onramp.New().CheckStatus(cmd.Context(), onrampRedeemID)
		if err != nil {
			return err
		}
		ui.Info("payment=%s issuer=%s redeem=%s", s.PaymentStatus, s.IssuerStatus, s.RedeemLink)
		return nil
	},
}

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Card provider selection",
}

var providerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List providers + current default",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		for name := range a.providers() {
			mark := " "
			if name == a.cfg.Provider {
				mark = "*"
			}
			ui.Info("%s %s", mark, name)
		}
		return nil
	},
}

var providerSetDefaultCmd = &cobra.Command{
	Use:   "set-default [kripi|onramp]",
	Short: "Change the default card provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name != "kripi" && name != "onramp" {
			return fmt.Errorf("unknown provider %q (kripi|onramp)", name)
		}
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		file := a.paths.ConfigFile
		if cfgFile != "" {
			file = cfgFile
		}
		v := viper.New()
		v.SetConfigFile(file)
		_ = v.ReadInConfig()
		v.Set("provider", name)
		if err := v.WriteConfig(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		ui.Ok("default provider -> %s", name)
		return nil
	},
}

func init() {
	onrampStatusCmd.Flags().StringVar(&onrampRedeemID, "redeem-id", "", "order redeem_id")
	onrampCmd.AddCommand(onrampStockCmd, onrampStatusCmd)
	rootCmd.AddCommand(onrampCmd)

	providerCmd.AddCommand(providerListCmd, providerSetDefaultCmd)
	rootCmd.AddCommand(providerCmd)
}
