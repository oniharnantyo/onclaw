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
	Web              WebConfig      `mapstructure:"web"`
	Memory           MemoryConfig   `mapstructure:"memory"`

	// Runtime-only metadata (populated by Load, not read from the file).
	LoadedFrom  string   `mapstructure:"-"`
	SearchPaths []string `mapstructure:"-"`
}

type WebConfig struct {
	Bind string `mapstructure:"bind"`
	Port int    `mapstructure:"port"`
}

type ToolsConfig struct {
	Shell ShellConfig `mapstructure:"shell"`
}

type ShellConfig struct {
	Policy    string   `mapstructure:"policy"`
	Allowlist []string `mapstructure:"allowlist"`
	Denylist  []string `mapstructure:"denylist"`
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

type MemoryConfig struct {
	Enabled           bool    `mapstructure:"enabled"`
	EmbeddingProvider string  `mapstructure:"embedding_provider"`
	EmbeddingModel    string  `mapstructure:"embedding_model"`
	CharLimit         int     `mapstructure:"char_limit"`
	WriteApproval     bool    `mapstructure:"write_approval"`
	FtsWeight         float64 `mapstructure:"fts_weight"`
	VectorWeight      float64 `mapstructure:"vector_weight"`
	ReviewModel       string  `mapstructure:"review_model"`
	DreamThreshold    int     `mapstructure:"dream_threshold"`
	EpisodicTTLDays   int     `mapstructure:"episodic_ttl_days"`
	KGEnabled         bool    `mapstructure:"kg_enabled"`
	KGTraversalDepth  int     `mapstructure:"kg_traversal_depth"`
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
		"tools.shell.denylist",
		"agent.max_iterations",
		"langfuse.host",
		"langfuse.public_key",
		"langfuse.secret_key",
		"langfuse.session_id",
		"langfuse.release",
		"langfuse.mask",
		"web.bind",
		"web.port",
		"memory.enabled",
		"memory.embedding_provider",
		"memory.embedding_model",
		"memory.char_limit",
		"memory.write_approval",
		"memory.fts_weight",
		"memory.vector_weight",
		"memory.review_model",
		"memory.dream_threshold",
		"memory.episodic_ttl_days",
		"memory.kg_enabled",
		"memory.kg_traversal_depth",
	}

	for _, key := range keys {
		envVar := "ONCLAW_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(key, ".", "_"), "-", "_"))

		// 1. Sanitize OS Environment Variable if present
		if val := os.Getenv(envVar); val != "" {
			switch key {
			case "concurrency", "max_context_tokens", "agent.max_iterations", "web.port", "memory.char_limit", "memory.kg_traversal_depth":
				if _, err := strconv.Atoi(val); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: invalid integer value in environment %s: %q. Falling back to default.\n", envVar, val)
					os.Unsetenv(envVar)
					continue
				}
			case "langfuse.mask", "memory.enabled", "memory.kg_enabled":
				if _, err := strconv.ParseBool(val); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: invalid boolean value in environment %s: %q. Falling back to default.\n", envVar, val)
					os.Unsetenv(envVar)
					continue
				}
			case "memory.fts_weight", "memory.vector_weight":
				if _, err := strconv.ParseFloat(val, 64); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: invalid float value in environment %s: %q. Falling back to default.\n", envVar, val)
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
				case "concurrency", "max_context_tokens", "agent.max_iterations", "web.port", "memory.char_limit", "memory.dream_threshold", "memory.episodic_ttl_days", "memory.kg_traversal_depth":
					if strVal, ok := rawVal.(string); ok {
						if _, err := strconv.Atoi(strVal); err != nil {
							fmt.Fprintf(os.Stderr, "WARNING: invalid integer value for %s: %q. Falling back to default.\n", envVar, strVal)
							continue
						}
					}
				case "langfuse.mask", "memory.enabled", "memory.kg_enabled":
					if strVal, ok := rawVal.(string); ok {
						if _, err := strconv.ParseBool(strVal); err != nil {
							fmt.Fprintf(os.Stderr, "WARNING: invalid boolean value for %s: %q. Falling back to default.\n", envVar, strVal)
							continue
						}
					}
				case "memory.fts_weight", "memory.vector_weight":
					if strVal, ok := rawVal.(string); ok {
						if _, err := strconv.ParseFloat(strVal, 64); err != nil {
							fmt.Fprintf(os.Stderr, "WARNING: invalid float value for %s: %q. Falling back to default.\n", envVar, strVal)
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
	v.SetDefault("tools.shell.denylist", d.Tools.Shell.Denylist)
	v.SetDefault("agent.max_iterations", d.Agent.MaxIterations)
	v.SetDefault("langfuse.host", d.Langfuse.Host)
	v.SetDefault("langfuse.public_key", d.Langfuse.PublicKey)
	v.SetDefault("langfuse.secret_key", d.Langfuse.SecretKey)
	v.SetDefault("langfuse.session_id", d.Langfuse.SessionID)
	v.SetDefault("langfuse.release", d.Langfuse.Release)
	v.SetDefault("langfuse.mask", d.Langfuse.Mask)
	v.SetDefault("web.bind", d.Web.Bind)
	v.SetDefault("web.port", d.Web.Port)
	v.SetDefault("memory.enabled", d.Memory.Enabled)
	v.SetDefault("memory.embedding_provider", d.Memory.EmbeddingProvider)
	v.SetDefault("memory.embedding_model", d.Memory.EmbeddingModel)
	v.SetDefault("memory.char_limit", d.Memory.CharLimit)
	v.SetDefault("memory.write_approval", d.Memory.WriteApproval)
	v.SetDefault("memory.fts_weight", d.Memory.FtsWeight)
	v.SetDefault("memory.vector_weight", d.Memory.VectorWeight)
	v.SetDefault("memory.review_model", d.Memory.ReviewModel)
	v.SetDefault("memory.dream_threshold", d.Memory.DreamThreshold)
	v.SetDefault("memory.episodic_ttl_days", d.Memory.EpisodicTTLDays)
	v.SetDefault("memory.kg_enabled", d.Memory.KGEnabled)
	v.SetDefault("memory.kg_traversal_depth", d.Memory.KGTraversalDepth)

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
