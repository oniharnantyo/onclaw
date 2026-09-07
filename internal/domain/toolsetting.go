package domain

import (
	"fmt"
	"time"
)

// WorkspaceToolSetting represents a workspace-scoped per-tool setting: the
// global enable toggle plus the tool's structured configuration. Absence of a
// row means the tool is enabled with its default configuration.
//
// Config values are opaque at the domain layer; per-tool schema validation
// (known keys, enums, required fields) lives with the tool catalog and is
// layered on top of the structural checks in ValidateToolConfigValues.
type WorkspaceToolSetting struct {
	WorkspaceID string         `json:"workspace_id"`
	ToolKey     string         `json:"tool_key"`
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// positiveNumberConfigKeys lists config fields that must be positive numbers
// when present, regardless of the owning tool's schema.
var positiveNumberConfigKeys = map[string]struct{}{
	"max_pages":              {},
	"idle_timeout_seconds":   {},
	"action_timeout_seconds": {},
}

// Validate reports whether the setting is structurally valid: the workspace
// scope and tool key must be non-empty, and config values must satisfy the
// structural constraints shared by all tool schemas.
func (s *WorkspaceToolSetting) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if s.ToolKey == "" {
		return fmt.Errorf("%w: tool key cannot be empty", ErrInvalid)
	}
	return ValidateToolConfigValues(s.Config)
}

// ValidateToolConfigValues checks the structural constraints every tool config
// must satisfy: fields in positiveNumberConfigKeys must be positive numbers
// when present. Per-tool schema validation is applied by the tool catalog on
// top of these shared checks.
func ValidateToolConfigValues(config map[string]any) error {
	for key, value := range config {
		if _, shared := positiveNumberConfigKeys[key]; !shared {
			continue
		}
		number, ok := numericConfigValue(value)
		if !ok || number <= 0 {
			return fmt.Errorf("%w: config field %q must be a positive number", ErrInvalid, key)
		}
	}
	return nil
}

// numericConfigValue coerces the numeric kinds a decoded config map may carry
// (float64 from encoding/json, fixed-width ints from in-process callers).
func numericConfigValue(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
