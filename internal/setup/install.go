package setup

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/uchabokeria/autokey/internal/ui"
)

//go:embed tunnel.sh
var tunnelScript string

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
Restart=always
RestartSec=5
Environment=HOME=%s

[Install]
WantedBy=default.target
`, exe, home)
}

// TunnelUnit returns the ingress unit. When a named-tunnel config exists
// (~/.config/autokey/tunnel.yml) the unit runs it directly (stable
// hostname, no repoint needed). Otherwise it runs the embedded quick
// tunnel + worker-repoint script (self-healing across reboots).
func TunnelUnit(scriptPath string) string {
	return fmt.Sprintf(`[Unit]
Description=autokey ingress: quick tunnel + worker repoint (self-healing)
After=network-online.target autokey.service
Wants=network-online.target
BindsTo=autokey.service

[Service]
Type=simple
Environment=HOME=%s
ExecStart=%s
Restart=always
RestartSec=15

[Install]
WantedBy=default.target
`, os.Getenv("HOME"), scriptPath)
}

// TunnelScriptPath writes the embedded tunnel script and returns its path.
func TunnelScriptPath(home string) (string, error) {
	dir := filepath.Join(home, ".config", "autokey")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir autokey conf: %w", err)
	}
	path := filepath.Join(dir, "autokey-tunnel.sh")
	if err := os.WriteFile(path, []byte(tunnelScript), 0o755); err != nil {
		return "", fmt.Errorf("write tunnel script: %w", err)
	}
	return path, nil
}

// InstallService writes + enables the hook and tunnel units.
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
	scriptPath, err := TunnelScriptPath(home)
	if err != nil {
		return err
	}
	tunit := filepath.Join(unitDir, "autokey-tunnel.service")
	if err := os.WriteFile(tunit, []byte(TunnelUnit(scriptPath)), 0o644); err != nil {
		return fmt.Errorf("write tunnel unit: %w", err)
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "autokey.service"},
		{"--user", "enable", "--now", "autokey-tunnel.service"},
	} {
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("systemctl %v: %w (WSL1 has no systemd — run `autokey hook serve` manually)", args, err)
		}
	}
	ui.Ok("service + ingress enabled (systemd --user)")
	return nil
}
