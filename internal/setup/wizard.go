package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/uchabokeria/autokey/internal/config"
	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/ui"
)

// Answers collects the first-run wizard results.
type Answers struct {
	Domain      string
	CFToken     string
	ZoneID      string
	KripiKey    string
	CustomKey   string
	BIN         string
	Amount      float64
	Provider    string
	OnrampProd  string
	Bind        string
	AllowLAN    bool
	InstallBin  bool
	EnableSvc   bool
	WalletAlert float64
}

// Wizard runs the interactive first-run setup.
func Wizard() (Answers, error) {
	a := Answers{
		BIN: "539502", Amount: 20, Bind: "127.0.0.1",
		Provider: "", OnrampProd: "mastercard",
		InstallBin: true, WalletAlert: 50,
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Domain for wildcard emails").Value(&a.Domain).
				Validate(func(s string) error {
					if len(s) < 3 || !containsDot(s) {
						return fmt.Errorf("enter a real domain, e.g. my.com")
					}
					return nil
				}),
			huh.NewSelect[string]().Title("Preferred card provider (hint only, --provider still required)").Options(
				huh.NewOption("None (choose per command)", ""),
				huh.NewOption("Your own card (typed in, encrypted)", "custom"),
				huh.NewOption("Onramp Pay one-time ($5+)", "onramp"),
				huh.NewOption("KripiCard virtual (funded wallet)", "kripi"),
			).Value(&a.Provider),
			huh.NewInput().Title("Custom card encryption key (optional now, required to add own cards)").EchoMode(huh.EchoModePassword).
				Value(&a.CustomKey),
			huh.NewInput().Title("Cloudflare API token").EchoMode(huh.EchoModePassword).
				Value(&a.CFToken),
			huh.NewInput().Title("Cloudflare Zone ID").Value(&a.ZoneID),
			huh.NewInput().Title("KripiCard API key (optional)").EchoMode(huh.EchoModePassword).
				Value(&a.KripiKey),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Onramp product").Options(
				huh.NewOption("Mastercard (default)", "mastercard"),
				huh.NewOption("Visa", "visa"),
				huh.NewOption("PayPal", "paypal"),
			).Value(&a.OnrampProd),
			huh.NewInput().Title("Default mint BIN (kripi)").Value(&a.BIN),
			huh.NewInput().Title("Bind address (default localhost)").Value(&a.Bind),
			huh.NewConfirm().Title("Install to /usr/local/bin?").Value(&a.InstallBin),
			huh.NewConfirm().Title("Enable systemd --user service? (default off)").Value(&a.EnableSvc),
		),
	)
	if err := form.Run(); err != nil {
		return a, fmt.Errorf("wizard: %w", err)
	}
	return a, nil
}

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// Apply writes home dir, config, secrets, and initializes the DB.
func Apply(ctx context.Context, a Answers) (config.Paths, error) {
	paths, err := config.HomePaths()
	if err != nil {
		return paths, err
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return paths, fmt.Errorf("mkdir home: %w", err)
	}
	if err := os.MkdirAll(paths.LogsDir, 0o700); err != nil {
		return paths, fmt.Errorf("mkdir logs: %w", err)
	}
	cfg := fmt.Sprintf(`domain: %s
email_format: mmmDDMMYYYY-rand4
provider: %s
hook:
  bind: %s
  port: 8765
  bearer_token_file: secrets.env
  ip_allowlist: []
  mtls: false
kripi:
  base_url: https://appapi.kripicard.com
  default_bin: "%s"
  default_amount_usd: %.2f
  purchase_cap_per_run: 3
onramp:
  product: "%s"
  amount_usd: 5
  deposit_ticker: polygon/usdt
cloudflare:
  zone_id: "%s"
  worker_name: autokey-inbox
poller:
  gmail_fallback_enabled: true
  interval_sec: 60
wallet_alert_usd: %.2f
`, a.Domain, a.Provider, a.Bind, a.BIN, a.Amount, a.OnrampProd, a.ZoneID, a.WalletAlert)
	if err := os.WriteFile(paths.ConfigFile, []byte(cfg), 0o600); err != nil {
		return paths, fmt.Errorf("write config: %w", err)
	}
	var b [32]byte
	_, _ = rand.Read(b[:])
	bearer := hex.EncodeToString(b[:])
	customKeyLine := ""
	if a.CustomKey != "" {
		customKeyLine = fmt.Sprintf("CUSTOM_CARD_KEY=%s\n", a.CustomKey)
		a.CustomKey = "" // drop from memory once written
	}
	secrets := fmt.Sprintf(`KRIPI_API_KEY=%s
CF_API_TOKEN=%s
HOOK_BEARER=%s
KRIPI_WEBHOOK_SECRET=
GMAIL_APP_PASSWORD=
%s`, a.KripiKey, a.CFToken, bearer, customKeyLine)
	if err := os.WriteFile(paths.Secrets, []byte(secrets), 0o600); err != nil {
		return paths, fmt.Errorf("write secrets: %w", err)
	}
	sqldb, err := db.Open(ctx, paths.DB)
	if err != nil {
		return paths, err
	}
	_ = sqldb.Close()
	ui.Ok("home initialized at %s", paths.Home)
	return paths, nil
}
