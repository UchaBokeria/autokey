package cmd

import (
	"github.com/spf13/cobra"
)

var (
	verbose   bool
	jsonOut   bool
	cfgFile   string
	assumeYes bool
	dryRun    bool
)

var rootCmd = &cobra.Command{
	Use:   "autokey",
	Short: "Wildcard inbox + virtual-card pool + key pipeline in one binary",
	Long: `autokey — neon cyberpunk CLI for automated key generation.

Mint unique wildcard emails, pull working virtual cards from a
SQLite pool (or mint fresh via KripiCard), and drive the key
pipeline behind a secure hook.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.autoApiKeys/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&assumeYes, "yes", "y", false, "assume yes for prompts")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview without mutating")
}
