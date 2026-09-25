package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/flow"
	"github.com/uchabokeria/autokey/internal/hook"
	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/ui"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Run the secure hook server",
}

var hookBind string
var hookPort int

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
	keyEmail string
	keyCard  string
	keyQty   int
)

var keysGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run generateDetails + generateKeys + wireup",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		deps := a.flowDeps()
		if keyEmail != "" {
			// Direct single-shot with explicit email+card.
			prod := deps.Producer
			secrets, _, _, err := deps.Kripi.Details(a.ctx, keyCard)
			if err != nil {
				return err
			}
			keys, err := prod.Generate(a.ctx, keyEmail, secrets, keyQty)
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
		detail, err := flow.GenerateDetails(a.ctx, deps, keyQty)
		if err != nil {
			return err
		}
		ui.Ok("email=%s card=%s last4=%s", detail.Email, detail.CardID, detail.Last4)
		return nil
	},
}

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Inbound email operations",
}

var inboxLimit int

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
		defer rows.Close()
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
	Short: "Watch inbound email for one address (Gmail fallback poll)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("gmail fallback poller: not wired yet (spec §18 open: IMAP vs OAuth)")
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Cloudflare Email Routing worker",
}

var workerDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the catch-all inbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("worker deploy: Cloudflare REST wiring pending (template embedded)")
	},
}

var workerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show worker/route status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("worker status: Cloudflare REST wiring pending")
	},
}

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "systemd user service (opt-in daemon)",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and enable systemd --user unit",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("service install: pending setup implementation")
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Disable and remove the unit",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("service uninstall: pending setup implementation")
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("service status: pending setup implementation")
	},
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail service logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("service logs: pending setup implementation")
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
	keysCmd.AddCommand(keysGenerateCmd)
	rootCmd.AddCommand(keysCmd)

	inboxListCmd.Flags().IntVarP(&inboxLimit, "limit", "n", 20, "max rows")
	inboxCmd.AddCommand(inboxListCmd, inboxWatchCmd)
	rootCmd.AddCommand(inboxCmd)

	workerCmd.AddCommand(workerDeployCmd, workerStatusCmd)
	rootCmd.AddCommand(workerCmd)

	serviceCmd.AddCommand(serviceInstallCmd, serviceUninstallCmd, serviceStatusCmd, serviceLogsCmd)
	rootCmd.AddCommand(serviceCmd)

	_ = pool.UUID4 // keep pool import if unused in future edits
}
