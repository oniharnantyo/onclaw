package browseruse

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// EnvAllowPrivate opts the server out of the navigate private-network guard
// (loopback, private, link-local, and unique-local targets become navigable).
// It is read from the server process's environment; it exists for local
// development and testing.
const EnvAllowPrivate = "BUDDIES_BROWSER_ALLOW_PRIVATE"

// defaultActionSeconds bounds every action when the configuration omits one.
const defaultActionSeconds = 30

// DefaultReadChars is the read tool's default text bound, and MaxReadChars
// the hard cap a larger maxLength argument is clamped to.
const (
	DefaultReadChars = 20_000
	MaxReadChars     = 100_000
)

// Config is the resolved server configuration snapshot handed to the server
// at spawn (design D8): command-line flags carry it, settings changes apply
// at the next spawn boundary, never mid-action.
type Config struct {
	// BrowserDir is the browser root: profiles/, screenshots/, and the
	// ownership lockfile live under it.
	BrowserDir string
	// DefaultProfile is used when a tool call names no profile; the empty
	// value falls back to DefaultBrowserProfile.
	DefaultProfile string
	// Headless launches Chromium without a window.
	Headless bool
	// IdleTimeoutSeconds is how long a profile browser may sit idle before
	// the janitor closes it (0 => 120).
	IdleTimeoutSeconds int
	// ActionTimeoutSeconds bounds every single tool action (0 => 30).
	ActionTimeoutSeconds int
	// AllowPrivate permits navigating private and local network addresses.
	AllowPrivate bool
}

// DefaultConfig returns the server defaults: profile "default", headless on,
// 120 s idle timeout, 30 s action timeout, private networks blocked, and no
// browser directory (the caller always sets one).
func DefaultConfig() Config {
	return Config{
		DefaultProfile:       DefaultBrowserProfile,
		Headless:             true,
		IdleTimeoutSeconds:   managerIdleSeconds,
		ActionTimeoutSeconds: defaultActionSeconds,
	}
}

// storedConfigJSON is the persisted tool-settings document's shape (the
// browser key of the store's tool_settings table). Pointer fields separate a
// key that is missing (keep the default) from one stored explicitly (even at
// its zero value), so a saved headless:false survives the decode. This is
// the single decode site for that document.
type storedConfigJSON struct {
	DefaultProfile       *string `json:"defaultProfile"`
	Headless             *bool   `json:"headless"`
	IdleTimeoutSeconds   *int    `json:"idleTimeoutSeconds"`
	ActionTimeoutSeconds *int    `json:"actionTimeoutSeconds"`
	AllowPrivate         *bool   `json:"allowPrivate"`
}

// DecodeStoredConfig parses the stored browser-config JSON document into the
// resolved Config. A missing or empty document decodes to the defaults; an
// explicitly stored value always wins, including explicit false/zero —
// pointer fields keep that distinction. Config.BrowserDir is not part of the
// stored document; the caller sets it.
func DecodeStoredConfig(raw string) (Config, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	var stored storedConfigJSON
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return Config{}, fmt.Errorf("browser: decode stored config: %w", err)
	}
	if stored.DefaultProfile != nil {
		cfg.DefaultProfile = *stored.DefaultProfile
	}
	if stored.Headless != nil {
		cfg.Headless = *stored.Headless
	}
	if stored.IdleTimeoutSeconds != nil {
		cfg.IdleTimeoutSeconds = *stored.IdleTimeoutSeconds
	}
	if stored.ActionTimeoutSeconds != nil {
		cfg.ActionTimeoutSeconds = *stored.ActionTimeoutSeconds
	}
	if stored.AllowPrivate != nil {
		cfg.AllowPrivate = *stored.AllowPrivate
	}
	return cfg, nil
}

// idleTTL resolves the idle lifetime from the config; the zero value means
// the default (non-positive values would never reap or reap instantly).
func idleTTL(cfg Config) time.Duration {
	if cfg.IdleTimeoutSeconds <= 0 {
		return managerIdleSeconds * time.Second
	}
	return time.Duration(cfg.IdleTimeoutSeconds) * time.Second
}

// actionTimeout resolves the per-action bound from the config; the zero
// value means the default.
func actionTimeout(cfg Config) time.Duration {
	if cfg.ActionTimeoutSeconds <= 0 {
		return defaultActionSeconds * time.Second
	}
	return time.Duration(cfg.ActionTimeoutSeconds) * time.Second
}

// envAllowPrivate reports whether the server process's environment opts out
// of the private-network guard.
func envAllowPrivate() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAllowPrivate))) {
	case "1", "true":
		return true
	default:
		return false
	}
}
