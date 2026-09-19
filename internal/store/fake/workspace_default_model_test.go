package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// TestWorkspaceStore_DefaultModelRoundTrip covers the workspace default-model
// pair through the fake store: create-with pair, update-replace, and clear
// (refactor-workspace-settings task 1.3).
func TestWorkspaceStore_DefaultModelRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "default-model-ws", Name: "Default Model WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	other := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "anthropic", Name: "Anthropic", Enabled: true}
	if err := s.Providers().Create(ctx, other); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Read back before any pair is set: unset.
	got, err := s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if got.DefaultModel != nil {
		t.Fatalf("expected nil default model before set, got %+v", got.DefaultModel)
	}

	// Set the pair via Update and read it back.
	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: prov.ID, Model: "gpt-4o"}
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("update workspace: %v", err)
	}
	got, err = s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if got.DefaultModel == nil || got.DefaultModel.ProviderID != prov.ID || got.DefaultModel.Model != "gpt-4o" {
		t.Fatalf("expected stored pair, got %+v", got.DefaultModel)
	}

	// Replace with another provider.
	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: other.ID, Model: "claude-sonnet-4-5"}
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("update workspace: %v", err)
	}
	got, err = s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if got.DefaultModel == nil || got.DefaultModel.ProviderID != other.ID {
		t.Fatalf("expected replaced pair, got %+v", got.DefaultModel)
	}

	// Clear.
	ws.DefaultModel = nil
	if err := s.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("update workspace: %v", err)
	}
	got, err = s.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if got.DefaultModel != nil {
		t.Fatalf("expected cleared pair, got %+v", got.DefaultModel)
	}
}

// TestAgentStore_NullPairRoundTrip covers inheriting agents: the empty
// provider/model pair persists and reads back, half pairs are refused, and
// CountInheriting counts only the empty-pair rows.
func TestAgentStore_NullPairRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "null-pair-ws", Name: "Null Pair WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
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
		Brief:       "Pinned to a provider",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := s.Agents().Create(ctx, pinned); err != nil {
		t.Fatalf("create pinned agent: %v", err)
	}

	for _, slug := range []string{"inheritor", "pinned"} {
		a, err := s.Agents().BySlug(ctx, ws.ID, slug)
		if err != nil {
			t.Fatalf("read agent %s: %v", slug, err)
		}
		if slug == "inheritor" && (a.ProviderID != "" || a.Model != "") {
			t.Fatalf("expected empty pair on inheritor, got %q/%q", a.ProviderID, a.Model)
		}
		if slug == "pinned" && (a.ProviderID != prov.ID || a.Model != "gpt-4o") {
			t.Fatalf("expected pinned pair preserved, got %q/%q", a.ProviderID, a.Model)
		}
	}

	// Half pairs never persist.
	half := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "half",
		Name:        "Half",
		Role:        "assistant",
		Brief:       "Half pair",
		ProviderID:  prov.ID,
	}
	if err := s.Agents().Create(ctx, half); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected invalid error on half pair create, got %v", err)
	}

	// CountInheriting is workspace-scoped.
	other := &domain.Workspace{Slug: "null-pair-other", Name: "Null Pair Other"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	count, err := s.Agents().CountInheriting(ctx, ws.ID)
	if err != nil {
		t.Fatalf("count inheriting: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 inheriting agent, got %d", count)
	}
	count, err = s.Agents().CountInheriting(ctx, other.ID)
	if err != nil {
		t.Fatalf("count inheriting other: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 inheriting agents in other workspace, got %d", count)
	}

	// An update may switch a pinned agent to inherit.
	pinned.ProviderID = ""
	pinned.Model = ""
	if err := s.Agents().Update(ctx, pinned); err != nil {
		t.Fatalf("update to inherit: %v", err)
	}
	count, err = s.Agents().CountInheriting(ctx, ws.ID)
	if err != nil {
		t.Fatalf("count inheriting: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 inheriting agents after switch, got %d", count)
	}
}
