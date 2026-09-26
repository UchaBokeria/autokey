package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/uchabokeria/autokey/internal/config"
	"github.com/uchabokeria/autokey/internal/custom"
	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/flow"
	"github.com/uchabokeria/autokey/internal/kripi"
	"github.com/uchabokeria/autokey/internal/onramp"
	"github.com/uchabokeria/autokey/internal/pool"
	"github.com/uchabokeria/autokey/internal/provider"
	"github.com/uchabokeria/autokey/internal/ui"
)

// app is the shared runtime context for commands.
type app struct {
	ctx   context.Context
	paths config.Paths
	cfg   config.Config
	sqldb *sql.DB
}

func loadApp() (*app, error) {
	ctx := context.Background()
	paths, err := config.HomePaths()
	if err != nil {
		return nil, err
	}
	cfg := config.Defaults()
	v := viper.New()
	v.SetConfigFile(paths.ConfigFile)
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	}
	v.SetEnvPrefix("AUTOKEY")
	v.AutomaticEnv()
	if err := v.ReadInConfig(); err == nil {
		_ = v.Unmarshal(&cfg)
	}
	if _, err := os.Stat(paths.Home); err != nil {
		return nil, fmt.Errorf("not set up yet — run `autokey setup` first: %w", err)
	}
	sqldb, err := db.Open(ctx, paths.DB)
	if err != nil {
		return nil, err
	}
	return &app{ctx: ctx, paths: paths, cfg: cfg, sqldb: sqldb}, nil
}

func (a *app) close() {
	if a.sqldb != nil {
		_ = a.sqldb.Close()
	}
}

func secretsFromFile(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range splitLines(string(raw)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				out[line[:i]] = line[i+1:]
				break
			}
		}
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func (a *app) kripiClient() *kripi.Client {
	sec := secretsFromFile(a.paths.Secrets)
	c := kripi.New(a.cfg.Kripi.BaseURL, sec["KRIPI_API_KEY"])
	c.OnRateWait = func(scope string, wait time.Duration) {
		ui.Warn("kripicard rate-limited (scope=%s), waiting %s", scope, wait)
	}
	return c
}

// providers builds the CardProvider registry.
func (a *app) providers() map[string]provider.CardProvider {
	sec := secretsFromFile(a.paths.Secrets)
	return map[string]provider.CardProvider{
		"kripi":  kripi.NewProvider(a.kripiClient()),
		"onramp": onramp.NewProvider(),
		"custom": &custom.Provider{
			DB:  a.sqldb,
			Key: func() string { return sec["CUSTOM_CARD_KEY"] },
		},
	}
}

// providerNames lists registry keys for error messages.
func (a *app) providerNames() string {
	return "kripi|onramp|custom"
}

func (a *app) flowDeps() flow.Deps {
	return flow.Deps{
		DB:        a.sqldb,
		Providers: a.providers(),
		Producer:  flow.StubProducer{},
		Domain:    a.cfg.Domain,
		Service:   "x",
		Mint: provider.MintParams{
			AmountUSD:     a.cfg.Onramp.AmountUSD,
			Name:          "autokey",
			Product:       a.cfg.Onramp.Product,
			BIN:           a.cfg.Kripi.DefaultBIN,
			DepositTicker: a.cfg.Onramp.DepositTicker,
		},
		MintCap:   a.cfg.Kripi.MintCap,
		Now:       time.Now,
		RequestID: pool.UUID4,
	}
}

// resolveProvider requires --provider unless exactly one provider exists.
// Config default is only a display hint, never an implicit selection.
func (a *app) resolveProvider(flag string) (provider.CardProvider, string, error) {
	provs := a.providers()
	if flag != "" {
		p, ok := provs[flag]
		if !ok {
			return nil, "", fmt.Errorf("unknown provider %q (available: %s)", flag, a.providerNames())
		}
		return p, flag, nil
	}
	if len(provs) == 1 {
		for name, p := range provs {
			return p, name, nil
		}
	}
	hint := ""
	if a.cfg.Provider != "" {
		hint = fmt.Sprintf(" (hint: %s)", a.cfg.Provider)
	}
	return nil, "", fmt.Errorf("no provider given%s — use --provider %s", hint, a.providerNames())
}
