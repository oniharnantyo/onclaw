//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The graph stores' live-Postgres contract (wave3-memory-vectors-and-graph):
// the same table the fake tests run, exercised against the real schema —
// halfvec casts and cosine ordering through the pgvector extension, the
// trigram label fallback through pg_trgm, the unique-index upsert and
// idempotency semantics, and the structural visibility scope computed in the
// WHERE clause (D4/D5/D7/D8/D10).

type graphFixtures struct {
	wsID     string
	otherID  string
	userA    string // member A
	userB    string // member B (the stranger)
	agentX   string // serving agent
	agentY   string // a foreign agent
	learned  time.Time
}

func seedGraphFixtures(t *testing.T, s store.Store) *graphFixtures {
	t.Helper()
	wsID, userA, userB, agentX, agentY := seedMemoryScopesFixtures(t, s)
	other := &domain.Workspace{Slug: "pg-graph-rival", Name: "Rival"}
	if err := s.Workspaces().Create(context.Background(), other); err != nil {
		t.Fatalf("create rival workspace: %v", err)
	}
	return &graphFixtures{
		wsID:    wsID,
		otherID: other.ID,
		userA:   userA,
		userB:   userB,
		agentX:  agentX,
		agentY:  agentY,
		learned: time.Date(2026, 9, 15, 10, 0, 1, 0, time.UTC),
	}
}

func (f *graphFixtures) seedNote(t *testing.T, s store.Store, visibility domain.MemoryVisibility, ownerID, content string) *domain.MemoryNote {
	t.Helper()
	note := newPGMemoryNote(f.wsID, visibility, ownerID, content)
	if err := s.MemoryNotes().InsertNote(context.Background(), note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	return note
}

func (f *graphFixtures) seedEvent(t *testing.T, s store.Store, visibility domain.MemoryVisibility, ownerID, description string) *domain.MemoryEvent {
	t.Helper()
	event := newPGMemoryEvent(f.wsID, f.agentX, "sess-graph", "turn-graph", visibility, ownerID, description)
	if err := s.MemoryEvents().InsertEvent(context.Background(), event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return event
}

func (f *graphFixtures) embedding(targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility, ownerID string, vec ...float32) domain.MemoryEmbedding {
	e := domain.MemoryEmbedding{
		WorkspaceID:   f.wsID,
		TargetType:    targetType,
		TargetID:      targetID,
		Dimension:     len(vec),
		Embedding:     vec,
		Visibility:    visibility,
		SourceEventID: "evt_" + targetID,
		LearnedAt:     f.learned,
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		e.UserID = &ownerID
	case domain.MemoryVisibilityAgent:
		e.AgentID = &ownerID
	}
	return e
}

func (f *graphFixtures) entity(label string) *domain.MemoryEntity {
	return &domain.MemoryEntity{
		WorkspaceID:     f.wsID,
		Label:           label,
		NormalizedLabel: domain.NormalizeEntityLabel(label),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt_entity",
		LearnedAt:       f.learned,
	}
}

func TestIntegration_MemoryEmbeddings_InsertSearchDimensionAndVisibility(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	f := seedGraphFixtures(t, s)

	// Cosine ordering: the query is closest to raw-2.
	first := f.embedding(domain.MemoryTargetRaw, "raw-1", domain.MemoryVisibilityShared, "", 1, 0, 0)
	second := f.embedding(domain.MemoryTargetRaw, "raw-2", domain.MemoryVisibilityShared, "", 0, 1, 0)
	// A stale-dimension row (the pre-change embedding model) coexisting.
	stale := f.embedding(domain.MemoryTargetRaw, "raw-3", domain.MemoryVisibilityShared, "", 1, 1)
	stale.Dimension = 2

	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{first, second, stale}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	results, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentX,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0.1, 0.9, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 hits at dimension 3 (stale dimension excluded), got %d", len(results))
	}
	if results[0].TargetID != "raw-2" || results[1].TargetID != "raw-1" {
		t.Fatalf("expected cosine ordering raw-2 then raw-1, got %s then %s", results[0].TargetID, results[1].TargetID)
	}
	if results[0].SourceEventID != "evt_raw-2" {
		t.Errorf("expected the citation pointer on the hit, got %q", results[0].SourceEventID)
	}
	if results[0].Embedding != nil {
		t.Error("search results must not carry the vector")
	}

	// The stale dimension stays retrievable at its own dimension (D5:
	// exclusion, not deletion).
	staleHits, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentX, nil, 2, []float32{1, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("SearchByVector at dimension 2: %v", err)
	}
	if len(staleHits) != 1 || staleHits[0].TargetID != "raw-3" {
		t.Fatalf("expected the stale-dimension row at its own dimension, got %+v", staleHits)
	}

	// Validation: dimension mismatch and non-positive dimension.
	if _, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, "", "", nil, 3, []float32{1, 0}, store.MemoryEmbeddingFilters{}, 5); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for query/dimension mismatch, got %v", err)
	}
	if _, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, "", "", nil, 0, nil, store.MemoryEmbeddingFilters{}, 5); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for zero dimension, got %v", err)
	}

	// Upsert: re-embed at the same dimension replaces the vector, and the
	// created_at/id identity stays.
	original := results
	revised := f.embedding(domain.MemoryTargetRaw, "raw-2", domain.MemoryVisibilityShared, "", 0, 0, 1)
	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{revised}); err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	after, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentX,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search after re-embed: %v", err)
	}
	if len(after) < 1 || after[0].TargetID != "raw-2" {
		t.Fatalf("expected the re-embedded row nearest, got %+v", after)
	}
	if len(original) == 2 && after[0].ID != original[0].ID {
		// original[0] is raw-2: identity must survive the upsert.
		t.Fatalf("upsert must keep the row id, got %s vs %s", after[0].ID, original[0].ID)
	}
	if len(original) == 2 && !after[0].CreatedAt.Equal(original[0].CreatedAt) {
		t.Errorf("upsert must keep created_at, got %v vs %v", after[0].CreatedAt, original[0].CreatedAt)
	}

	// DeleteByTarget removes exactly the target's rows.
	removed, err := s.MemoryEmbeddings().DeleteByTarget(ctx, f.wsID, domain.MemoryTargetRaw, "raw-2")
	if err != nil {
		t.Fatalf("DeleteByTarget: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed embedding, got %d", removed)
	}
	hits, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentX,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	for _, hit := range hits {
		if hit.TargetID == "raw-2" {
			t.Fatal("the deleted target's embeddings must be gone")
		}
	}
}

func TestIntegration_MemoryEmbeddings_ScopeFollowsTheLiveRow(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	f := seedGraphFixtures(t, s)

	// Raw rows filter through their own snapshot columns.
	rawUser := f.embedding(domain.MemoryTargetRaw, "raw-a", domain.MemoryVisibilityUser, f.userA, 1, 0, 0)
	rawShared := f.embedding(domain.MemoryTargetRaw, "raw-s", domain.MemoryVisibilityShared, "", 0, 1, 0)
	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{rawUser, rawShared}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	ownerHits, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userA, f.agentX, nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("owner search: %v", err)
	}
	if len(ownerHits) != 2 || ownerHits[0].TargetID != "raw-a" {
		t.Fatalf("expected the owner's raw row nearest and both rows visible, got %+v", ownerHits)
	}
	strangerHits, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentY, nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("stranger search: %v", err)
	}
	if len(strangerHits) != 1 || strangerHits[0].TargetID != "raw-s" {
		t.Fatalf("expected the stranger to see only the shared row, got %+v", strangerHits)
	}

	// Note embeddings take scope from the LIVE linked row: hidden until the
	// human promotion widens the note itself.
	userNote := f.seedNote(t, s, domain.MemoryVisibilityUser, f.userA, "private atlas fact")
	noteEmbedding := f.embedding(domain.MemoryTargetNote, userNote.ID, domain.MemoryVisibilityUser, f.userA, 0, 0, 1)
	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{noteEmbedding}); err != nil {
		t.Fatalf("InsertEmbeddings(note): %v", err)
	}
	hits, err := s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentY, nil, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("pre-promotion search: %v", err)
	}
	for _, hit := range hits {
		if hit.TargetID == userNote.ID {
			t.Fatal("the private note's embedding must be hidden before promotion")
		}
	}
	if err := s.MemoryNotes().PromoteNote(ctx, f.wsID, userNote.ID, f.userA); err != nil {
		t.Fatalf("PromoteNote: %v", err)
	}
	hits, err = s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentY, nil, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("post-promotion search: %v", err)
	}
	found := false
	for _, hit := range hits {
		if hit.TargetID == userNote.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected promotion to widen reach through the row, got %+v", hits)
	}

	// Tombstoning the linked event kills its embedding at read time.
	sharedEvent := f.seedEvent(t, s, domain.MemoryVisibilityShared, "", "atlas shipped")
	eventEmbedding := f.embedding(domain.MemoryTargetEvent, sharedEvent.ID, domain.MemoryVisibilityShared, "", 1, 1, 1)
	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{eventEmbedding}); err != nil {
		t.Fatalf("InsertEmbeddings(event): %v", err)
	}
	if err := s.MemoryEvents().TombstoneEvent(ctx, f.wsID, sharedEvent.ID); err != nil {
		t.Fatalf("TombstoneEvent: %v", err)
	}
	hits, err = s.MemoryEmbeddings().SearchByVector(ctx, f.wsID, f.userB, f.agentX, nil, 3, []float32{1, 1, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search after event tombstone: %v", err)
	}
	for _, hit := range hits {
		if hit.TargetID == sharedEvent.ID {
			t.Fatal("the tombstoned event's embedding must be excluded at read time")
		}
	}
}

func TestIntegration_MemoryEntities_ResolveFindAndTenantIsolation(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	f := seedGraphFixtures(t, s)

	proposed := f.entity("Sari")
	if err := s.MemoryEntities().ResolveEntity(ctx, proposed); err != nil {
		t.Fatalf("ResolveEntity: %v", err)
	}

	again := f.entity("  SARIS ")
	if err := s.MemoryEntities().ResolveEntity(ctx, again); err != nil {
		t.Fatalf("second ResolveEntity: %v", err)
	}
	if again.ID != proposed.ID {
		t.Fatalf("expected the same entity row, got %s vs %s", again.ID, proposed.ID)
	}
	if again.Label != "Sari" {
		t.Errorf("expected the first label's spelling to win, got %q", again.Label)
	}
	if !again.CreatedAt.Equal(proposed.CreatedAt) {
		t.Errorf("expected the birth created_at, got %v vs %v", again.CreatedAt, proposed.CreatedAt)
	}

	// Drifted normalization is rejected.
	drifted := f.entity("Beacon")
	drifted.NormalizedLabel = "beacons"
	if err := s.MemoryEntities().ResolveEntity(ctx, drifted); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for normalization drift, got %v", err)
	}

	// Tenant isolation: the same label in another workspace is a new row.
	otherEntity := f.entity("Sari")
	otherEntity.WorkspaceID = f.otherID
	if err := s.MemoryEntities().ResolveEntity(ctx, otherEntity); err != nil {
		t.Fatalf("ResolveEntity in rival workspace: %v", err)
	}
	if otherEntity.ID == proposed.ID {
		t.Fatal("entities must never be shared across workspaces")
	}

	byLabel, err := s.MemoryEntities().GetEntityByLabel(ctx, f.wsID, "sari")
	if err != nil || byLabel == nil || byLabel.ID != proposed.ID {
		t.Fatalf("GetEntityByLabel: %+v, err %v", byLabel, err)
	}
	missing, err := s.MemoryEntities().GetEntityByLabel(ctx, f.wsID, "nonexistent")
	if err != nil || missing != nil {
		t.Fatalf("expected (nil, nil) for unknown label, got (%+v, %v)", missing, err)
	}

	// Seed fallback: exact first, then prefix, then trigram similarity.
	labels := []string{"Project Atlas", "project atlas docs", "Atlas", "Unrelated"}
	ids := make(map[string]string)
	for _, label := range labels {
		e := f.entity(label)
		if err := s.MemoryEntities().ResolveEntity(ctx, e); err != nil {
			t.Fatalf("ResolveEntity(%q): %v", label, err)
		}
		ids[label] = e.ID
	}
	found, err := s.MemoryEntities().FindEntitiesByLabelPrefix(ctx, f.wsID, "project atlas", 0)
	if err != nil {
		t.Fatalf("FindEntitiesByLabelPrefix: %v", err)
	}
	if len(found) == 0 || found[0].ID != ids["Project Atlas"] {
		t.Fatalf("expected the exact match first, got %+v", found)
	}
	found, err = s.MemoryEntities().FindEntitiesByLabelPrefix(ctx, f.wsID, "atlas", 10)
	if err != nil {
		t.Fatalf("FindEntitiesByLabelPrefix(atlas): %v", err)
	}
	if len(found) != 3 {
		t.Fatalf("expected the 3 atlas-ish labels, got %+v", found)
	}
	for _, e := range found {
		if e.ID == ids["Unrelated"] {
			t.Fatal("the unrelated label must not match")
		}
	}
}

func TestIntegration_MemoryEntities_EdgesVisibilityIdempotencyAndFold(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	f := seedGraphFixtures(t, s)

	survivor := f.entity("Acme Corp")
	if err := s.MemoryEntities().ResolveEntity(ctx, survivor); err != nil {
		t.Fatalf("ResolveEntity(survivor): %v", err)
	}
	duplicate := f.entity("Acme Corp.")
	if err := s.MemoryEntities().ResolveEntity(ctx, duplicate); err != nil {
		t.Fatalf("ResolveEntity(duplicate): %v", err)
	}

	userNote := f.seedNote(t, s, domain.MemoryVisibilityUser, f.userA, "acme private note")
	sharedEvent := f.seedEvent(t, s, domain.MemoryVisibilityShared, "", "acme shipped")
	tombstonedNote := f.seedNote(t, s, domain.MemoryVisibilityShared, "", "dead note")
	if err := s.MemoryNotes().TombstoneNote(ctx, f.wsID, tombstonedNote.ID); err != nil {
		t.Fatalf("TombstoneNote: %v", err)
	}

	edge := func(entityID string, targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility, ownerID string) domain.MemoryEntityEdge {
		// Edges carry no owner columns (D7 — all gating lives on the linked
		// row); ownerID is used only by the note/event seeders.
		_ = ownerID
		return domain.MemoryEntityEdge{
			EntityID:      entityID,
			TargetType:    targetType,
			TargetID:      targetID,
			Visibility:    visibility,
			Origin:        domain.MemoryOriginInfer,
			SourceEventID: "evt_edge_" + targetID,
		}
	}

	batch := []domain.MemoryEntityEdge{
		edge(survivor.ID, domain.MemoryTargetNote, userNote.ID, domain.MemoryVisibilityUser, f.userA),
		edge(survivor.ID, domain.MemoryTargetEvent, sharedEvent.ID, domain.MemoryVisibilityShared, ""),
		edge(survivor.ID, domain.MemoryTargetNote, tombstonedNote.ID, domain.MemoryVisibilityShared, ""),
		edge("not-an-entity", domain.MemoryTargetEvent, sharedEvent.ID, domain.MemoryVisibilityShared, ""),
	}
	inserted, err := s.MemoryEntities().AddEdges(ctx, f.wsID, batch)
	if err != nil {
		t.Fatalf("AddEdges: %v", err)
	}
	if inserted != 3 {
		t.Fatalf("expected 3 edges (foreign entity dropped), got %d", inserted)
	}

	// Re-mention inserts nothing.
	replay, err := s.MemoryEntities().AddEdges(ctx, f.wsID, batch[:1])
	if err != nil {
		t.Fatalf("replayed AddEdges: %v", err)
	}
	if replay != 0 {
		t.Fatalf("expected the replay to insert nothing, got %d", replay)
	}

	// Edge reads gate through the live linked row: the user-tier note edge is
	// the owner's only, the dead note's edge is gone for everyone, the
	// shared event edge is visible to both.
	ownerEdges, err := s.MemoryEntities().ListEdgesForEntities(ctx, f.wsID, f.userA, f.agentX, []string{survivor.ID})
	if err != nil {
		t.Fatalf("owner ListEdgesForEntities: %v", err)
	}
	if len(ownerEdges) != 2 {
		t.Fatalf("expected the owner to see 2 live edges, got %+v", ownerEdges)
	}
	strangerEdges, err := s.MemoryEntities().ListEdgesForEntities(ctx, f.wsID, f.userB, f.agentY, []string{survivor.ID})
	if err != nil {
		t.Fatalf("stranger ListEdgesForEntities: %v", err)
	}
	if len(strangerEdges) != 1 || strangerEdges[0].TargetID != sharedEvent.ID {
		t.Fatalf("expected the stranger to see only the shared edge, got %+v", strangerEdges)
	}

	// Fold: the duplicate's colliding edge stays, its unique edge moves with
	// provenance intact.
	dupOnlyNote := f.seedNote(t, s, domain.MemoryVisibilityShared, "", "duplicate-only fact")
	if _, err := s.MemoryEntities().AddEdges(ctx, f.wsID, []domain.MemoryEntityEdge{
		edge(duplicate.ID, domain.MemoryTargetNote, userNote.ID, domain.MemoryVisibilityUser, f.userA), // collision with survivor's
		edge(duplicate.ID, domain.MemoryTargetNote, dupOnlyNote.ID, domain.MemoryVisibilityShared, ""),
	}); err != nil {
		t.Fatalf("AddEdges(duplicate): %v", err)
	}
	moved, err := s.MemoryEntities().FoldEntity(ctx, f.wsID, duplicate.ID, survivor.ID)
	if err != nil {
		t.Fatalf("FoldEntity: %v", err)
	}
	if moved != 1 {
		t.Fatalf("expected 1 edge re-pointed (the colliding one stays), got %d", moved)
	}
	survivorEdges, err := s.MemoryEntities().ListEdgesForEntities(ctx, f.wsID, f.userA, f.agentX, []string{survivor.ID})
	if err != nil {
		t.Fatalf("ListEdgesForEntities after fold: %v", err)
	}
	var movedEdge *domain.MemoryEntityEdge
	for i := range survivorEdges {
		if survivorEdges[i].TargetID == dupOnlyNote.ID {
			movedEdge = &survivorEdges[i]
		}
	}
	if movedEdge == nil {
		t.Fatal("expected the duplicate-only edge on the survivor after the fold")
	}
	if movedEdge.Origin != domain.MemoryOriginInfer || movedEdge.SourceEventID != "evt_edge_"+dupOnlyNote.ID {
		t.Errorf("fold must preserve provenance, got %+v", movedEdge)
	}

	// Guard rails.
	if _, err := s.MemoryEntities().FoldEntity(ctx, f.wsID, survivor.ID, survivor.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for a self-fold, got %v", err)
	}
	if _, err := s.MemoryEntities().FoldEntity(ctx, f.otherID, duplicate.ID, survivor.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound across the tenant boundary, got %v", err)
	}
	if _, err := s.MemoryEntities().FoldEntity(ctx, f.wsID, "00000000-0000-0000-0000-000000000000", survivor.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for an unknown duplicate, got %v", err)
	}
}

func TestIntegration_MemoryGraph_WithTxCommit(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	f := seedGraphFixtures(t, s)

	// The new sub-stores participate in the transaction seam like every
	// other sub-store: the WithTx store's accessors run on the tx executor.
	err := s.WithTx(ctx, func(tx store.Store) error {
		entity := f.entity("Tx Entity")
		if err := tx.MemoryEntities().ResolveEntity(ctx, entity); err != nil {
			return err
		}
		note := f.seedNote(t, tx, domain.MemoryVisibilityShared, "", "tx fact")
		batch := []domain.MemoryEntityEdge{{
			EntityID:      entity.ID,
			TargetType:    domain.MemoryTargetNote,
			TargetID:      note.ID,
			Visibility:    domain.MemoryVisibilityShared,
			Origin:        domain.MemoryOriginDialogue,
			SourceEventID: "evt_tx",
		}}
		_, err := tx.MemoryEntities().AddEdges(ctx, f.wsID, batch)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	entity, err := s.MemoryEntities().GetEntityByLabel(ctx, f.wsID, "tx entity")
	if err != nil || entity == nil {
		t.Fatalf("expected the committed entity, got (%+v, %v)", entity, err)
	}
	edges, err := s.MemoryEntities().ListEdgesForEntities(ctx, f.wsID, f.userA, f.agentX, []string{entity.ID})
	if err != nil || len(edges) != 1 {
		t.Fatalf("expected the committed edge, got (%+v, %v)", edges, err)
	}
}
