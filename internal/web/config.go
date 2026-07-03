package web

import (
	"encoding/json"
)

// Config represents the Web category configuration schema.
type Config struct {
	SearchProvider    string `json:"search_provider"`
	FetchProvider     string `json:"fetch_provider"`
	UserAgent         string `json:"user_agent"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	MaxBytes          int64  `json:"max_bytes"`
	GoogleCX          string `json:"google_cx"`
	LightpandaBinPath string `json:"lightpanda_bin_path"`
}

// ParseConfig parses a JSON configuration string and returns the Config with defaults applied.
func ParseConfig(raw string) (Config, error) {
	var cfg Config
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return Config{}, err
		}
	}

	// Apply defaults
	if cfg.SearchProvider == "" {
		cfg.SearchProvider = "duckduckgo"
	}
	if cfg.FetchProvider == "" {
		cfg.FetchProvider = "http"
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 1024 * 1024 // 1MB default
	}
	if cfg.LightpandaBinPath == "" {
		cfg.LightpandaBinPath = "lightpanda"
	}
	return cfg, nil
}
