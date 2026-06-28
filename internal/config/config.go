// Package config resolves onclaw configuration in priority order:
//
//	defaults  <  .env file  <  ONCLAW_* env vars  <  CLI flags
//
// The file is searched as ".env" in (cwd, ~/.onclaw, /etc/onclaw)
// unless an explicit path is passed to Load. Env vars use the ONCLAW_ prefix
// with "." and "-" mapped to "_" (e.g. log_level -> ONCLAW_LOG_LEVEL).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Config is the resolved onclaw configuration.
type Config struct {
	LogLevel         string         `mapstructure:"log_level"`
	LogFormat        string         `mapstructure:"log_format"`
	Concurrency      int            `mapstructure:"concurrency"`
	MaxContextTokens int            `mapstructure:"max_context_tokens"`
	Model            string         `mapstructure:"model"`
	DbPath           string         `mapstructure:"db_path"`
	Workspace        string         `mapstructure:"workspace"`
	Tools            ToolsConfig    `mapstructure:"tools"`
	Agent            AgentConfig    `mapstructure:"agent"`
	Langfuse         LangfuseConfig `mapstructure:"langfuse"`

	// Runtime-only metadata (populated by Load, not read from the file).
	LoadedFrom  string   `mapstructure:"-"`
	SearchPaths []string `mapstructure:"-"`
}

type ToolsConfig struct {
	Shell ShellConfig `mapstructure:"shell"`
}

type ShellConfig struct {
	Policy    string   `mapstructure:"policy"`
	Allowlist []string `mapstructure:"allowlist"`
}

type AgentConfig struct {
	MaxIterations int `mapstructure:"max_iterations"`
}

type LangfuseConfig struct {
	Host      string `mapstructure:"host"`
	PublicKey string `mapstructure:"public_key"`
	SecretKey string `mapstructure:"secret_key"`
	SessionID string `mapstructure:"session_id"`
	Release   string `mapstructure:"release"`
	Mask      bool   `mapstructure:"mask"`
}

// SearchPaths returns the directories onclaw searches for .env, in order.
func SearchPaths() []string {
	paths := []string{"."} // current working directory
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".onclaw"))
	}
	paths = append(paths, "/etc/onclaw")
	return paths
}

func mapAndSanitizeEnvKeys(v *viper.Viper, fileLoaded bool) {
	keys := []string{
		"log_level",
		"log_format",
		"concurrency",
		"max_context_tokens",
		"model",
		"db_path",
		"workspace",
		"tools.shell.policy",
		"tools.shell.allowlist",
		"agent.max_iterations",
		"langfuse.host",
		"langfuse.public_key",
		"langfuse.secret_key",
		"langfuse.session_id",
		"langfuse.release",
		"langfuse.mask",
	}

	for _, key := range keys {
		envVar := "ONCLAW_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(key, ".", "_"), "-", "_"))

		// 1. Sanitize OS Environment Variable if present
		if val := os.Getenv(envVar); val != "" {
			switch key {
			case "concurrency", "max_context_tokens", "agent.max_iterations":
				if _, err := strconv.Atoi(val); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: invalid integer value in environment %s: %q. Falling back to default.\n", envVar, val)
					os.Unsetenv(envVar)
					continue
				}
			case "langfuse.mask":
				if _, err := strconv.ParseBool(val); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: invalid boolean value in environment %s: %q. Falling back to default.\n", envVar, val)
					os.Unsetenv(envVar)
					continue
				}
			}
			// If it's valid OS env var, let that take precedence over the file
			continue
		}

		// 2. Map and Sanitize .env File Variable if file was loaded
		if fileLoaded {
			envStyleKey := "onclaw_" + strings.ReplaceAll(strings.ReplaceAll(key, ".", "_"), "-", "_")
			if v.IsSet(envStyleKey) {
				rawVal := v.Get(envStyleKey)
				switch key {
				case "concurrency", "max_context_tokens", "agent.max_iterations":
					if strVal, ok := rawVal.(string); ok {
						if _, err := strconv.Atoi(strVal); err != nil {
							fmt.Fprintf(os.Stderr, "WARNING: invalid integer value for %s: %q. Falling back to default.\n", envVar, strVal)
							continue
						}
					}
				case "langfuse.mask":
					if strVal, ok := rawVal.(string); ok {
						if _, err := strconv.ParseBool(strVal); err != nil {
							fmt.Fprintf(os.Stderr, "WARNING: invalid boolean value for %s: %q. Falling back to default.\n", envVar, strVal)
							continue
						}
					}
				}
				v.Set(key, rawVal)
			}
		}
	}
}

// Load resolves the configuration. If explicitPath is non-empty it is used
// verbatim; otherwise the standard search paths are consulted.
// A missing .env file is not an error — defaults + env apply.
func Load(explicitPath string) (*Config, error) {
	v := viper.New()

	d := defaults()
	v.SetDefault("log_level", d.LogLevel)
	v.SetDefault("log_format", d.LogFormat)
	v.SetDefault("concurrency", d.Concurrency)
	v.SetDefault("max_context_tokens", d.MaxContextTokens)
	v.SetDefault("model", d.Model)
	v.SetDefault("db_path", d.DbPath)
	v.SetDefault("workspace", d.Workspace)
	v.SetDefault("tools.shell.policy", d.Tools.Shell.Policy)
	v.SetDefault("tools.shell.allowlist", d.Tools.Shell.Allowlist)
	v.SetDefault("agent.max_iterations", d.Agent.MaxIterations)
	v.SetDefault("langfuse.host", d.Langfuse.Host)
	v.SetDefault("langfuse.public_key", d.Langfuse.PublicKey)
	v.SetDefault("langfuse.secret_key", d.Langfuse.SecretKey)
	v.SetDefault("langfuse.session_id", d.Langfuse.SessionID)
	v.SetDefault("langfuse.release", d.Langfuse.Release)
	v.SetDefault("langfuse.mask", d.Langfuse.Mask)

	v.SetEnvPrefix("ONCLAW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	cfg := &Config{SearchPaths: SearchPaths()}

	// Check for deprecated legacy config.yaml and warn the user
	for _, p := range cfg.SearchPaths {
		legacyPath := filepath.Join(p, "config.yaml")
		if _, err := os.Stat(legacyPath); err == nil {
			fmt.Fprintln(os.Stderr, "WARNING: config.yaml configuration is deprecated and no longer supported. Please migrate your settings to a .env file.")
			break
		}
	}

	switch {
	case explicitPath != "":
		v.SetConfigFile(explicitPath)
	default:
		v.SetConfigName(".env")
		v.SetConfigType("env")
		for _, p := range cfg.SearchPaths {
			v.AddConfigPath(p)
		}
	}

	fileLoaded := false
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			cfg.LoadedFrom = "" // no file; defaults + env only
		} else {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else {
		cfg.LoadedFrom = v.ConfigFileUsed()
		fileLoaded = true
	}

	// Map env keys and sanitize type conversions unconditionally
	mapAndSanitizeEnvKeys(v, fileLoaded)

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
