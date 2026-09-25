package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/setup"
	"github.com/uchabokeria/autokey/internal/ui"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "First-run wizard: dirs, secrets, install, completions, service",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Banner()
		ctx := context.Background()
		answers, err := setup.Wizard()
		if err != nil {
			return err
		}
		if dryRun {
			ui.Info("dry-run: would apply setup for domain %s", answers.Domain)
			return nil
		}
		if _, err := setup.Apply(ctx, answers); err != nil {
			return err
		}
		if answers.InstallBin {
			if err := setup.InstallSelf(); err != nil {
				ui.Warn("self-install: %v", err)
			}
		}
		setup.InstallCompletions(func(shell, dir string) error {
			switch shell {
			case "bash":
				f, err := os.Create(filepath.Join(dir, "autokey"))
				if err != nil {
					// system dir may need sudo; fall back to user dir
					home, _ := os.UserHomeDir()
					f, err = os.Create(filepath.Join(home, ".local/share/bash-completion/completions/autokey"))
					if err != nil {
						return err
					}
				}
				defer f.Close()
				return rootCmd.GenBashCompletion(f)
			case "fish":
				_ = os.MkdirAll(dir, 0o755)
				f, err := os.Create(filepath.Join(dir, "autokey.fish"))
				if err != nil {
					return err
				}
				defer f.Close()
				return rootCmd.GenFishCompletion(f, true)
			case "zsh":
				_ = os.MkdirAll(dir, 0o755)
				f, err := os.Create(filepath.Join(dir, "_autokey"))
				if err != nil {
					return err
				}
				defer f.Close()
				return rootCmd.GenZshCompletion(f)
			default:
				return nil
			}
		})
		if answers.EnableSvc {
			if err := setup.InstallService(); err != nil {
				ui.Warn("service: %v", err)
			}
		} else {
			ui.Info("service left disabled (default). Enable later: autokey service install")
		}
		ui.Ok("setup complete")
		return nil
	},
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return rootCmd.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd, completionCmd)
}
