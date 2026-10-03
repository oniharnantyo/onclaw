package skillcuration

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// Spec defaults (add-skill-curation-from-traces 2.2): unset config yields
// them, hand-edited garbage degrades to them.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MinToolCalls != 8 {
		t.Errorf("MinToolCalls = %d, want 8", cfg.MinToolCalls)
	}
	if cfg.MinDistinctTools != 2 {
		t.Errorf("MinDistinctTools = %d, want 2", cfg.MinDistinctTools)
	}
	if cfg.ClusterMinimum != 2 {
		t.Errorf("ClusterMinimum = %d, want 2", cfg.ClusterMinimum)
	}
	if cfg.ProbationWindowDays != 14 {
		t.Errorf("ProbationWindowDays = %d, want 14", cfg.ProbationWindowDays)
	}
	if cfg.MinProbationSample != 6 {
		t.Errorf("MinProbationSample = %d, want 6", cfg.MinProbationSample)
	}
	if cfg.HarmfulRatioThreshold != 0.5 {
		t.Errorf("HarmfulRatioThreshold = %v, want 0.5", cfg.HarmfulRatioThreshold)
	}
	if cfg.CatalogBudgetPerAgent != 20 {
		t.Errorf("CatalogBudgetPerAgent = %d, want 20", cfg.CatalogBudgetPerAgent)
	}
	if cfg.NightlyBudgetK != 10 {
		t.Errorf("NightlyBudgetK = %d, want 10", cfg.NightlyBudgetK)
	}
	if cfg.CycleInterval != 24*time.Hour {
		t.Errorf("CycleInterval = %v, want 24h", cfg.CycleInterval)
	}
	if cfg.QualificationRateAlarm != 0.15 {
		t.Errorf("QualificationRateAlarm = %v, want 0.15", cfg.QualificationRateAlarm)
	}
	if cfg.ApprovalRateAlarm != 0.20 {
		t.Errorf("ApprovalRateAlarm = %v, want 0.20", cfg.ApprovalRateAlarm)
	}
	if cfg.SidecallProviderID != "" || cfg.SidecallModel != "" {
		t.Errorf("sidecall pair should default to unset, got (%q, %q)", cfg.SidecallProviderID, cfg.SidecallModel)
	}
}

func TestConfigFromMap(t *testing.T) {
	t.Run("nil and empty maps yield defaults", func(t *testing.T) {
		if got := ConfigFromMap(nil); got != DefaultConfig() {
			t.Errorf("nil map: %+v, want %+v", got, DefaultConfig())
		}
		if got := ConfigFromMap(map[string]any{}); got != DefaultConfig() {
			t.Errorf("empty map: %+v, want %+v", got, DefaultConfig())
		}
	})

	t.Run("every field parses", func(t *testing.T) {
		cfg := ConfigFromMap(map[string]any{
			"min_tool_calls":           float64(20), // JSON decode shape
			"min_distinct_tools":       3,           // in-process shape
			"cluster_minimum":          int64(4),
			"probation_window_days":    float64(30),
			"min_probation_sample":     float64(10),
			"harmful_ratio_threshold":  0.6,
			"catalog_budget_per_agent": float64(5),
			"nightly_budget_k":         float64(3),
			"cycle_interval_hours":     float64(12),
			"qualification_rate_alarm": 0.25,
			"approval_rate_alarm":      0.3,
			"sidecall_provider_id":     "prov-c",
			"sidecall_model":           "m-c",
		})
		if cfg.MinToolCalls != 20 || cfg.MinDistinctTools != 3 || cfg.ClusterMinimum != 4 {
			t.Errorf("qualification fields: %+v", cfg)
		}
		if cfg.ProbationWindowDays != 30 || cfg.MinProbationSample != 10 || cfg.HarmfulRatioThreshold != 0.6 {
			t.Errorf("probation fields: %+v", cfg)
		}
		if cfg.CatalogBudgetPerAgent != 5 || cfg.NightlyBudgetK != 3 {
			t.Errorf("budget fields: %+v", cfg)
		}
		if cfg.CycleInterval != 12*time.Hour {
			t.Errorf("CycleInterval = %v, want 12h", cfg.CycleInterval)
		}
		if cfg.QualificationRateAlarm != 0.25 || cfg.ApprovalRateAlarm != 0.3 {
			t.Errorf("alarm fields: %+v", cfg)
		}
		if cfg.SidecallProviderID != "prov-c" || cfg.SidecallModel != "m-c" {
			t.Errorf("sidecall pair: %+v", cfg)
		}
	})

	t.Run("out-of-contract values degrade to defaults", func(t *testing.T) {
		cfg := ConfigFromMap(map[string]any{
			"min_tool_calls":          float64(0),    // below bound
			"min_distinct_tools":      float64(9999), // above bound
			"cluster_minimum":         "eight",       // wrong type
			"probation_window_days":   14.5,          // non-integral
			"harmful_ratio_threshold": float64(0),    // ratios are (0, 1]
			"cycle_interval_hours":    float64(1000), // above bound
		})
		want := DefaultConfig()
		if cfg.MinToolCalls != want.MinToolCalls {
			t.Errorf("MinToolCalls = %d, want default %d", cfg.MinToolCalls, want.MinToolCalls)
		}
		if cfg.MinDistinctTools != want.MinDistinctTools {
			t.Errorf("MinDistinctTools = %d, want default %d", cfg.MinDistinctTools, want.MinDistinctTools)
		}
		if cfg.ClusterMinimum != want.ClusterMinimum {
			t.Errorf("ClusterMinimum = %d, want default %d", cfg.ClusterMinimum, want.ClusterMinimum)
		}
		if cfg.ProbationWindowDays != want.ProbationWindowDays {
			t.Errorf("ProbationWindowDays = %d, want default %d", cfg.ProbationWindowDays, want.ProbationWindowDays)
		}
		if cfg.HarmfulRatioThreshold != want.HarmfulRatioThreshold {
			t.Errorf("HarmfulRatioThreshold = %v, want default %v", cfg.HarmfulRatioThreshold, want.HarmfulRatioThreshold)
		}
		if cfg.CycleInterval != want.CycleInterval {
			t.Errorf("CycleInterval = %v, want default %v", cfg.CycleInterval, want.CycleInterval)
		}
	})

	t.Run("half-set sidecall pair degrades to unset", func(t *testing.T) {
		cfg := ConfigFromMap(map[string]any{
			"sidecall_provider_id": "prov-c",
			"sidecall_model":       "",
		})
		if cfg.SidecallProviderID != "" || cfg.SidecallModel != "" {
			t.Errorf("expected the pair to degrade to unset, got (%q, %q)", cfg.SidecallProviderID, cfg.SidecallModel)
		}
	})
}

func TestConfigForWorkspace(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	t.Run("unset row yields spec defaults", func(t *testing.T) {
		st := storefake.New()
		ws := &domain.Workspace{ID: "ws-curation-cfg", Slug: "curation-cfg", Name: "Curation Config"}
		if err := st.Workspaces().Create(ctx, ws); err != nil {
			t.Fatalf("create ws: %v", err)
		}
		cfg := ConfigForWorkspace(ctx, st.ToolSettings(), ws.ID, log)
		if cfg != DefaultConfig() {
			t.Errorf("unconfigured workspace: %+v, want %+v", cfg, DefaultConfig())
		}
	})

	t.Run("configured row parses through", func(t *testing.T) {
		st := storefake.New()
		ws := &domain.Workspace{ID: "ws-curation-cfg2", Slug: "curation-cfg-2", Name: "Curation Config 2"}
		if err := st.Workspaces().Create(ctx, ws); err != nil {
			t.Fatalf("create ws: %v", err)
		}
		row := &domain.WorkspaceToolSetting{
			WorkspaceID: ws.ID,
			ToolKey:     ToolKey,
			Enabled:     true,
			Config:      map[string]any{"min_tool_calls": float64(12), "cluster_minimum": float64(3)},
		}
		if err := st.ToolSettings().Upsert(ctx, row); err != nil {
			t.Fatalf("upsert settings row: %v", err)
		}
		cfg := ConfigForWorkspace(ctx, st.ToolSettings(), ws.ID, log)
		if cfg.MinToolCalls != 12 || cfg.ClusterMinimum != 3 {
			t.Errorf("configured values lost: %+v", cfg)
		}
		if cfg.ProbationWindowDays != 14 {
			t.Errorf("unset field must keep its default, got %d", cfg.ProbationWindowDays)
		}
	})
}
