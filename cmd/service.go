package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/uchabokeria/autokey/internal/setup"
	"github.com/uchabokeria/autokey/internal/ui"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "systemd user service (opt-in daemon)",
}

var serviceInstallRealCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and enable systemd --user unit (opt-in)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return setup.InstallService()
	},
}

var serviceUninstallRealCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Disable and remove the unit",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, args := range [][]string{
			{"--user", "disable", "--now", "autokey.service"},
		} {
			c := exec.Command("systemctl", args...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("systemctl %v: %w", args, err)
			}
		}
		home, _ := os.UserHomeDir()
		unit := home + "/.config/systemd/user/autokey.service"
		_ = os.Remove(unit)
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		ui.Ok("service removed")
		return nil
	},
}

var serviceStatusRealCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service status",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := exec.Command("systemctl", "--user", "is-active", "autokey.service")
		out, err := c.Output()
		fmt.Printf("autokey.service: %s", string(out))
		return err
	},
}

var serviceLogsRealCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail service logs (file + journal)",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		n, _ := cmd.Flags().GetInt("lines")
		f, err := os.Open(a.paths.LogsDir + "/autokey.jsonl")
		if err != nil {
			ui.Warn("no log file yet: %v", err)
		} else {
			defer f.Close()
			var lines []string
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				lines = append(lines, sc.Text())
				if len(lines) > n {
					lines = lines[1:]
				}
			}
			for _, l := range lines {
				fmt.Println(l)
			}
		}
		j := exec.Command("journalctl", "--user", "-u", "autokey.service", "-n", fmt.Sprint(n), "--no-pager")
		j.Stdout = os.Stdout
		j.Stderr = os.Stderr
		_ = j.Run()
		return nil
	},
}

func init() {
	serviceLogsRealCmd.Flags().IntP("lines", "n", 50, "lines to show")
	serviceCmd.AddCommand(serviceInstallRealCmd, serviceUninstallRealCmd, serviceStatusRealCmd, serviceLogsRealCmd)
	rootCmd.AddCommand(serviceCmd)
}
