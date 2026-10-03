package domain

import "testing"

// Save-time contract for the curation tool key (add-skill-curation-from-traces
// 2.2): known keys only, right kind, inside bounds, and the sidecall pair
// all-or-nothing. The skillcuration package parses against the same table, so
// anything this accepts degrades to a parsed value and anything it rejects
// degrades to defaults.
func TestValidateSkillCurationConfig(t *testing.T) {
	t.Run("empty config is valid", func(t *testing.T) {
		for _, config := range []map[string]any{nil, {}} {
			if err := ValidateSkillCurationConfig(config); err != nil {
				t.Errorf("ValidateSkillCurationConfig(%v) = %v, want nil", config, err)
			}
		}
	})

	t.Run("in-contract values are valid", func(t *testing.T) {
		config := map[string]any{
			"min_tool_calls":           float64(20),
			"min_distinct_tools":       3,
			"cluster_minimum":          int64(4),
			"probation_window_days":    float64(30),
			"min_probation_sample":     float64(6),
			"harmful_ratio_threshold":  0.5,
			"catalog_budget_per_agent": float64(20),
			"nightly_budget_k":         float64(10),
			"cycle_interval_hours":     float64(24),
			"qualification_rate_alarm": 0.15,
			"approval_rate_alarm":      0.2,
			"sidecall_provider_id":     "prov-c",
			"sidecall_model":           "m-c",
		}
		if err := ValidateSkillCurationConfig(config); err != nil {
			t.Errorf("in-contract config rejected: %v", err)
		}
	})

	t.Run("unknown keys are rejected", func(t *testing.T) {
		if err := ValidateSkillCurationConfig(map[string]any{"min_tool_callz": float64(8)}); err == nil {
			t.Error("expected an unknown key to be rejected")
		}
	})

	t.Run("wrong kinds and out-of-bounds values are rejected", func(t *testing.T) {
		cases := []struct {
			name  string
			key   string
			value any
		}{
			{"int below bound", "min_tool_calls", float64(0)},
			{"int above bound", "min_tool_calls", float64(1001)},
			{"non-integral int", "min_tool_calls", 8.5},
			{"wrong type", "min_tool_calls", "eight"},
			{"ratio zero", "harmful_ratio_threshold", float64(0)},
			{"ratio above one", "harmful_ratio_threshold", float64(1.5)},
			{"alarm zero", "qualification_rate_alarm", float64(0)},
			{"alarm above one", "approval_rate_alarm", float64(2)},
			{"string field wrong type", "sidecall_provider_id", 3},
		}
		for _, tc := range cases {
			if err := ValidateSkillCurationConfig(map[string]any{tc.key: tc.value}); err == nil {
				t.Errorf("%s: expected %q = %v to be rejected", tc.name, tc.key, tc.value)
			}
		}
	})

	t.Run("sidecall pair is all-or-nothing", func(t *testing.T) {
		if err := ValidateSkillCurationConfig(map[string]any{"sidecall_provider_id": "prov-c"}); err == nil {
			t.Error("expected a provider without a model to be rejected")
		}
		if err := ValidateSkillCurationConfig(map[string]any{"sidecall_model": "m-c"}); err == nil {
			t.Error("expected a model without a provider to be rejected")
		}
	})

	t.Run("setting validation dispatches the curation contract", func(t *testing.T) {
		s := &WorkspaceToolSetting{WorkspaceID: "ws", ToolKey: SkillCurationToolKey, Config: map[string]any{"nope": float64(1)}}
		if err := s.Validate(); err == nil {
			t.Error("expected the curation key's Validate to reject an unknown config key")
		}
		other := &WorkspaceToolSetting{WorkspaceID: "ws", ToolKey: "memory", Config: map[string]any{"nope": float64(1)}}
		if err := other.Validate(); err != nil {
			t.Errorf("a non-curation key must not hit the curation contract, got %v", err)
		}
	})
}

func TestSkillCurationConfigFields(t *testing.T) {
	fields := SkillCurationConfigFields()
	if len(fields) == 0 {
		t.Fatal("expected the curation config contract to be non-empty")
	}
	// The map is a copy: mutating it must not corrupt the contract.
	fields["min_tool_calls"] = SkillCurationConfigField{Key: "min_tool_calls", Kind: CurationConfigKindInt, Min: 0, Max: 0}
	again := SkillCurationConfigFields()
	if again["min_tool_calls"].Min == 0 && again["min_tool_calls"].Max == 0 {
		t.Error("SkillCurationConfigFields must return a defensive copy")
	}
}
