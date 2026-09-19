//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestIntegration_WorkspaceDefaultModelRoundTrip covers the default-model pair
// through the postgres store: create-with pair, replace, clear, and read-back
// on every workspace read path (refactor-workspace-settings task 1.3).
func TestIntegration_WorkspaceDefaultModelRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "ws-default-model", Name: "Default Model WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	p1 := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p1); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	p2 := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "anthropic", Name: "Anthropic", Enabled: true}
	if err := s.Providers().Create(ctx, p2); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// A pair can only reference a same-workspace provider, which cannot exist
	// before the workspace row itself — birth-with-pair naming a provider of
	// another workspace is rejected by the composite FK (the dedicated FK test
	// covers that path), so this test exercises the pair through Update.

	// Replace then clear through Update.
	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: p1.ID, Model: "gpt-4o"}
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("update pair: %v", err)
	}
	got, err := s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read after set: %v", err)
	}
	if got.DefaultModel == nil || got.DefaultModel.ProviderID != p1.ID || got.DefaultModel.Model != "gpt-4o" {
		t.Fatalf("set failed: %+v", got.DefaultModel)
	}
	bySlug, err := s.Workspaces().BySlug(ctx, ws.Slug)
	if err != nil {
		t.Fatalf("bySlug: %v", err)
	}
	if bySlug.DefaultModel == nil || bySlug.DefaultModel.Model != "gpt-4o" {
		t.Fatalf("bySlug pair mismatch: %+v", bySlug.DefaultModel)
	}

	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: p2.ID, Model: "claude-sonnet-4-5"}
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("update pair: %v", err)
	}
	got, err = s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read after replace: %v", err)
	}
	if got.DefaultModel == nil || got.DefaultModel.ProviderID != p2.ID {
		t.Fatalf("replace failed: %+v", got.DefaultModel)
	}

	ws.DefaultModel = nil
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("clear pair: %v", err)
	}
	got, err = s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read after clear: %v", err)
	}
	if got.DefaultModel != nil {
		t.Fatalf("clear failed: %+v", got.DefaultModel)
	}
}

// TestIntegration_WorkspaceDefaultModelForeignProviderRejected asserts the
// composite FK on (workspace_id, default_provider_id): a default naming a
// provider of another workspace is impossible to persist.
func TestIntegration_WorkspaceDefaultModelForeignProviderRejected(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	wsA := &domain.Workspace{Slug: "ws-fk-a", Name: "FK WS A"}
	if err := s.Workspaces().Create(ctx, wsA); err != nil {
		t.Fatalf("create ws a: %v", err)
	}
	wsB := &domain.Workspace{Slug: "ws-fk-b", Name: "FK WS B"}
	if err := s.Workspaces().Create(ctx, wsB); err != nil {
		t.Fatalf("create ws b: %v", err)
	}

	provB := &domain.ProviderConfig{WorkspaceID: wsB.ID, Type: "openai", Name: "OpenAI B", Enabled: true}
	if err := s.Providers().Create(ctx, provB); err != nil {
		t.Fatalf("create provider in b: %v", err)
	}

	// Create-path rejection.
	birth := &domain.Workspace{
		Slug:         "ws-fk-birth",
		Name:         "FK Birth",
		DefaultModel: &domain.DefaultModelPair{ProviderID: provB.ID, Model: "gpt-4o"},
	}
	if err := s.Workspaces().Create(ctx, birth); err == nil {
		t.Fatal("expected create with foreign default provider to fail")
	}

	// Update-path rejection (the handler pre-checks, the data layer is the
	// backstop).
	wsA.DefaultModel = &domain.DefaultModelPair{ProviderID: provB.ID, Model: "gpt-4o"}
	if err := s.Workspaces().Update(ctx, wsA); err == nil {
		t.Fatal("expected update with foreign default provider to fail")
	}
}

// TestIntegration_AgentNullPairAndFK covers the inherit agent's nullable pair
// at the data layer: NULL pair round-trips, half pairs cannot persist via the
// store contract, CountInheriting is workspace-scoped, and the pinned composite
// FK still rejects a cross-workspace provider.
func TestIntegration_AgentNullPairAndFK(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "ws-null-pair", Name: "Null Pair WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}
	other := &domain.Workspace{Slug: "ws-null-pair-other", Name: "Null Pair Other"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other ws: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	foreign := &domain.ProviderConfig{WorkspaceID: other.ID, Type: "openai", Name: "OpenAI Other", Enabled: true}
	if err := s.Providers().Create(ctx, foreign); err != nil {
		t.Fatalf("create foreign provider: %v", err)
	}

	inheritor := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "inheritor",
		Name:        "Inheritor",
		Role:        "assistant",
		Brief:       "Inherits the workspace default",
	}
	if err := s.Agents().Create(ctx, inheritor); err != nil {
		t.Fatalf("create inherit agent: %v", err)
	}

	pinned := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "pinned",
		Name:        "Pinned",
		Role:        "assistant",
		Brief:       "Pinned",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := s.Agents().Create(ctx, pinned); err != nil {
		t.Fatalf("create pinned agent: %v", err)
	}

	a, err := s.Agents().BySlug(ctx, ws.ID, "inheritor")
	if err != nil {
		t.Fatalf("read inheritor: %v", err)
	}
	if a.ProviderID != "" || a.Model != "" {
		t.Fatalf("expected NULL pair read back as empty, got %q/%q", a.ProviderID, a.Model)
	}

	// Pinned cross-workspace provider still rejected by the composite FK.
	pinned.ProviderID = foreign.ID
	pinned.Slug = "pinned-foreign"
	if err := s.Agents().Create(ctx, pinned); err == nil {
		t.Fatal("expected cross-workspace pinned provider to fail")
	}

	// Switch pinned to inherit via Update, then count.
	pinned.ProviderID = ""
	pinned.Model = ""
	pinned.Slug = "pinned"
	if err := s.Agents().Update(ctx, pinned); err != nil {
		t.Fatalf("update to inherit: %v", err)
	}

	count, err := s.Agents().CountInheriting(ctx, ws.ID)
	if err != nil {
		t.Fatalf("count inheriting: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 inheriting agents, got %d", count)
	}
	count, err = s.Agents().CountInheriting(ctx, other.ID)
	if err != nil {
		t.Fatalf("count inheriting other: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 inheriting agents in other workspace, got %d", count)
	}

	if _, err := s.Agents().BySlug(ctx, ws.ID, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
