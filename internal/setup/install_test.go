package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceUnit(t *testing.T) {
	u := ServiceUnit("/usr/local/bin/autokey", "/home/u")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/autokey hook serve",
		"Restart=always",
		"Environment=HOME=/home/u",
		"WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("unit missing %q:\n%s", want, u)
		}
	}
}

func TestTunnelUnitAndScript(t *testing.T) {
	u := TunnelUnit("/home/u/.config/autokey/autokey-tunnel.sh")
	for _, want := range []string{
		"BindsTo=autokey.service",
		"Restart=always",
		"autokey-tunnel.sh",
		"Environment=HOME=",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("tunnel unit missing %q:\n%s", want, u)
		}
	}
	home := t.TempDir()
	os.Setenv("HOME", home)
	path, err := TunnelScriptPath(home)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(home, ".config", "autokey") {
		t.Fatalf("bad script path %q", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tunnel --url", "worker deploy --inbox-url", "trycloudflare"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("script missing %q", want)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("script not executable: %v %v", path, err)
	}
}
