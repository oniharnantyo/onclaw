package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/openresponses"
)

// stubToolPolicy answers EnabledTools from a fixed key set (true = enabled).
type stubToolPolicy struct {
	enabled map[string]bool
	err     error
}

func (s stubToolPolicy) EnabledTools(context.Context, string) (map[string]bool, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.enabled, nil
}

func (stubToolPolicy) ToolConfigs(context.Context, string) (map[string]map[string]any, error) {
	return nil, nil
}

// allToolsOn answers the default gate: every catalog key enabled.
func allToolsOn() map[string]bool {
	enabled := make(map[string]bool)
	for _, entry := range agents.ToolCatalog() {
		enabled[entry.Key] = true
	}
	return enabled
}

// TestEffectiveToolSetDenylistResolution pins the v1 narrowing's resolution
// shape: the catalog minus the agent denylist, workspace-gated.
func TestEffectiveToolSetDenylistResolution(t *testing.T) {
	policy := stubToolPolicy{enabled: allToolsOn()}

	t.Run("empty denylist exposes every catalog tool", func(t *testing.T) {
		effective, err := effectiveToolSet(context.Background(), policy, "ws", &domain.Agent{})
		if err != nil {
			t.Fatalf("effectiveToolSet: %v", err)
		}
		for _, entry := range agents.ToolCatalog() {
			if !effective[entry.Key] {
				t.Errorf("catalog tool %q missing from the effective set", entry.Key)
			}
		}
	})

	t.Run("denied tool is absent, the rest stay exposed", func(t *testing.T) {
		agent := &domain.Agent{DisabledTools: []string{"web.search"}}
		effective, err := effectiveToolSet(context.Background(), policy, "ws", agent)
		if err != nil {
			t.Fatalf("effectiveToolSet: %v", err)
		}
		if effective["web.search"] {
			t.Error("denied web.search must be absent from the effective set")
		}
		if !effective["execute"] || !effective["read_file"] {
			t.Error("non-denied catalog tools must stay exposed")
		}
	})

	t.Run("unknown denylist name is inert", func(t *testing.T) {
		agent := &domain.Agent{DisabledTools: []string{"ghost.tool"}}
		effective, err := effectiveToolSet(context.Background(), policy, "ws", agent)
		if err != nil {
			t.Fatalf("effectiveToolSet: %v", err)
		}
		if len(effective) != len(agents.ToolCatalog()) {
			t.Errorf("unknown denial must not change the effective set size: %d", len(effective))
		}
	})

	t.Run("workspace gate wins over the denylist", func(t *testing.T) {
		gated := allToolsOn()
		gated["web.search"] = false
		gatePolicy := stubToolPolicy{enabled: gated}
		effective, err := effectiveToolSet(context.Background(), gatePolicy, "ws", &domain.Agent{})
		if err != nil {
			t.Fatalf("effectiveToolSet: %v", err)
		}
		if effective["web.search"] {
			t.Error("gate-disabled web.search must be absent even though no agent denies it")
		}
	})

	t.Run("gate failure propagates", func(t *testing.T) {
		failing := stubToolPolicy{err: errors.New("settings store down")}
		if _, err := effectiveToolSet(context.Background(), failing, "ws", &domain.Agent{}); err == nil {
			t.Fatal("expected the gate failure to propagate")
		}
	})
}

// TestNarrowRequestToolsCannotExtend pins the request narrowing: tool_choice
// "none" strips everything, a request's names may only narrow the effective
// set, and a request without tools leaves selection to the agent config.
func TestNarrowRequestToolsCannotExtend(t *testing.T) {
	policy := stubToolPolicy{enabled: allToolsOn()}
	agent := &domain.Agent{}

	t.Run("tool_choice none strips all tools", func(t *testing.T) {
		req := &openresponses.ResponseRequest{
			ToolChoice: json.RawMessage(`"none"`),
			Tools:      []openresponses.RequestTool{{Type: "function", Name: "web.search"}},
		}
		allowed, err := narrowRequestTools(context.Background(), policy, "ws", agent, req)
		if err != nil {
			t.Fatalf("narrowRequestTools: %v", err)
		}
		if allowed == nil || len(allowed) != 0 {
			t.Errorf("tool_choice none must strip every tool, got %v", allowed)
		}
	})

	t.Run("request names outside the effective set are dropped", func(t *testing.T) {
		req := &openresponses.ResponseRequest{
			Tools: []openresponses.RequestTool{
				{Type: "function", Name: "web.search"},
				{Type: "function", Name: "ghost.tool"},
			},
		}
		allowed, err := narrowRequestTools(context.Background(), policy, "ws", agent, req)
		if err != nil {
			t.Fatalf("narrowRequestTools: %v", err)
		}
		if !slices.Equal(allowed, []string{"web.search"}) {
			t.Errorf("allowed = %v, want [web.search] (a request may narrow, never extend)", allowed)
		}
	})

	t.Run("denied tool cannot ride back in through the request", func(t *testing.T) {
		denying := &domain.Agent{DisabledTools: []string{"web.search"}}
		req := &openresponses.ResponseRequest{
			Tools: []openresponses.RequestTool{{Type: "function", Name: "web.search"}},
		}
		allowed, err := narrowRequestTools(context.Background(), policy, "ws", denying, req)
		if err != nil {
			t.Fatalf("narrowRequestTools: %v", err)
		}
		if len(allowed) != 0 {
			t.Errorf("allowed = %v, want empty (the denylist wins over the request)", allowed)
		}
	})

	t.Run("gate-disabled tool cannot ride back in through the request", func(t *testing.T) {
		gated := allToolsOn()
		gated["web.search"] = false
		gatePolicy := stubToolPolicy{enabled: gated}
		req := &openresponses.ResponseRequest{
			Tools: []openresponses.RequestTool{{Type: "function", Name: "web.search"}},
		}
		allowed, err := narrowRequestTools(context.Background(), gatePolicy, "ws", agent, req)
		if err != nil {
			t.Fatalf("narrowRequestTools: %v", err)
		}
		if len(allowed) != 0 {
			t.Errorf("allowed = %v, want empty (the gate wins over the request)", allowed)
		}
	})

	t.Run("request without tools leaves selection to the agent config", func(t *testing.T) {
		req := &openresponses.ResponseRequest{}
		allowed, err := narrowRequestTools(context.Background(), policy, "ws", agent, req)
		if err != nil {
			t.Fatalf("narrowRequestTools: %v", err)
		}
		if allowed != nil {
			t.Errorf("allowed = %v, want nil (no per-turn override)", allowed)
		}
	})

	t.Run("gate failure propagates", func(t *testing.T) {
		failing := stubToolPolicy{err: errors.New("settings store down")}
		req := &openresponses.ResponseRequest{
			Tools: []openresponses.RequestTool{{Type: "function", Name: "web.search"}},
		}
		if _, err := narrowRequestTools(context.Background(), failing, "ws", agent, req); err == nil {
			t.Fatal("expected the gate failure to propagate")
		}
	})
}
