//go:build integration

package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// TestIntegration_ProviderStore_CatalogProviderRoundTrip covers the
// catalog_provider hint (fix-image-attachment-lane D3): the column persists on
// create, echoes on reads, replaces on update, and clears back to empty.
func TestIntegration_ProviderStore_CatalogProviderRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

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
		t.Fatalf("CatalogProvider after create = %q, want zai-coding-plan", found.CatalogProvider)
	}

	list, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("unexpected list: %v (%d providers)", err, len(list))
	}
	if list[0].CatalogProvider != "zai-coding-plan" {
		t.Fatalf("CatalogProvider in list = %q, want zai-coding-plan", list[0].CatalogProvider)
	}

	// Update replaces the hint, then clears it (empty = auto-detect).
	found.CatalogProvider = "zhipuai-coding-plan"
	if err := s.Providers().Update(ctx, found); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after update: %v", err)
	}
	if reloaded.CatalogProvider != "zhipuai-coding-plan" {
		t.Fatalf("CatalogProvider after update = %q, want zhipuai-coding-plan", reloaded.CatalogProvider)
	}

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

// TestIntegration_WorkspaceProviders_CatalogProviderMigration covers migration
// 000046 (fix-image-attachment-lane D3): providers existing before the upgrade
// carry the column default (empty = unmapped), a stored hint survives a
// down/up round trip, and the down migration drops the column.
func TestIntegration_WorkspaceProviders_CatalogProviderMigration(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)

	ws := &domain.Workspace{Slug: "hint-mig-ws", Name: "Hint Migration WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}

	p := &domain.ProviderConfig{
		WorkspaceID:     ws.ID,
		Type:            "openai-compatible",
		Name:            "Hinted Gateway",
		CatalogProvider: "zai-coding-plan",
		Enabled:         true,
	}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}

	mig := postgres.NewMigrator(schemaDSN)
	before, dirty, err := mig.Status()
	if err != nil || dirty {
		t.Fatalf("failed to read version before down: v=%d dirty=%v err=%v", before, dirty, err)
	}
	if before < 46 {
		t.Fatalf("expected version >= 46 before down, got %d", before)
	}

	// Down to the pre-000046 world: the column is gone.
	if err := mig.MigrateToVersion(45); err != nil {
		t.Fatalf("failed to migrate down to 000045: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 45 || dirty {
		t.Fatalf("expected clean version 45 after down, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'workspace_providers' AND column_name = 'catalog_provider')`,
	).Scan(&exists); err != nil {
		t.Fatalf("failed to probe catalog_provider column after down: %v", err)
	}
	if exists {
		t.Fatal("expected catalog_provider column to be dropped after down migration")
	}

	// Back up: the column returns. Dropping a column destroys its data by
	// design, so the pre-upgrade row reads as unmapped (default '') — the
	// host suggestion re-fills the form; nothing else about the row changed.
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to re-run migrations above 000045: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != before || dirty {
		t.Fatalf("expected clean version %d after up, got v=%d dirty=%v err=%v", before, v, dirty, err)
	}

	found, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after round trip: %v", err)
	}
	if found.CatalogProvider != "" {
		t.Fatalf("CatalogProvider after down/up round trip = %q, want empty (column drop resets to unmapped)", found.CatalogProvider)
	}
	if found.Type != "openai-compatible" || found.Name != "Hinted Gateway" || !found.Enabled {
		t.Fatalf("provider row damaged by round trip: %+v", found)
	}

	// The restored column stores and reads a fresh hint.
	found.CatalogProvider = "zai-coding-plan"
	if err := s.Providers().Update(ctx, found); err != nil {
		t.Fatalf("unexpected update after round trip: %v", err)
	}
	rehinted, err := s.Providers().ByID(ctx, ws.ID, p.ID)
	if err != nil {
		t.Fatalf("unexpected ByID for rehinted provider: %v", err)
	}
	if rehinted.CatalogProvider != "zai-coding-plan" {
		t.Fatalf("CatalogProvider after re-write = %q, want zai-coding-plan", rehinted.CatalogProvider)
	}

	// A fresh row without a hint reads as empty (unmapped), not NULL.
	fresh := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "No Hint",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, fresh); err != nil {
		t.Fatalf("unexpected create fresh provider error: %v", err)
	}
	reloaded, err := s.Providers().ByID(ctx, ws.ID, fresh.ID)
	if err != nil {
		t.Fatalf("unexpected ByID for fresh provider: %v", err)
	}
	if reloaded.CatalogProvider != "" {
		t.Fatalf("CatalogProvider default = %q, want empty", reloaded.CatalogProvider)
	}
}
