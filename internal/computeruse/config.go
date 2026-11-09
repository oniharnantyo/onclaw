package computeruse

import (
	"encoding/json"
	"fmt"
)

// The stored Computer Use configuration (add-computer-use design D1, tasks
// 2.1): the tool_settings "computer" document, shared verbatim by the
// settings service (internal/services/settings_computer.go, the pane's
// read/save) and the composition root's config provider, which consults it
// at every spawn boundary. This file is the single decode/encode site for
// that document.

// ComputerSettingsKey is the tool_settings key the Computer Use
// configuration persists under (the settings service re-exports it for the
// pane).
const ComputerSettingsKey = "computer"

// Config is the resolved Computer Use configuration. The zero value is the
// implicit default (disabled) — an absent stored document means the
// capability has never been enabled, and Computer Use is opt-in end to end
// (design D2: adding the system server must not widen any agent).
type Config struct {
	// Enabled gates the whole lane: when false the daemon and adapter do
	// not run and no agent is offered computer-use tools (spec
	// computer-use "Configuration and spawn boundaries").
	Enabled bool
}

// DefaultConfig returns the implicit configuration: opt-in, so disabled.
func DefaultConfig() Config {
	return Config{Enabled: false}
}

// storedConfigJSON is the tool_settings document's shape. Pointer fields
// separate a key that is missing (keep the default) from one stored
// explicitly (even at its zero value) — the browseruse storedConfigJSON
// pattern, so a future knob can join the document without a migration.
type storedConfigJSON struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// DecodeStoredConfig parses the tool_settings "computer" document. An empty
// string decodes to the default (disabled), exactly like a missing row.
func DecodeStoredConfig(raw string) (Config, error) {
	cfg := DefaultConfig()
	if raw == "" {
		return cfg, nil
	}
	var stored storedConfigJSON
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return Config{}, fmt.Errorf("decode computer config: %w", err)
	}
	if stored.Enabled != nil {
		cfg.Enabled = *stored.Enabled
	}
	return cfg, nil
}

// EncodeStoredConfig renders the configuration as the tool_settings
// document. Explicit is the rule: Enabled is always written so the stored
// document states the choice rather than inheriting it.
func EncodeStoredConfig(cfg Config) (string, error) {
	enabled := cfg.Enabled
	raw, err := json.Marshal(storedConfigJSON{Enabled: &enabled})
	if err != nil {
		return "", fmt.Errorf("encode computer config: %w", err)
	}
	return string(raw), nil
}
