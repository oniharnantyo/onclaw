// Package skillcuration hosts the skill-curation pipeline: the workspace
// curation config (this file), the qualifier and clustering, the wiki
// maintainer, and the proposer (later tasks). It consumes the shared
// turn-ingest stream as an independently registered consumer
// (add-skill-curation-from-traces D1).
package skillcuration

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ToolKey is the ToolSettingsStore row the curation config is read from.
const ToolKey = domain.SkillCurationToolKey

// Config carries the workspace's curation thresholds, budgets, and cadence.
// Every numeric field has a conservative spec default; values absent from the
// workspace's settings row — or outside the contract domain.ValidateSkillCurationConfig
// enforces at save time — resolve to that default, so hand-edited garbage
// degrades gracefully instead of mis-shaping the loop.
type Config struct {
	// Qualification gates (spec: "Qualification gates"). A run triggers
	// proposal work only when it completed, carries at least MinToolCalls
	// tool calls over at least MinDistinctTools distinct tools, contains a
	// recovery event, and ended with a final assistant message.
	MinToolCalls     int
	MinDistinctTools int

	// ClusterMinimum is how many qualifying runs a similarity cluster needs
	// before one proposal may be drafted (spec: "Cluster gate before
	// drafting").
	ClusterMinimum int

	// Probation (spec: "Curated skill probation and catalog hygiene"): an
	// approved skill is provisional for ProbationWindowDays; after at least
	// MinProbationSample classified outcomes, a harmful ratio above
	// HarmfulRatioThreshold auto-disables the skill.
	ProbationWindowDays   int
	MinProbationSample    int
	HarmfulRatioThreshold float64

	// CatalogBudgetPerAgent caps the agent's curated-skill catalog; approval
	// at capacity is refused with named remedies.
	CatalogBudgetPerAgent int

	// NightlyBudgetK caps the side-calls one cycle may spend.
	NightlyBudgetK int

	// CycleInterval is how often the curation cycle runs (default nightly —
	// 24h; spec: "Cycle cadence and manual trigger").
	CycleInterval time.Duration

	// SidecallProviderID/SidecallModel optionally pin the workspace's
	// curation side-call model. The pair is all-or-nothing: a half-set pair
	// in the settings row degrades to unset, mirroring how a half-set memory
	// side_call_model degrades to the next resolution tier.
	SidecallProviderID string
	SidecallModel      string

	// Health alarms (design D6): qualification rate above
	// QualificationRateAlarm flags thresholds too loose; approval rate below
	// ApprovalRateAlarm flags review fatigue. Both are ratios in (0, 1].
	QualificationRateAlarm float64
	ApprovalRateAlarm      float64
}

// DefaultConfig returns the conservative spec defaults: 8 tool calls across
// 2 distinct tools, a 2-run cluster gate, a 14-day probation with a 6-sample
// minimum and a 0.5 harmful-ratio breach, a 20-skill catalog budget, a
// nightly budget of 10 side-calls, and a 24h (nightly) cycle.
func DefaultConfig() Config {
	return Config{
		MinToolCalls:           8,
		MinDistinctTools:       2,
		ClusterMinimum:         2,
		ProbationWindowDays:    14,
		MinProbationSample:     6,
		HarmfulRatioThreshold:  0.5,
		CatalogBudgetPerAgent:  20,
		NightlyBudgetK:         10,
		CycleInterval:          24 * time.Hour,
		QualificationRateAlarm: 0.15,
		ApprovalRateAlarm:      0.20,
	}
}

// ConfigFromMap parses the curation config off a settings row's structured
// config map. Absent keys (and keys outside the domain contract — wrong
// type, non-integral, out of bounds) keep their defaults; a half-set
// sidecall pair degrades to unset. Never fails: garbage in, defaults for
// that field out.
func ConfigFromMap(config map[string]any) Config {
	cfg := DefaultConfig()
	if config == nil {
		return cfg
	}

	fields := domain.SkillCurationConfigFields()
	for key, target := range map[string]*int{
		"min_tool_calls":           &cfg.MinToolCalls,
		"min_distinct_tools":       &cfg.MinDistinctTools,
		"cluster_minimum":          &cfg.ClusterMinimum,
		"probation_window_days":    &cfg.ProbationWindowDays,
		"min_probation_sample":     &cfg.MinProbationSample,
		"catalog_budget_per_agent": &cfg.CatalogBudgetPerAgent,
		"nightly_budget_k":         &cfg.NightlyBudgetK,
		"cycle_interval_hours":     nil, // handled below: hours -> Duration
	} {
		if target == nil {
			continue
		}
		if v, ok := curationIntFromMap(config, fields, key); ok {
			*target = v
		}
	}
	if v, ok := curationIntFromMap(config, fields, "cycle_interval_hours"); ok {
		cfg.CycleInterval = time.Duration(v) * time.Hour
	}
	for key, target := range map[string]*float64{
		"harmful_ratio_threshold":  &cfg.HarmfulRatioThreshold,
		"qualification_rate_alarm": &cfg.QualificationRateAlarm,
		"approval_rate_alarm":      &cfg.ApprovalRateAlarm,
	} {
		if v, ok := curationFloatFromMap(config, fields, key); ok {
			*target = v
		}
	}
	cfg.SidecallProviderID, _ = config["sidecall_provider_id"].(string)
	cfg.SidecallModel, _ = config["sidecall_model"].(string)
	// Half-set pair degrades to unset (the next resolution tier).
	if (cfg.SidecallProviderID == "") != (cfg.SidecallModel == "") {
		cfg.SidecallProviderID = ""
		cfg.SidecallModel = ""
	}
	return cfg
}

// curationIntFromMap reads one integer config field through its domain
// contract: present, numeric, integral, in bounds. ok=false otherwise.
func curationIntFromMap(config map[string]any, fields map[string]domain.SkillCurationConfigField, key string) (int, bool) {
	field, known := fields[key]
	if !known || field.Kind != domain.CurationConfigKindInt {
		return 0, false
	}
	number, ok := curationNumberFromMap(config, field)
	if !ok {
		return 0, false
	}
	return int(number), true
}

// curationFloatFromMap reads one ratio config field through its domain
// contract: present, numeric, in bounds. ok=false otherwise.
func curationFloatFromMap(config map[string]any, fields map[string]domain.SkillCurationConfigField, key string) (float64, bool) {
	field, known := fields[key]
	if !known || field.Kind != domain.CurationConfigKindRatio {
		return 0, false
	}
	return curationNumberFromMap(config, field)
}

// curationNumberFromMap coerces the JSON-decode shapes a config map may
// carry and applies the field's bounds (the exact predicate
// domain.ValidateSkillCurationConfig rejects at save time).
func curationNumberFromMap(config map[string]any, field domain.SkillCurationConfigField) (float64, bool) {
	var value float64
	switch n := config[field.Key].(type) {
	case float64:
		value = n
	case float32:
		value = float64(n)
	case int:
		value = float64(n)
	case int32:
		value = float64(n)
	case int64:
		value = float64(n)
	default:
		return 0, false
	}
	if field.Kind == domain.CurationConfigKindInt && value != float64(int64(value)) {
		return 0, false
	}
	if field.Kind == domain.CurationConfigKindRatio {
		if value <= field.Min || value > field.Max {
			return 0, false
		}
	} else if value < field.Min || value > field.Max {
		return 0, false
	}
	return value, true
}

// ConfigForWorkspace resolves the workspace's curation config from its
// settings row (ToolKey "curation"). Absence of the row is the normal
// unconfigured state and yields the spec defaults; a read failure degrades
// to the defaults too (the loop is fail-soft end to end), with the read
// error logged. log must be non-nil.
func ConfigForWorkspace(ctx context.Context, settings store.ToolSettingsStore, workspaceID string, log *slog.Logger) Config {
	row, err := settings.Get(ctx, workspaceID, ToolKey)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		log.DebugContext(ctx, "skillcuration: config read failed; using defaults",
			"workspace_id", workspaceID, "error", err)
		return DefaultConfig()
	}
	if row == nil || row.Config == nil {
		return DefaultConfig()
	}
	return ConfigFromMap(row.Config)
}
