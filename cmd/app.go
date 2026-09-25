package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/uchabokeria/autokey/internal/config"
	"github.com/uchabokeria/autokey/internal/db"
	"github.com/uchabokeria/autokey/internal/flow"
	"github.com/uchabokeria/autokey/internal/kripi"
	"github.com/uchabokeria/autokey/internal/pool"
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

func (a *app) flowDeps() flow.Deps {
	return flow.Deps{
		DB:         a.sqldb,
		Kripi:      a.kripiClient(),
		Producer:   flow.StubProducer{},
		Domain:     a.cfg.Domain,
		Service:    "x",
		DefaultBIN: a.cfg.Kripi.DefaultBIN,
		DefaultAmt: a.cfg.Kripi.DefaultAmt,
		CardName:   "autokey",
		MintCap:    a.cfg.Kripi.MintCap,
		Now:        time.Now,
		RequestID:  pool.UUID4,
	}
}
