package memory

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The intent gate's classification budget is a workspace memory-setting
// (fix-memory-retrieval-lane D1/D2): the pinned 1.5s default could not
// accommodate a remote side-call model's round-trip, so the budget became
// configurable with a survivable default. Bounds are the save-time contract
// (the settings handler 422s outside them); a hand-edited row outside the
// bounds degrades to the default instead of failing the gate.
const (
	// DefaultGateBudgetMS is the classification budget when the memory
	// settings record does not carry gate_budget_ms (absence-is-defaults).
	DefaultGateBudgetMS = 4000
	// MinGateBudgetMS and MaxGateBudgetMS bound the save-time validation.
	MinGateBudgetMS = 500
	MaxGateBudgetMS = 20000
)

// GateBudgetFromConfig resolves gate_budget_ms off the memory settings
// record's structured config. ok=false when the key is absent or out of
// contract (non-integer, outside the bounds) — callers fall back to
// DefaultGateBudgetMS, mirroring how a half-set side_call_model degrades to
// the next resolution tier.
func GateBudgetFromConfig(config map[string]any) (time.Duration, bool) {
	if config == nil {
		return 0, false
	}
	var ms float64
	switch v := config["gate_budget_ms"].(type) {
	case float64: // JSON decode shape
		ms = v
	case int:
		ms = float64(v)
	case int64:
		ms = float64(v)
	default:
		return 0, false
	}
	if ms != math.Trunc(ms) || ms < MinGateBudgetMS || ms > MaxGateBudgetMS {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// SettingsGateBudget returns the per-turn gate budget resolver the runner
// hands the IntentGate call: one settings read per gate-primed turn, so a
// settings edit applies on the next turn without rebuilding the runner
// (D3). Absence and out-of-contract values resolve to the default.
func SettingsGateBudget(settings store.ToolSettingsStore, log *slog.Logger) func(ctx context.Context, workspaceID string) time.Duration {
	return func(ctx context.Context, workspaceID string) time.Duration {
		row, err := settings.Get(ctx, workspaceID, "memory")
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			log.DebugContext(ctx, "memory: gate budget read failed; using default",
				"workspace_id", workspaceID, "error", err)
			return time.Duration(DefaultGateBudgetMS) * time.Millisecond
		}
		if row == nil || row.Config == nil {
			return time.Duration(DefaultGateBudgetMS) * time.Millisecond
		}
		if budget, ok := GateBudgetFromConfig(row.Config); ok {
			return budget
		}
		return time.Duration(DefaultGateBudgetMS) * time.Millisecond
	}
}
