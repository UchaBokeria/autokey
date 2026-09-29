package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uchabokeria/autokey/internal/ui"
)

// servicesCmd manages automation target profiles (omegameta first).
// Plural on purpose: singular `service` owns systemd units.
var servicesCmd = &cobra.Command{
	Use:   "services",
	Short: "Automation target profiles (domains, flows)",
}

var servicesDomainCmd = &cobra.Command{
	Use:   "domain [name] [url]",
	Short: "Show or set a service base URL (e.g. services domain omegameta https://host)",
	Args:  cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := loadApp()
		if err != nil {
			return err
		}
		defer a.close()
		file := a.paths.ConfigFile
		if cfgFile != "" {
			file = cfgFile
		}
		v := viper.New()
		v.SetConfigFile(file)
		_ = v.ReadInConfig()
		if len(args) == 0 {
			all := v.GetStringMap("services")
			if jsonOut {
				raw, _ := json.MarshalIndent(all, "", "  ")
				fmt.Println(string(raw))
				return nil
			}
			if len(all) == 0 {
				ui.Info("no services configured — services domain omegameta https://host")
				return nil
			}
			for name, raw := range all {
				base := ""
				if m, ok := raw.(map[string]any); ok {
					base, _ = m["base_url"].(string)
				}
				ui.Info("%s -> %s", name, base)
			}
			return nil
		}
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if name == "" {
			return fmt.Errorf("service name required")
		}
		if len(args) == 1 {
			base := v.GetString("services." + name + ".base_url")
			if base == "" {
				return fmt.Errorf("no base_url for service %q", name)
			}
			ui.Info("%s -> %s", name, base)
			return nil
		}
		raw := strings.TrimSpace(args[1])
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid base URL %q (need http(s)://host)", raw)
		}
		v.Set("services."+name+".base_url", strings.TrimRight(raw, "/"))
		if err := v.WriteConfig(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		ui.Ok("services.%s.base_url -> %s", name, strings.TrimRight(raw, "/"))
		ui.Info("reminder: after devops publishes production, re-run this and test manually")
		return nil
	},
}

func init() {
	servicesCmd.AddCommand(servicesDomainCmd)
	rootCmd.AddCommand(servicesCmd)
}
