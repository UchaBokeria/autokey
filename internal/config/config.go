package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths for the autokey home directory (~/.autoApiKeys).
type Paths struct {
	Home       string
	ConfigFile string
	Secrets    string
	DB         string
	LogsDir    string
}

// HomePaths resolves ~/.autoApiKeys and its children.
func HomePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home dir: %w", err)
	}
	base := filepath.Join(home, ".autoApiKeys")
	return Paths{
		Home:       base,
		ConfigFile: filepath.Join(base, "config.yaml"),
		Secrets:    filepath.Join(base, "secrets.env"),
		DB:         filepath.Join(base, "autokey.db"),
		LogsDir:    filepath.Join(base, "logs"),
	}, nil
}

// Config mirrors spec §3 (PROPOSED, frozen at build).
type Config struct {
	Domain      string  `mapstructure:"domain"`
	EmailFormat string  `mapstructure:"email_format"`
	Provider    string  `mapstructure:"provider"` // display hint only; never implicit (no default provider; kripi|onramp|custom)
	Hook        Hook    `mapstructure:"hook"`
	Kripi       Kripi   `mapstructure:"kripi"`
	Onramp      Onramp  `mapstructure:"onramp"`
	Cloudflare  CF      `mapstructure:"cloudflare"`
	Poller      Poller  `mapstructure:"poller"`
	WalletAlert float64 `mapstructure:"wallet_alert_usd"`
}

// Hook holds the hook server security matrix.
type Hook struct {
	Bind      string   `mapstructure:"bind"`
	Port      int      `mapstructure:"port"`
	TokenFile string   `mapstructure:"bearer_token_file"`
	IPAllow   []string `mapstructure:"ip_allowlist"`
	MTLS      bool     `mapstructure:"mtls"`
}

// Kripi holds KripiCard provider defaults.
type Kripi struct {
	BaseURL    string  `mapstructure:"base_url"`
	DefaultBIN string  `mapstructure:"default_bin"`
	DefaultAmt float64 `mapstructure:"default_amount_usd"`
	MintCap    int     `mapstructure:"purchase_cap_per_run"`
}

// Onramp holds Onramp one-time-card defaults.
type Onramp struct {
	Product       string  `mapstructure:"product"` // visa|mastercard|paypal
	AmountUSD     float64 `mapstructure:"amount_usd"`
	DepositTicker string  `mapstructure:"deposit_ticker"`
}

// CF holds Cloudflare Email Routing settings.
type CF struct {
	ZoneID     string `mapstructure:"zone_id"`
	WorkerName string `mapstructure:"worker_name"`
}

// Poller holds the Gmail fallback poller settings.
type Poller struct {
	Enabled  bool `mapstructure:"gmail_fallback_enabled"`
	Interval int  `mapstructure:"interval_sec"`
}

// Defaults returns the default configuration.
func Defaults() Config {
	return Config{
		Domain:      "example.com",
		EmailFormat: "mmmDDMMYYYY-rand4",
		Provider:    "",
		Hook: Hook{
			Bind:      "127.0.0.1",
			Port:      8765,
			TokenFile: "secrets.env",
		},
		Kripi: Kripi{
			BaseURL:    "https://appapi.kripicard.com",
			DefaultBIN: "539502",
			DefaultAmt: 20,
			MintCap:    3,
		},
		Onramp: Onramp{
			Product: "mastercard", AmountUSD: 5,
			DepositTicker: "polygon/usdt",
		},
		Cloudflare:  CF{WorkerName: "autokey-inbox"},
		Poller:      Poller{Enabled: true, Interval: 60},
		WalletAlert: 50,
	}
}
