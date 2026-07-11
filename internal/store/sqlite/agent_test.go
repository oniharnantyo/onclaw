package sqlite_test

import (
	"context"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestAgentStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	as := sqlite.NewAgentStore(db)

	// Test adding invalid agent (empty name)
	invalidA := &store.Agent{Name: "", Provider: "openai"}
	if err := as.AddAgent(ctx, invalidA); err == nil {
		t.Error("expected error adding empty agent, got nil")
	}

	// Test adding invalid agent (empty provider)
	invalidA2 := &store.Agent{Name: "agent-1", Provider: ""}
	if err := as.AddAgent(ctx, invalidA2); err == nil {
		t.Error("expected error adding agent with empty provider, got nil")
	}

	// Test adding valid agent
	a := &store.Agent{
		Name:            "test-agent",
		Provider:        "openai-prov",
		Model:           "gpt-4o",
		ReasoningEffort: "medium",
		SystemPrompt:    "System prompt text",
		Workspace:       "/home/workspace",
		Tools:           "read_file,write_file",
		MaxIterations:   10,
		MaxContextTokens: 4000,
	}

	if err := as.AddAgent(ctx, a); err != nil {
		t.Fatalf("failed to AddAgent: %v", err)
	}

	// Test getting agent
	gotA, err := as.GetAgent(ctx, a.Name)
	if err != nil {
		t.Fatalf("failed to GetAgent: %v", err)
	}

	if gotA.MemoryConfig != "{}" && gotA.MemoryConfig != "" {
		t.Errorf("expected empty/default memory config, got %q", gotA.MemoryConfig)
	}

	// Verify agent fields match
	if gotA.Name != a.Name ||
		gotA.Provider != a.Provider ||
		gotA.Model != a.Model ||
		gotA.ReasoningEffort != a.ReasoningEffort ||
		gotA.SystemPrompt != a.SystemPrompt ||
		gotA.Workspace != a.Workspace ||
		gotA.Tools != a.Tools ||
		gotA.MaxIterations != a.MaxIterations ||
		gotA.MaxContextTokens != a.MaxContextTokens {
		t.Errorf("agent fields mismatch. got: %+v, want: %+v", gotA, a)
	}

	// Verify timestamps were set
	if gotA.CreatedAt == "" || gotA.UpdatedAt == "" {
		t.Error("expected CreatedAt and UpdatedAt to be set, got empty strings")
	}

	// Test getting non-existent agent
	_, err = as.GetAgent(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting nonexistent agent, got nil")
	}

	// Test listing agents
	list, err := as.ListAgents(ctx)
	if err != nil {
		t.Fatalf("failed to ListAgents: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected list length 1, got %d", len(list))
	}
	if list[0].Name != a.Name {
		t.Errorf("expected agent name %s, got %s", a.Name, list[0].Name)
	}

	// Test that adding duplicate agent fails
	if err := as.AddAgent(ctx, a); err == nil {
		t.Error("expected error when adding duplicate agent, got nil")
	}

	// Test updating agent (including workspace, reasoning budget, memory config, and max context tokens)
	gotA.Workspace = "/home/new-workspace"
	gotA.ReasoningEffort = "high"
	gotA.ReasoningBudgetTokens = 4096
	gotA.MaxIterations = 20
	gotA.MaxContextTokens = 8000
	gotA.MemoryConfig = `{"curated_enabled":false}`
	if err := as.UpdateAgent(ctx, gotA); err != nil {
		t.Fatalf("failed to UpdateAgent: %v", err)
	}

	updatedA, err := as.GetAgent(ctx, a.Name)
	if err != nil {
		t.Fatalf("failed to GetAgent after update: %v", err)
	}

	if updatedA.Workspace != gotA.Workspace {
		t.Errorf("expected updated workspace %q, got %q", gotA.Workspace, updatedA.Workspace)
	}
	if updatedA.ReasoningEffort != gotA.ReasoningEffort {
		t.Errorf("expected updated reasoning effort %q, got %q", gotA.ReasoningEffort, updatedA.ReasoningEffort)
	}
	if updatedA.ReasoningBudgetTokens != gotA.ReasoningBudgetTokens {
		t.Errorf("expected updated reasoning budget %d, got %d", gotA.ReasoningBudgetTokens, updatedA.ReasoningBudgetTokens)
	}
	if updatedA.MaxIterations != gotA.MaxIterations {
		t.Errorf("expected updated max iterations %d, got %d", gotA.MaxIterations, updatedA.MaxIterations)
	}
	if updatedA.MaxContextTokens != gotA.MaxContextTokens {
		t.Errorf("expected updated max context tokens %d, got %d", gotA.MaxContextTokens, updatedA.MaxContextTokens)
	}
	if updatedA.MemoryConfig != gotA.MemoryConfig {
		t.Errorf("expected updated memory config %q, got %q", gotA.MemoryConfig, updatedA.MemoryConfig)
	}

	// Test updating agent tools
	if err := as.UpdateAgentTools(ctx, a.Name, "new_tool_1,new_tool_2"); err != nil {
		t.Fatalf("failed to UpdateAgentTools: %v", err)
	}
	updatedToolsA, err := as.GetAgent(ctx, a.Name)
	if err != nil {
		t.Fatalf("failed to GetAgent: %v", err)
	}
	if updatedToolsA.Tools != "new_tool_1,new_tool_2" {
		t.Errorf("expected updated tools 'new_tool_1,new_tool_2', got %q", updatedToolsA.Tools)
	}
	// Test updating tools for non-existent agent returns error
	if err := as.UpdateAgentTools(ctx, "nonexistent", "new_tool_1"); err == nil {
		t.Error("expected error when updating tools for nonexistent agent, got nil")
	}

	// Test removing agent
	if err := as.RemoveAgent(ctx, a.Name); err != nil {
		t.Fatalf("failed to RemoveAgent: %v", err)
	}

	// Verify agent was removed
	_, err = as.GetAgent(ctx, a.Name)
	if err == nil {
		t.Error("expected error getting removed agent, got nil")
	}

	// Test removing non-existent agent
	if err := as.RemoveAgent(ctx, "nonexistent"); err == nil {
		t.Error("expected RemoveAgent to return error for nonexistent agent")
	}
}
