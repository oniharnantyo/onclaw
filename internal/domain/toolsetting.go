package domain

import (
	"fmt"
	"math"
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
// structural constraints shared by all tool schemas plus any per-tool
// contract layered on the owning key.
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
	if err := ValidateToolConfigValues(s.Config); err != nil {
		return err
	}
	if s.ToolKey == SkillCurationToolKey {
		return ValidateSkillCurationConfig(s.Config)
	}
	return nil
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

// SkillCurationToolKey is the ToolSettingsStore row the skill-curation
// pipeline reads its workspace thresholds from (add-skill-curation-from-traces
// 2.2). Absence of the row means the spec defaults — the same
// absence-is-defaults convention the memory settings row ("memory") follows.
const SkillCurationToolKey = "curation"

// SkillCurationConfigField is the save-time contract of one skill-curation
// config key: its value kind and numeric bounds. For kind "ratio" the Min
// bound is exclusive and Max inclusive (a harmful ratio of 0 would disable
// the alarm, so 0 is out of contract); for "int" both bounds are inclusive.
// "string" values only check the type — emptiness is allowed and means unset.
type SkillCurationConfigField struct {
	Key      string
	Kind     string // "int", "ratio", or "string"
	Min, Max float64
}

// Skill-curation config value kinds.
const (
	CurationConfigKindInt    = "int"
	CurationConfigKindRatio  = "ratio"
	CurationConfigKindString = "string"
)

// skillCurationConfigFields is the single source of truth for the curation
// config contract: the settings save path validates against it and the
// skillcuration package parses (and degrades out-of-contract values to
// defaults) against the same table.
var skillCurationConfigFields = []SkillCurationConfigField{
	{Key: "min_tool_calls", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "min_distinct_tools", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "cluster_minimum", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "probation_window_days", Kind: CurationConfigKindInt, Min: 1, Max: 365},
	{Key: "min_probation_sample", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "harmful_ratio_threshold", Kind: CurationConfigKindRatio, Min: 0, Max: 1},
	{Key: "catalog_budget_per_agent", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "nightly_budget_k", Kind: CurationConfigKindInt, Min: 1, Max: 1000},
	{Key: "cycle_interval_hours", Kind: CurationConfigKindInt, Min: 1, Max: 720},
	{Key: "qualification_rate_alarm", Kind: CurationConfigKindRatio, Min: 0, Max: 1},
	{Key: "approval_rate_alarm", Kind: CurationConfigKindRatio, Min: 0, Max: 1},
	{Key: "sidecall_provider_id", Kind: CurationConfigKindString},
	{Key: "sidecall_model", Kind: CurationConfigKindString},
}

// SkillCurationConfigFields returns the curation config contract keyed by
// config key. The map is a fresh copy — callers may not mutate the contract.
func SkillCurationConfigFields() map[string]SkillCurationConfigField {
	fields := make(map[string]SkillCurationConfigField, len(skillCurationConfigFields))
	for _, f := range skillCurationConfigFields {
		fields[f.Key] = f
	}
	return fields
}

// ValidateSkillCurationConfig enforces the curation tool key's config
// contract: every key must be a known curation field with a value of the
// right kind inside its bounds, and the side-call override pair must be
// fully set or fully empty — the same pair rule as the agent-level override.
// An empty (or nil) map is valid: absence is defaults.
func ValidateSkillCurationConfig(config map[string]any) error {
	fields := SkillCurationConfigFields()
	values := make(map[string]any, len(config))
	for key, value := range config {
		field, known := fields[key]
		if !known {
			return fmt.Errorf("%w: unknown skill-curation config key %q", ErrInvalid, key)
		}
		if field.Kind == CurationConfigKindString {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%w: config field %q must be a string", ErrInvalid, key)
			}
			values[key] = value
			continue
		}
		number, ok := numericConfigValue(value)
		if !ok {
			return fmt.Errorf("%w: config field %q must be a number", ErrInvalid, key)
		}
		if field.Kind == CurationConfigKindInt && number != math.Trunc(number) {
			return fmt.Errorf("%w: config field %q must be a whole number", ErrInvalid, key)
		}
		if field.Kind == CurationConfigKindRatio {
			if number <= field.Min || number > field.Max {
				return fmt.Errorf("%w: config field %q must be greater than %v and at most %v", ErrInvalid, key, field.Min, field.Max)
			}
		} else if number < field.Min || number > field.Max {
			return fmt.Errorf("%w: config field %q must be between %v and %v", ErrInvalid, key, field.Min, field.Max)
		}
		values[key] = number
	}
	providerID, _ := values["sidecall_provider_id"].(string)
	model, _ := values["sidecall_model"].(string)
	if (providerID == "") != (model == "") {
		return fmt.Errorf("%w: skill-curation sidecall override needs both provider and model, or neither", ErrInvalid)
	}
	return nil
}
