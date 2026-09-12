package fake_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// TestProviderStore_CatalogProviderRoundTrip covers the catalog_provider hint
// (fix-image-attachment-lane D3): the fake persists it on create, echoes it on
// reads, replaces it on update, and clears it back to empty.
func TestProviderStore_CatalogProviderRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "catalog-hint-ws", Name: "Catalog Hint WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}

	p := &domain.ProviderConfig{
		WorkspaceID:     ws.ID,
		Type:            "openai-compatible",
		Name:            "Z.ai Gateway",
		BaseURL:         "https://api.z.ai/api/paas/v4",
		CatalogProvider: "zai-coding-plan",
		Enabled:         true,
	}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}

	found, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if found.CatalogProvider != "zai-coding-plan" {
		t.Fatalf("CatalogProvider after create = %q, want %q", found.CatalogProvider, "zai-coding-plan")
	}

	// List round-trips the hint too.
	list, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("unexpected list: %v (%d providers)", err, len(list))
	}
	if list[0].CatalogProvider != "zai-coding-plan" {
		t.Fatalf("CatalogProvider in list = %q, want %q", list[0].CatalogProvider, "zai-coding-plan")
	}

	// Update replaces the hint.
	found.CatalogProvider = "zhipuai-coding-plan"
	if err := s.Providers().Update(ctx, found); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after update: %v", err)
	}
	if reloaded.CatalogProvider != "zhipuai-coding-plan" {
		t.Fatalf("CatalogProvider after update = %q, want %q", reloaded.CatalogProvider, "zhipuai-coding-plan")
	}

	// Update clears the hint back to empty (auto-detect).
	reloaded.CatalogProvider = ""
	if err := s.Providers().Update(ctx, reloaded); err != nil {
		t.Fatalf("unexpected clear update error: %v", err)
	}
	cleared, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after clear: %v", err)
	}
	if cleared.CatalogProvider != "" {
		t.Fatalf("CatalogProvider after clear = %q, want empty", cleared.CatalogProvider)
	}
}
