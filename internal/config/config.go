// Package config resolves onclaw configuration in priority order:
//
//	defaults  <  config file  <  ONCLAW_* env vars  <  CLI flags
//
// The file is searched as "config.yaml" in (cwd, ~/.config/onclaw, /etc/onclaw)
// unless an explicit path is passed to Load. Env vars use the ONCLAW_ prefix
// with "." and "-" mapped to "_" (e.g. log_level -> ONCLAW_LOG_LEVEL).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the resolved onclaw configuration.
type Config struct {
	LogLevel         string `mapstructure:"log_level"`
	LogFormat        string `mapstructure:"log_format"`
	Concurrency      int    `mapstructure:"concurrency"`
	MaxContextTokens int    `mapstructure:"max_context_tokens"`
	Model            string `mapstructure:"model"`

	// Runtime-only metadata (populated by Load, not read from the file).
	LoadedFrom   string   `mapstructure:"-"`
	SearchPaths  []string `mapstructure:"-"`
}

// SearchPaths returns the directories onclaw searches for config.yaml, in order.
func SearchPaths() []string {
	paths := []string{"."} // current working directory
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "onclaw"))
	}
	paths = append(paths, "/etc/onclaw")
	return paths
}

// Load resolves the configuration. If explicitPath is non-empty it is used
// verbatim (extension determines type); otherwise the standard search paths
// are consulted. A missing config file is not an error — defaults + env apply.
func Load(explicitPath string) (*Config, error) {
	v := viper.New()

	d := defaults()
	v.SetDefault("log_level", d.LogLevel)
	v.SetDefault("log_format", d.LogFormat)
	v.SetDefault("concurrency", d.Concurrency)
	v.SetDefault("max_context_tokens", d.MaxContextTokens)
	v.SetDefault("model", d.Model)

	v.SetEnvPrefix("ONCLAW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	cfg := &Config{SearchPaths: SearchPaths()}

	switch {
	case explicitPath != "":
		v.SetConfigFile(explicitPath)
	default:
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		for _, p := range cfg.SearchPaths {
			v.AddConfigPath(p)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			cfg.LoadedFrom = "" // no file; defaults + env only
		} else {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else {
		cfg.LoadedFrom = v.ConfigFileUsed()
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
