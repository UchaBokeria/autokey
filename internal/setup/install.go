package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/uchabokeria/autokey/internal/ui"
)

// InstallSelf copies the running binary to /usr/local/bin/autokey (sudo only for the copy).
func InstallSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	dst := "/usr/local/bin/autokey"
	if exe == dst {
		ui.Info("already installed at %s", dst)
		return nil
	}
	cmd := exec.Command("sudo", "install", "-m", "0755", exe, dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	ui.Info("installing to %s (sudo required for this step only)...", dst)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install to %s: %w", dst, err)
	}
	ui.Ok("installed %s", dst)
	return nil
}

// InstallCompletions writes shell completions for detected shells.
func InstallCompletions(gen func(shell, dir string) error) {
	home, _ := os.UserHomeDir()
	targets := []struct{ shell, dir string }{
		{"bash", "/usr/share/bash-completion/completions"},
		{"fish", filepath.Join(home, ".config/fish/completions")},
		{"zsh", filepath.Join(home, ".zsh/completions")},
	}
	for _, t := range targets {
		if _, err := exec.LookPath(t.shell); err != nil {
			continue
		}
		if err := gen(t.shell, t.dir); err != nil {
			ui.Warn("completion for %s: %v", t.shell, err)
			continue
		}
		ui.Ok("completion installed for %s", t.shell)
	}
}

// ServiceUnit returns the systemd --user unit text.
func ServiceUnit(exe, home string) string {
	return fmt.Sprintf(`[Unit]
Description=autokey hook server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%s hook serve
Restart=on-failure
Environment=HOME=%s

[Install]
WantedBy=default.target
`, exe, home)
}

// InstallService writes + enables the systemd --user unit. Opt-in only.
func InstallService() error {
	home, _ := os.UserHomeDir()
	exe := "/usr/local/bin/autokey"
	if _, err := os.Stat(exe); err != nil {
		exe, _ = os.Executable()
	}
	unitDir := filepath.Join(home, ".config/systemd/user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return fmt.Errorf("mkdir units: %w", err)
	}
	unit := filepath.Join(unitDir, "autokey.service")
	if err := os.WriteFile(unit, []byte(ServiceUnit(exe, home)), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "autokey.service"},
	} {
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("systemctl %v: %w (WSL1 has no systemd — run `autokey hook serve` manually)", args, err)
		}
	}
	ui.Ok("service enabled (systemd --user)")
	return nil
}
