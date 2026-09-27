package cmd

import (
	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/dashboard"
	"github.com/uchabokeria/autokey/internal/onramp"
	"github.com/uchabokeria/autokey/internal/ui"
)

var (
	dashboardBind string
	dashboardPort int
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Serve the web dashboard (SPA + JSON API)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		sec := secretsFromFile(a.paths.Secrets)
		bind := dashboardBind
		if bind == "" {
			bind = a.cfg.Hook.Bind
		}
		port := dashboardPort
		if port == 0 {
			port = a.cfg.Hook.Port + 1
		}
		s := &dashboard.Server{
			DB: a.sqldb, Bind: bind, Port: port,
			Bearer: sec["HOOK_BEARER"], AllowIP: a.cfg.Hook.IPAllow,
			LogFile: a.paths.LogsDir + "/autokey.jsonl",
			Onramp:  onramp.New(),
			Version: version,
		}
		ui.Banner()
		ui.Ok("dashboard on http://%s:%d (same bearer as hook)", bind, port)
		return s.Serve(a.ctx)
	},
}

func init() {
	dashboardCmd.Flags().StringVar(&dashboardBind, "bind", "", "bind address (default hook.bind)")
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 0, "port (default hook.port+1)")
	rootCmd.AddCommand(dashboardCmd)
}
