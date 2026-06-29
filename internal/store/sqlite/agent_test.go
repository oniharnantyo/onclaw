package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestAgentStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	as := NewAgentStore(db)

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
	}

	if err := as.AddAgent(ctx, a); err != nil {
		t.Fatalf("failed to AddAgent: %v", err)
	}

	// Test getting agent
	gotA, err := as.GetAgent(ctx, a.Name)
	if err != nil {
		t.Fatalf("failed to GetAgent: %v", err)
	}

	// Verify agent fields match
	if gotA.Name != a.Name ||
		gotA.Provider != a.Provider ||
		gotA.Model != a.Model ||
		gotA.ReasoningEffort != a.ReasoningEffort ||
		gotA.SystemPrompt != a.SystemPrompt ||
		gotA.Workspace != a.Workspace ||
		gotA.Tools != a.Tools ||
		gotA.MaxIterations != a.MaxIterations {
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

	// Test updating agent (including workspace and reasoning budget)
	gotA.Workspace = "/home/new-workspace"
	gotA.ReasoningEffort = "high"
	gotA.ReasoningBudgetTokens = 4096
	gotA.MaxIterations = 20
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
