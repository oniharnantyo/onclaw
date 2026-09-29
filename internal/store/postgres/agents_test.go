//go:build integration

package postgres_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestIntegration_AgentStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// Seed workspaces
	ws1 := &domain.Workspace{Slug: "ws-agent-1", Name: "Agent Workspace 1"}
	if err := s.Workspaces().Create(ctx, ws1); err != nil {
		t.Fatalf("unexpected create workspace 1 error: %v", err)
	}

	ws2 := &domain.Workspace{Slug: "ws-agent-2", Name: "Agent Workspace 2"}
	if err := s.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("unexpected create workspace 2 error: %v", err)
	}

	// Seed providers
	p1 := &domain.ProviderConfig{
		WorkspaceID: ws1.ID,
		Type:        "openai",
		Name:        "OpenAI Prod",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p1); err != nil {
		t.Fatalf("unexpected create provider 1 error: %v", err)
	}

	p2 := &domain.ProviderConfig{
		WorkspaceID: ws2.ID,
		Type:        "anthropic",
		Name:        "Anthropic WS2",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p2); err != nil {
		t.Fatalf("unexpected create provider 2 error: %v", err)
	}

	// 1. Create agent with full properties
	maxTok := 2048
	effort := "medium"
	a1 := &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "support-agent",
		Name:        "Support Agent",
		Role:        "customer-success",
		Description: "Helps users with questions",
		Brief:       "Friendly support agent persona",
		ProviderID:  p1.ID,
		Model:       "gpt-4o",
		Temperature: 0.8,
		MaxTokens:   &maxTok,
		Effort:      &effort,
		Autonomy:    domain.AutonomyApproval,
		DisabledTools: []string{"search_kb", "calc"},
		EnabledMCPS: []string{"github"},
		Avatar:      json.RawMessage(`{"shape":"circle","color":"#336699"}`),
	}
	if err := s.Agents().Create(ctx, a1); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	if a1.ID == "" {
		t.Fatal("expected agent ID to be assigned")
	}
	if a1.PromptsStatus != domain.PromptsStatusGenerating {
		t.Fatalf("expected default prompts_status generating, got %q", a1.PromptsStatus)
	}
	if a1.CreatedAt.IsZero() || a1.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Validation failures on Create
	if err := s.Agents().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil agent, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: "", Name: "A", Slug: "a", ProviderID: p1.ID, Model: "m"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing workspace_id, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "BAD_SLUG!", ProviderID: p1.ID, Model: "m"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for invalid slug, got %v", err)
	}
	badTokens := -5
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a2", ProviderID: p1.ID, Model: "m", MaxTokens: &badTokens}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for negative max_tokens, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a3", ProviderID: p1.ID, Model: "m", Temperature: 2.5}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for temp > 2.0, got %v", err)
	}

	// Cross-tenant provider rejected by DB composite FK (workspace_id, provider_id)
	err := s.Agents().Create(ctx, &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "cross-tenant-agent",
		Name:        "Cross Agent",
		ProviderID:  p2.ID, // belongs to ws2!
		Model:       "gpt-4o",
	})
	if !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrNotFound or ErrInvalid for cross-tenant provider FK, got %v", err)
	}

	// 3. Duplicate slug in same workspace rejected
	dupSlugErr := s.Agents().Create(ctx, &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "support-agent",
		Name:        "Duplicate Support Agent",
		ProviderID:  p1.ID,
		Model:       "gpt-4o",
	})
	if !errors.Is(dupSlugErr, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate slug in same workspace, got %v", dupSlugErr)
	}

	// 4. Same slug in different workspace allowed
	aWS2 := &domain.Agent{
		WorkspaceID: ws2.ID,
		Slug:        "support-agent",
		Name:        "Support Agent WS2",
		ProviderID:  p2.ID,
		Model:       "claude-3-5-sonnet",
	}
	if err := s.Agents().Create(ctx, aWS2); err != nil {
		t.Fatalf("expected same slug in different workspace to succeed, got %v", err)
	}

	// 5. ByID lookup
	found, err := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if found.ID != a1.ID || found.Name != "Support Agent" || found.Autonomy != domain.AutonomyApproval {
		t.Fatalf("unexpected agent retrieved: %+v", found)
	}
	if len(found.DisabledTools) != 2 || len(found.EnabledMCPS) != 1 {
		t.Fatalf("unexpected capabilities on agent: %+v", found)
	}

	// Cross-tenant ByID returns ErrNotFound
	_, err = s.Agents().ByID(ctx, ws2.ID, a1.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant ByID, got %v", err)
	}

	// Non-existent ByID
	_, err = s.Agents().ByID(ctx, ws1.ID, uuid.NewString())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent ID, got %v", err)
	}

	// Invalid UUID format
	_, err = s.Agents().ByID(ctx, ws1.ID, "invalid-uuid")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid UUID, got %v", err)
	}

	// 6. BySlug lookup
	foundSlug, err := s.Agents().BySlug(ctx, ws1.ID, "support-agent")
	if err != nil {
		t.Fatalf("unexpected BySlug error: %v", err)
	}
	if foundSlug.ID != a1.ID {
		t.Fatalf("expected ID %s, got %s", a1.ID, foundSlug.ID)
	}

	// Cross-tenant BySlug returns ErrNotFound
	_, err = s.Agents().BySlug(ctx, ws2.ID, "nonexistent-slug")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent slug, got %v", err)
	}

	// 7. ListForWorkspace
	listWS1, err := s.Agents().ListForWorkspace(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listWS1) != 1 {
		t.Fatalf("expected 1 agent in ws1, got %d", len(listWS1))
	}

	listEmpty, err := s.Agents().ListForWorkspace(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listEmpty) != 0 {
		t.Fatalf("expected 0 agents in empty workspace, got %d", len(listEmpty))
	}

	// 8. CountByProvider
	countP1, err := s.Agents().CountByProvider(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected CountByProvider error: %v", err)
	}
	if countP1 != 1 {
		t.Fatalf("expected provider count 1, got %d", countP1)
	}

	countP2InWS1, err := s.Agents().CountByProvider(ctx, ws1.ID, p2.ID)
	if err != nil {
		t.Fatalf("unexpected CountByProvider error: %v", err)
	}
	if countP2InWS1 != 0 {
		t.Fatalf("expected provider count 0, got %d", countP2InWS1)
	}

	// 9. Update
	found.Name = "Support Agent Advanced"
	found.Role = "senior-support"
	found.Autonomy = domain.AutonomySuggest
	found.DisabledTools = []string{"search_kb", "calc", "ticket_creator"}
	if err := s.Agents().Update(ctx, found); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}

	reloaded, err := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after update: %v", err)
	}
	if reloaded.Name != "Support Agent Advanced" || reloaded.Autonomy != domain.AutonomySuggest || len(reloaded.DisabledTools) != 3 {
		t.Fatalf("unexpected agent after update: %+v", reloaded)
	}

	// 10. SetPromptState
	if err := s.Agents().SetPromptState(ctx, ws1.ID, a1.ID, domain.PromptsStatusReady, nil); err != nil {
		t.Fatalf("unexpected SetPromptState error: %v", err)
	}

	readyAgent, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if readyAgent.PromptsStatus != domain.PromptsStatusReady || readyAgent.PromptsError != nil {
		t.Fatalf("unexpected ready agent: %+v", readyAgent)
	}
	if readyAgent.Identity != "" || readyAgent.Soul != "" {
		t.Fatalf("expected store to persist no prompt content, got: %+v", readyAgent)
	}

	// Transition to generating (regenerate flow)
	if err := s.Agents().SetPromptState(ctx, ws1.ID, a1.ID, domain.PromptsStatusGenerating, nil); err != nil {
		t.Fatalf("unexpected SetPromptState to generating: %v", err)
	}
	genAgent, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if genAgent.PromptsStatus != domain.PromptsStatusGenerating || genAgent.PromptsError != nil {
		t.Fatalf("expected generating status and nil error: %+v", genAgent)
	}

	// Transition to failed
	failedErr := "rate limit exceeded from provider"
	if err := s.Agents().SetPromptState(ctx, ws1.ID, a1.ID, domain.PromptsStatusFailed, &failedErr); err != nil {
		t.Fatalf("unexpected SetPromptState to failed: %v", err)
	}
	failedAgent, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if failedAgent.PromptsStatus != domain.PromptsStatusFailed || failedAgent.PromptsError == nil || *failedAgent.PromptsError != failedErr {
		t.Fatalf("expected failed status with error message: %+v", failedAgent)
	}

	// 11. SweepGenerating
	stuckAgent := &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "stuck-generating-agent",
		Name:        "Stuck Agent",
		ProviderID:  p1.ID,
		Model:       "gpt-4o",
	}
	if err := s.Agents().Create(ctx, stuckAgent); err != nil {
		t.Fatalf("unexpected create stuckAgent: %v", err)
	}

	sweptCount, err := s.Agents().SweepGenerating(ctx, "prompt generation interrupted — retry")
	if err != nil {
		t.Fatalf("unexpected SweepGenerating error: %v", err)
	}
	// stuckAgent and aWS2 were in 'generating' state
	if sweptCount < 1 {
		t.Fatalf("expected at least 1 agent swept, got %d", sweptCount)
	}

	reloadedStuck, _ := s.Agents().ByID(ctx, ws1.ID, stuckAgent.ID)
	if reloadedStuck.PromptsStatus != domain.PromptsStatusFailed || reloadedStuck.PromptsError == nil || *reloadedStuck.PromptsError != "prompt generation interrupted — retry" {
		t.Fatalf("unexpected swept agent: %+v", reloadedStuck)
	}

	// 12. Delete
	// Cross-tenant delete fails with ErrNotFound
	if err := s.Agents().Delete(ctx, ws2.ID, a1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on cross-tenant delete, got %v", err)
	}
	// Correct delete
	if err := s.Agents().Delete(ctx, ws1.ID, a1.ID); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	if _, err := s.Agents().ByID(ctx, ws1.ID, a1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if _, err := s.Agents().BySlug(ctx, ws1.ID, "support-agent"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for slug after delete, got %v", err)
	}
}

func TestIntegration_WithTx_Agents(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "tx-pg-ws", Name: "Tx PG WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create ws: %v", err)
	}

	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider: %v", err)
	}

	// Commit test
	err := s.WithTx(ctx, func(txStore store.Store) error {
		a := &domain.Agent{
			WorkspaceID: ws.ID,
			Slug:        "tx-pg-agent",
			Name:        "Tx PG Agent",
			ProviderID:  p.ID,
			Model:       "gpt-4o",
		}
		return txStore.Agents().Create(ctx, a)
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}

	// Verify committed
	aList, err := s.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(aList) != 1 {
		t.Fatalf("expected 1 agent, got %d (err: %v)", len(aList), err)
	}

	// Rollback test
	rollbackErr := errors.New("abort transaction")
	err = s.WithTx(ctx, func(txStore store.Store) error {
		a2 := &domain.Agent{
			WorkspaceID: ws.ID,
			Slug:        "rolled-back-agent",
			Name:        "Rolled Back",
			ProviderID:  p.ID,
			Model:       "gpt-4o",
		}
		if err := txStore.Agents().Create(ctx, a2); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	// Verify not committed
	_, err = s.Agents().BySlug(ctx, ws.ID, "rolled-back-agent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back agent, got %v", err)
	}
}

func TestIntegration_AgentStore_ListOrdering(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "ws-agent-ordering", Name: "Order WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}

	p := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}

	now := time.Now().UTC()
	aA := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-a",
		Name:        "Agent A",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now.Add(-2 * time.Minute),
	}
	if err := s.Agents().Create(ctx, aA); err != nil {
		t.Fatalf("create A: %v", err)
	}

	aB := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-b",
		Name:        "Agent B",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now.Add(-1 * time.Minute),
	}
	if err := s.Agents().Create(ctx, aB); err != nil {
		t.Fatalf("create B: %v", err)
	}

	aC := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-c",
		Name:        "Agent C",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now,
	}
	if err := s.Agents().Create(ctx, aC); err != nil {
		t.Fatalf("create C: %v", err)
	}

	list, err := s.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(list))
	}
	if list[0].ID != aC.ID || list[1].ID != aB.ID || list[2].ID != aA.ID {
		t.Fatalf("expected newest-first ordering [C, B, A], got [%s, %s, %s]", list[0].Slug, list[1].Slug, list[2].Slug)
	}

	// Tiebreak on ID DESC when CreatedAt is identical
	tEqual := now.Add(1 * time.Hour)
	a1 := &domain.Agent{
		ID:          "11111111-1111-1111-1111-111111111111",
		WorkspaceID: ws.ID,
		Slug:        "agent-id-1",
		Name:        "Agent ID 1",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   tEqual,
	}
	if err := s.Agents().Create(ctx, a1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	a2 := &domain.Agent{
		ID:          "22222222-2222-2222-2222-222222222222",
		WorkspaceID: ws.ID,
		Slug:        "agent-id-2",
		Name:        "Agent ID 2",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   tEqual,
	}
	if err := s.Agents().Create(ctx, a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}

	listWithTiebreak, err := s.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace with tiebreak: %v", err)
	}
	if len(listWithTiebreak) != 5 {
		t.Fatalf("expected 5 agents, got %d", len(listWithTiebreak))
	}
	// a2 has higher ID than a1, both newer than C, B, A
	if listWithTiebreak[0].ID != a2.ID || listWithTiebreak[1].ID != a1.ID {
		t.Fatalf("expected tiebreak [a2, a1], got [%s, %s]", listWithTiebreak[0].Slug, listWithTiebreak[1].Slug)
	}
}
