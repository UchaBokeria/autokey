package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/flow"
	"github.com/uchabokeria/autokey/internal/hook"
	"github.com/uchabokeria/autokey/internal/mail"
	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/ui"
	"github.com/uchabokeria/autokey/internal/worker"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Run the secure hook server",
}

var (
	hookBind string
	hookPort int
)

var hookServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Listen for generate + inbox requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		sec := secretsFromFile(a.paths.Secrets)
		bind := hookBind
		if bind == "" {
			bind = a.cfg.Hook.Bind
		}
		port := hookPort
		if port == 0 {
			port = a.cfg.Hook.Port
		}
		s := &hook.Server{
			DB: a.sqldb, Deps: a.flowDeps(),
			Bind: bind, Port: port,
			Bearer: sec["HOOK_BEARER"], AllowIP: a.cfg.Hook.IPAllow,
			LogFile: a.paths.LogsDir + "/autokey.jsonl",
		}
		ui.Banner()
		ui.Ok("hook listening on %s:%d", bind, port)
		return s.Serve(a.ctx)
	},
}

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Key pipeline operations",
}

var (
	keyEmail    string
	keyCard     string
	keyQty      int
	keyProvider string
)

var keysGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate API keys: mint email + working card, create keys, print result",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		deps := a.flowDeps()
		prov, providerName, err := a.resolveProvider(keyProvider)
		if err != nil {
			return err
		}
		if keyEmail != "" {
			// Direct single-shot with explicit email+card.
			secrets, err := prov.Secrets(a.ctx, keyCard)
			if err != nil {
				return err
			}
			keys, err := deps.Producer.Generate(a.ctx, keyEmail, secrets, keyQty)
			if err != nil {
				return err
			}
			if jsonOut {
				raw, _ := json.Marshal(map[string]any{"email": keyEmail, "keys": keys})
				fmt.Println(string(raw))
				return nil
			}
			ui.Ok("generated %d keys for %s", len(keys), keyEmail)
			return nil
		}
		if keyQty < 1 || keyQty > 100 {
			return fmt.Errorf("keyQuantity must be 1..100")
		}
		detail, err := flow.GenerateDetails(a.ctx, deps, providerName, keyQty)
		if err != nil {
			return err
		}
		ui.Ok("email=%s card=%s last4=%s provider=%s", detail.Email, detail.CardID, detail.Last4, detail.Provider)
		return nil
	},
}

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Inbound email operations",
}

var emailCmd = &cobra.Command{
	Use:   "email",
	Short: "Unique email user operations",
}

var emailMintCmd = &cobra.Command{
	Use:   "mint",
	Short: "Mint one unique email user and reserve it (no card, no keys)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		email, err := pool.CreateUniqueEmailUser(a.ctx, a.sqldb, a.cfg.Domain)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := a.sqldb.ExecContext(a.ctx, `
INSERT INTO requests(id,email,service,key_quantity,provider,status,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)`, pool.UUID4(), email, "x", 0, "", "reserved", now, now); err != nil {
			return fmt.Errorf("reserve email: %w", err)
		}
		if jsonOut {
			raw, _ := json.Marshal(map[string]any{"email": email, "status": "reserved"})
			fmt.Println(string(raw))
			return nil
		}
		ui.Ok("minted %s (reserved)", email)
		return nil
	},
}

var inboxLimit int

var (
	watchAddr   string
	watchOnce   bool
	watchPeriod int
)

var (
	otpEmail   string
	otpTimeout time.Duration
	otpLatest  bool
)

var inboxOTPCmd = &cobra.Command{
	Use:   "otp",
	Short: "Read a one-time code for an email user (wait or latest)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		if otpEmail == "" {
			return fmt.Errorf("need --email user@domain")
		}
		if otpLatest {
			r, ok, err := mail.LatestOTP(a.ctx, a.sqldb, otpEmail, "")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no code found for %s", otpEmail)
			}
			printOTP(r)
			return nil
		}
		if otpTimeout <= 0 {
			otpTimeout = 2 * time.Minute
		}
		ui.Info("waiting up to %s for OTP to %s...", otpTimeout, otpEmail)
		r, err := mail.WaitForOTP(a.ctx, a.sqldb, otpEmail, otpTimeout)
		if err != nil {
			return err
		}
		printOTP(r)
		return nil
	},
}

func printOTP(r mail.OTPResult) {
	if jsonOut {
		raw, _ := json.Marshal(map[string]any{
			"email": r.Recipient, "code": r.Code,
			"subject": r.Subject, "from": r.Sender, "received_at": r.ReceivedAt,
		})
		fmt.Println(string(raw))
		return
	}
	ui.Ok("OTP for %s: %s", r.Recipient, r.Code)
	ui.Dim("from=%s subj=%q at=%s", r.Sender, r.Subject, r.ReceivedAt)
}

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent inbound emails",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		rows, err := a.sqldb.QueryContext(a.ctx,
			`SELECT message_id,recipient,sender,subject,received_at FROM inbound_emails ORDER BY received_at DESC LIMIT ?`, inboxLimit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, rcpt, from, subj, at string
			_ = rows.Scan(&id, &rcpt, &from, &subj, &at)
			ui.Info("%s -> %s from=%s subj=%q", at, rcpt, from, subj)
		}
		return rows.Err()
	},
}

var inboxWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Poll Gmail fallback inbox (IMAP) for new mail",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		sec := secretsFromFile(a.paths.Secrets)
		cfg := mail.PollConfig{
			Username: watchAddr,
			Password: sec["GMAIL_APP_PASSWORD"],
		}
		if cfg.Username == "" {
			cfg.Username = sec["GMAIL_ADDRESS"]
		}
		if cfg.Username == "" || cfg.Password == "" {
			return fmt.Errorf("need Gmail address (--addr or GMAIL_ADDRESS) + GMAIL_APP_PASSWORD in secrets.env")
		}
		if watchOnce {
			n, err := mail.PollOnce(a.ctx, a.sqldb, cfg)
			if err != nil {
				return err
			}
			ui.Ok("stored %d new messages", n)
			return nil
		}
		if watchPeriod <= 0 {
			watchPeriod = a.cfg.Poller.Interval
		}
		ui.Info("polling %s every %ds (Ctrl-C to stop)", cfg.Username, watchPeriod)
		t := time.NewTicker(time.Duration(watchPeriod) * time.Second)
		defer t.Stop()
		for {
			n, err := mail.PollOnce(a.ctx, a.sqldb, cfg)
			if err != nil {
				ui.Warn("poll: %v", err)
			} else if n > 0 {
				ui.Ok("stored %d new messages", n)
			}
			select {
			case <-a.ctx.Done():
				return nil
			case <-t.C:
			}
		}
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Cloudflare Email Routing worker",
}

var workerDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Enable routing DNS, upload inbox worker, set catch-all",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		sec := secretsFromFile(a.paths.Secrets)
		token := sec["CF_API_TOKEN"]
		if token == "" {
			return fmt.Errorf("CF_API_TOKEN missing in secrets.env (run autokey setup)")
		}
		accountID, _ := cmd.Flags().GetString("account")
		if accountID == "" {
			accountID = sec["CF_ACCOUNT_ID"]
		}
		if accountID == "" {
			return fmt.Errorf("need --account or CF_ACCOUNT_ID in secrets.env")
		}
		inboxURL, _ := cmd.Flags().GetString("inbox-url")
		if inboxURL == "" {
			inboxURL = fmt.Sprintf("http://%s:%d/v1/inbox", a.cfg.Hook.Bind, a.cfg.Hook.Port)
		}
		fallback, _ := cmd.Flags().GetString("fallback")
		bearer := sec["HOOK_BEARER"]
		if bearer == "" {
			return fmt.Errorf("HOOK_BEARER missing in secrets.env")
		}
		deps := worker.New(token, accountID, a.cfg.Cloudflare.ZoneID, a.cfg.Cloudflare.WorkerName)
		if dryRun {
			ui.Info("dry-run: would upload %s, enable DNS, set catch-all", deps.ScriptName)
			return nil
		}
		ui.Info("enabling Email Routing DNS...")
		if err := deps.EnableRoutingDNS(a.ctx); err != nil {
			ui.Warn("dns: %v", err)
		}
		ui.Info("uploading worker %s...", deps.ScriptName)
		if err := deps.UploadScript(a.ctx, inboxURL, bearer, fallback); err != nil {
			return err
		}
		if fallback != "" {
			ui.Info("registering fallback destination %s (verify via email!)...", fallback)
			if err := deps.CreateDestination(a.ctx, fallback); err != nil {
				ui.Warn("destination: %v", err)
			}
		}
		ui.Info("setting catch-all -> worker...")
		if err := deps.SetCatchAllWorker(a.ctx); err != nil {
			return err
		}
		ui.Ok("worker deployed, catch-all -> %s", deps.ScriptName)
		return nil
	},
}

var workerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Email Routing + catch-all status",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		sec := secretsFromFile(a.paths.Secrets)
		accountID := sec["CF_ACCOUNT_ID"]
		deps := worker.New(sec["CF_API_TOKEN"], accountID, a.cfg.Cloudflare.ZoneID, a.cfg.Cloudflare.WorkerName)
		routing, catchAll, err := deps.Status(a.ctx)
		if err != nil {
			return err
		}
		ui.Info("routing: %s", routing)
		ui.Info("catch-all: %s", catchAll)
		return nil
	},
}

func init() {
	hookServeCmd.Flags().StringVar(&hookBind, "bind", "", "bind address (default config)")
	hookServeCmd.Flags().IntVar(&hookPort, "port", 0, "port (default config)")
	hookCmd.AddCommand(hookServeCmd)
	rootCmd.AddCommand(hookCmd)

	keysGenerateCmd.Flags().StringVar(&keyEmail, "email", "", "explicit email (skip mint)")
	keysGenerateCmd.Flags().StringVar(&keyCard, "card", "", "card ID for explicit mode")
	keysGenerateCmd.Flags().IntVarP(&keyQty, "qty", "n", 1, "keyQuantity 1..100")
	keysGenerateCmd.Flags().StringVar(&keyProvider, "provider", "", "card provider kripi|onramp|custom (required)")
	keysCmd.AddCommand(keysGenerateCmd)
	rootCmd.AddCommand(keysCmd)

	inboxListCmd.Flags().IntVarP(&inboxLimit, "limit", "n", 20, "max rows")
	inboxWatchCmd.Flags().StringVar(&watchAddr, "addr", "", "Gmail address (default GMAIL_ADDRESS)")
	inboxWatchCmd.Flags().BoolVar(&watchOnce, "once", false, "single poll then exit")
	inboxWatchCmd.Flags().IntVar(&watchPeriod, "interval", 0, "seconds between polls (default config)")
	inboxOTPCmd.Flags().StringVar(&otpEmail, "email", "", "email user to read the code for (required)")
	inboxOTPCmd.Flags().DurationVar(&otpTimeout, "timeout", 2*time.Minute, "how long to wait for a new code")
	inboxOTPCmd.Flags().BoolVar(&otpLatest, "latest", false, "print newest stored code instead of waiting")
	inboxCmd.AddCommand(inboxListCmd, inboxWatchCmd, inboxOTPCmd)
	rootCmd.AddCommand(inboxCmd)

	emailCmd.AddCommand(emailMintCmd)
	rootCmd.AddCommand(emailCmd)

	workerDeployCmd.Flags().String("account", "", "Cloudflare account ID (or CF_ACCOUNT_ID)")
	workerDeployCmd.Flags().String("inbox-url", "", "autokey /v1/inbox URL (default bind:port)")
	workerDeployCmd.Flags().String("fallback", "", "fallback forward address (Gmail)")
	workerCmd.AddCommand(workerDeployCmd, workerStatusCmd)
	rootCmd.AddCommand(workerCmd)
}
