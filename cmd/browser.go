package cmd

import (
	pw "github.com/mxschmitt/playwright-go"
	"github.com/spf13/cobra"

	"github.com/uchabokeria/autokey/internal/ui"
)

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Browser automation runtime (playwright driver + browsers)",
}

var browserInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the playwright driver (one-time, outside the binary)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Info("installing playwright driver (node runtime, one-time)...")
		if err := pw.Install(); err != nil {
			return err
		}
		ui.Ok("playwright driver installed")
		ui.Info("browsers resolve: --executable, system chrome channel, or `playwright install chromium`")
		return nil
	},
}

func init() {
	browserCmd.AddCommand(browserInstallCmd)
	rootCmd.AddCommand(browserCmd)
}
