package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// The graph stores' fake contract (wave3-memory-vectors-and-graph): the
// structural visibility predicate applies at read time with note/event scope
// taken from the LIVE linked row, the SQL unique indexes' upsert/idempotency
// semantics are mirrored exactly, and folds re-point without deleting.

type graphFixture struct {
	store     store.Store
	workspace *domain.Workspace
	other     *domain.Workspace // second tenant for isolation assertions
	user      *domain.User
	stranger  *domain.User
	agent     *domain.Agent
}

func newGraphFixture(t *testing.T) *graphFixture {
	t.Helper()
	ctx := context.Background()
	s := fake.New()
	f := &graphFixture{store: s}

	f.workspace = &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := s.Workspaces().Create(ctx, f.workspace); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	f.other = &domain.Workspace{Slug: "rival", Name: "Rival"}
	if err := s.Workspaces().Create(ctx, f.other); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	f.user = &domain.User{Email: "owner@example.com", Name: "Owner"}
	if err := s.Users().Create(ctx, f.user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	f.stranger = &domain.User{Email: "stranger@example.com", Name: "Stranger"}
	if err := s.Users().Create(ctx, f.stranger); err != nil {
		t.Fatalf("seed stranger: %v", err)
	}
	f.agent = &domain.Agent{WorkspaceID: f.workspace.ID, Name: "Atlas", Slug: "atlas"}
	if err := s.Agents().Create(ctx, f.agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return f
}

// seedNote inserts a live note with the given tier and owner shape.
func (f *graphFixture) seedNote(t *testing.T, visibility domain.MemoryVisibility, userID, agentID *string, content string) *domain.MemoryNote {
	t.Helper()
	note := &domain.MemoryNote{
		WorkspaceID:   f.workspace.ID,
		Visibility:    visibility,
		UserID:        userID,
		AgentID:       agentID,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		SourceEventID: "evt-note-1",
		Content:       content,
	}
	if err := f.store.MemoryNotes().InsertNote(context.Background(), note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	return note
}

// seedEvent inserts a live gist row owned by the fixture's agent.
func (f *graphFixture) seedEvent(t *testing.T, visibility domain.MemoryVisibility, userID *string, description string) *domain.MemoryEvent {
	t.Helper()
	event := &domain.MemoryEvent{
		WorkspaceID:   f.workspace.ID,
		AgentID:       f.agent.ID,
		SessionID:     "sess-1",
		TurnID:        "turn-1",
		Visibility:    visibility,
		UserID:        userID,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		SourceEventID: "evt-event-1",
		Description:   description,
	}
	if err := f.store.MemoryEvents().InsertEvent(context.Background(), event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return event
}

func (f *graphFixture) embedding(targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility, userID, agentID *string, vec ...float32) domain.MemoryEmbedding {
	return domain.MemoryEmbedding{
		WorkspaceID:   f.workspace.ID,
		TargetType:    targetType,
		TargetID:      targetID,
		Dimension:     len(vec),
		Embedding:     vec,
		Visibility:    visibility,
		UserID:        userID,
		AgentID:       agentID,
		SourceEventID: "evt-" + targetID,
		LearnedAt:     time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
	}
}

func TestFakeMemoryEmbeddings_BatchInsertSearchOrderingAndDimensionFilter(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	// Two vectors at the workspace's current dimension; the query is closest
	// to the second — the ordering contract the fusion channel relies on.
	first := f.embedding(domain.MemoryTargetRaw, "raw-1", domain.MemoryVisibilityShared, nil, nil, 1, 0, 0)
	second := f.embedding(domain.MemoryTargetRaw, "raw-2", domain.MemoryVisibilityShared, nil, nil, 0, 1, 0)
	// A stale-dimension row (the pre-change embedding model) coexisting.
	stale := f.embedding(domain.MemoryTargetRaw, "raw-3", domain.MemoryVisibilityShared, nil, nil, 1, 1)
	stale.Dimension = 2

	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{first, second, stale}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	results, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, f.agent.ID,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0.9, 0.1, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 hits at dimension 3 (stale dimension excluded), got %d", len(results))
	}
	if results[0].TargetID != "raw-1" || results[1].TargetID != "raw-2" {
		t.Fatalf("expected cosine ordering raw-1 then raw-2, got %s then %s", results[0].TargetID, results[1].TargetID)
	}
	// Rows carry their scope fields and citation pointer, never the vector.
	if results[0].SourceEventID != "evt-raw-1" {
		t.Errorf("expected source event id on the hit, got %q", results[0].SourceEventID)
	}
	if results[0].Embedding != nil {
		t.Error("search results must not carry the vector")
	}

	// The stale dimension stays retrievable when queried at its own
	// dimension (D5: exclusion, not deletion).
	staleHits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, f.agent.ID,
		nil, 2, []float32{1, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("SearchByVector at dimension 2: %v", err)
	}
	if len(staleHits) != 1 || staleHits[0].TargetID != "raw-3" {
		t.Fatalf("expected the stale-dimension row at its own dimension, got %+v", staleHits)
	}
}

func TestFakeMemoryEmbeddings_Validation(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	if _, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, "", "", nil, 0, nil, store.MemoryEmbeddingFilters{}, 5); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for non-positive dimension, got %v", err)
	}
	if _, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, "", "", nil, 3, []float32{1, 0}, store.MemoryEmbeddingFilters{}, 5); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for query/dimension length mismatch, got %v", err)
	}

	bad := f.embedding(domain.MemoryTargetRaw, "raw-1", domain.MemoryVisibilityUser, nil, nil, 1, 0, 0) // user tier without owner
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for owner-shape violation, got %v", err)
	}

	empty := f.embedding(domain.MemoryTargetRaw, "raw-1", domain.MemoryVisibilityShared, nil, nil)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{empty}); err == nil {
		t.Error("expected an error for an empty vector (length != dimension)")
	}

	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, nil); err != nil {
		t.Errorf("empty batch must be a no-op, got %v", err)
	}
}

func TestFakeMemoryEmbeddings_VisibilityFiltering(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	// Raw rows filter through their own snapshot; note/event rows through the
	// live linked row.
	rawUser := f.embedding(domain.MemoryTargetRaw, "raw-user", domain.MemoryVisibilityUser, &f.user.ID, nil, 1, 0, 0)
	rawShared := f.embedding(domain.MemoryTargetRaw, "raw-shared", domain.MemoryVisibilityShared, nil, nil, 0, 1, 0)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{rawUser, rawShared}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	agentNote := f.seedNote(t, domain.MemoryVisibilityAgent, nil, &f.agent.ID, "Atlas-only fact")
	noteEmbedding := f.embedding(domain.MemoryTargetNote, agentNote.ID, domain.MemoryVisibilityAgent, nil, &f.agent.ID, 0, 0, 1)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{noteEmbedding}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	// The owner sees their user-tier raw row nearest (distance 0); the other
	// visible rows share distance 1 (orthogonal), ordered deterministically.
	ownerHits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.user.ID, f.agent.ID, nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("owner search: %v", err)
	}
	if len(ownerHits) != 3 {
		t.Fatalf("expected all three visible rows, got %+v", ownerHits)
	}
	if ownerHits[0].TargetID != "raw-user" {
		t.Fatalf("expected the owner's raw row nearest, got %+v", ownerHits)
	}
	// A stranger with a non-matching serving agent sees only the shared row —
	// never the owner's user-tier raw row nor the agent's note embedding.
	strangerHits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, "other-agent", nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("stranger search: %v", err)
	}
	if len(strangerHits) != 1 || strangerHits[0].TargetID != "raw-shared" {
		t.Fatalf("expected the stranger to see only the shared row, got %+v", strangerHits)
	}

	// The serving agent reaches its own agent-tier note embedding nearest;
	// the viewer's identity alone does not unlock it.
	agentHits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, f.agent.ID, nil, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("agent search: %v", err)
	}
	if len(agentHits) == 0 || agentHits[0].TargetID != agentNote.ID {
		t.Fatalf("expected the serving agent to reach its note embedding first, got %+v", agentHits)
	}
	viewerOnlyHits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.user.ID, "other-agent", nil, 3, []float32{0, 0, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("viewer-only search: %v", err)
	}
	for _, hit := range viewerOnlyHits {
		if hit.TargetID == agentNote.ID {
			t.Fatal("the note embedding must require the serving agent's identity")
		}
	}

	// The visibility filter narrows within the visible set.
	sharedOnly, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.user.ID, f.agent.ID,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0, 1, 0}, store.MemoryEmbeddingFilters{Visibility: domain.MemoryVisibilityShared}, 0)
	if err != nil {
		t.Fatalf("shared-only search: %v", err)
	}
	if len(sharedOnly) != 1 || sharedOnly[0].TargetID != "raw-shared" {
		t.Fatalf("expected only the shared raw row, got %+v", sharedOnly)
	}
}

func TestFakeMemoryEmbeddings_UpsertDeleteAndDeadTargets(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	note := f.seedNote(t, domain.MemoryVisibilityShared, nil, nil, "the deploy window moved")
	embedding := f.embedding(domain.MemoryTargetNote, note.ID, domain.MemoryVisibilityShared, nil, nil, 1, 0, 0)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{embedding}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	// Re-embed at the same dimension replaces the vector instead of
	// duplicating the row (the SQL upsert).
	revised := f.embedding(domain.MemoryTargetNote, note.ID, domain.MemoryVisibilityShared, nil, nil, 0, 1, 0)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{revised}); err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	hits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, "", nil, 3, []float32{0, 1, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search after re-embed: %v", err)
	}
	if len(hits) != 1 || hits[0].TargetID != note.ID {
		t.Fatalf("expected one re-embedded row, got %+v", hits)
	}

	// Tombstoning the note kills the embedding at read time, before any
	// pipeline cleanup runs (read-time tombstone modeling).
	if err := f.store.MemoryNotes().TombstoneNote(ctx, f.workspace.ID, note.ID); err != nil {
		t.Fatalf("TombstoneNote: %v", err)
	}
	hits, err = f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, "", nil, 3, []float32{0, 1, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search after tombstone: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected the dead note's embedding to be excluded, got %+v", hits)
	}

	// The cleanup primitive drops the rows outright.
	removed, err := f.store.MemoryEmbeddings().DeleteByTarget(ctx, f.workspace.ID, domain.MemoryTargetNote, note.ID)
	if err != nil {
		t.Fatalf("DeleteByTarget: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed embedding, got %d", removed)
	}
	if _, err := f.store.MemoryEmbeddings().DeleteByTarget(ctx, f.other.ID, domain.MemoryTargetNote, note.ID); err != nil {
		t.Errorf("delete scoped to a foreign workspace removes nothing but must not error, got %v", err)
	}
}

func TestFakeMemoryEmbeddings_PromotionWidensThroughTheRow(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	note := f.seedNote(t, domain.MemoryVisibilityUser, &f.user.ID, nil, "private fact about billing")
	embedding := f.embedding(domain.MemoryTargetNote, note.ID, domain.MemoryVisibilityUser, &f.user.ID, nil, 1, 0, 0)
	if err := f.store.MemoryEmbeddings().InsertEmbeddings(ctx, []domain.MemoryEmbedding{embedding}); err != nil {
		t.Fatalf("InsertEmbeddings: %v", err)
	}

	// Before promotion the stranger cannot reach the note's embedding.
	hits, err := f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, "", nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("pre-promotion search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected the private embedding to be hidden, got %+v", hits)
	}

	// The human promotion path widens the ROW; the embedding read follows the
	// live row, so the stranger now reaches it through the same door.
	if err := f.store.MemoryNotes().PromoteNote(ctx, f.workspace.ID, note.ID, f.user.ID); err != nil {
		t.Fatalf("PromoteNote: %v", err)
	}
	hits, err = f.store.MemoryEmbeddings().SearchByVector(ctx, f.workspace.ID, f.stranger.ID, "", nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("post-promotion search: %v", err)
	}
	if len(hits) != 1 || hits[0].TargetID != note.ID {
		t.Fatalf("expected promotion to widen reach through the row, got %+v", hits)
	}
}

func TestFakeMemoryEntities_ResolveIsIdempotentPerWorkspace(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	proposed := &domain.MemoryEntity{
		WorkspaceID:     f.workspace.ID,
		Label:           "Sari",
		NormalizedLabel: domain.NormalizeEntityLabel("Sari"),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt-1",
		LearnedAt:       time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
	}
	if err := f.store.MemoryEntities().ResolveEntity(ctx, proposed); err != nil {
		t.Fatalf("ResolveEntity: %v", err)
	}

	// A different spelling of the same normalized label resolves to the SAME
	// row and keeps the first label's spelling (birth immutability).
	again := &domain.MemoryEntity{
		WorkspaceID:     f.workspace.ID,
		Label:           "  SARIS ",
		NormalizedLabel: domain.NormalizeEntityLabel("SARIS"),
		Origin:          domain.MemoryOriginInfer,
		SourceEventID:   "evt-2",
		LearnedAt:       time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
	}
	if err := f.store.MemoryEntities().ResolveEntity(ctx, again); err != nil {
		t.Fatalf("second ResolveEntity: %v", err)
	}
	if again.ID != proposed.ID {
		t.Fatalf("expected the same entity row, got %s vs %s", again.ID, proposed.ID)
	}
	if again.Label != "Sari" {
		t.Errorf("expected the first label's spelling to win, got %q", again.Label)
	}
	if !again.CreatedAt.Equal(proposed.CreatedAt) {
		t.Errorf("expected the birth created_at to be kept, got %v vs %v", again.CreatedAt, proposed.CreatedAt)
	}

	// The same label in another workspace is a distinct row (tenant boundary).
	otherEntity := &domain.MemoryEntity{
		WorkspaceID:     f.other.ID,
		Label:           "Sari",
		NormalizedLabel: domain.NormalizeEntityLabel("Sari"),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt-1",
		LearnedAt:       proposed.LearnedAt,
	}
	if err := f.store.MemoryEntities().ResolveEntity(ctx, otherEntity); err != nil {
		t.Fatalf("ResolveEntity in other workspace: %v", err)
	}
	if otherEntity.ID == proposed.ID {
		t.Fatal("entities must never be shared across workspaces")
	}

	// Parity enforcement: a normalized label that disagrees with the label is
	// rejected (normalization drift is a bug, not data).
	drifted := &domain.MemoryEntity{
		WorkspaceID:     f.workspace.ID,
		Label:           "Beacon",
		NormalizedLabel: "beacons",
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt-3",
		LearnedAt:       proposed.LearnedAt,
	}
	if err := f.store.MemoryEntities().ResolveEntity(ctx, drifted); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for normalization drift, got %v", err)
	}

	// Exact lookup round-trips; unknown labels are (nil, nil), not errors.
	byLabel, err := f.store.MemoryEntities().GetEntityByLabel(ctx, f.workspace.ID, "sari")
	if err != nil || byLabel == nil || byLabel.ID != proposed.ID {
		t.Fatalf("GetEntityByLabel: %+v, err %v", byLabel, err)
	}
	missing, err := f.store.MemoryEntities().GetEntityByLabel(ctx, f.workspace.ID, "nonexistent")
	if err != nil || missing != nil {
		t.Fatalf("expected (nil, nil) for an unknown label, got (%+v, %v)", missing, err)
	}
}

func TestFakeMemoryEntities_FindByLabelPrefix(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	learned := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	labels := []string{"Project Atlas", "project atlas docs", "Atlas", "Unrelated"}
	ids := make(map[string]string)
	for i, label := range labels {
		e := &domain.MemoryEntity{
			WorkspaceID:     f.workspace.ID,
			Label:           label,
			NormalizedLabel: domain.NormalizeEntityLabel(label),
			Origin:          domain.MemoryOriginDialogue,
			SourceEventID:   "evt-1",
			LearnedAt:       learned.Add(time.Duration(i) * time.Minute),
		}
		if err := f.store.MemoryEntities().ResolveEntity(ctx, e); err != nil {
			t.Fatalf("ResolveEntity(%q): %v", label, err)
		}
		ids[label] = e.ID
	}

	// Exact normalized match ranks first even when prefix and similarity legs
	// also fire.
	found, err := f.store.MemoryEntities().FindEntitiesByLabelPrefix(ctx, f.workspace.ID, "project atlas", 0)
	if err != nil {
		t.Fatalf("FindEntitiesByLabelPrefix: %v", err)
	}
	if len(found) == 0 || found[0].ID != ids["Project Atlas"] {
		t.Fatalf("expected the exact match first, got %+v", found)
	}

	// A short seed falls back to prefix then similarity; unrelated labels
	// stay out.
	found, err = f.store.MemoryEntities().FindEntitiesByLabelPrefix(ctx, f.workspace.ID, "atlas", 10)
	if err != nil {
		t.Fatalf("FindEntitiesByLabelPrefix: %v", err)
	}
	if len(found) != 3 {
		t.Fatalf("expected the 3 atlas-ish labels, got %+v", found)
	}
	for _, e := range found {
		if e.ID == ids["Unrelated"] {
			t.Fatal("the unrelated label must not match")
		}
	}

	// Tenant isolation: the other workspace holds no such labels.
	found, err = f.store.MemoryEntities().FindEntitiesByLabelPrefix(ctx, f.other.ID, "atlas", 10)
	if err != nil {
		t.Fatalf("FindEntitiesByLabelPrefix in other workspace: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected no cross-tenant labels, got %+v", found)
	}
}

func TestFakeMemoryEntities_EdgeInsertVisibilityAndIdempotency(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	entity := &domain.MemoryEntity{
		WorkspaceID:     f.workspace.ID,
		Label:           "Project Atlas",
		NormalizedLabel: domain.NormalizeEntityLabel("Project Atlas"),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "evt-1",
		LearnedAt:       time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
	}
	if err := f.store.MemoryEntities().ResolveEntity(ctx, entity); err != nil {
		t.Fatalf("ResolveEntity: %v", err)
	}

	userNote := f.seedNote(t, domain.MemoryVisibilityUser, &f.user.ID, nil, "atlas private note")
	sharedEvent := f.seedEvent(t, domain.MemoryVisibilityShared, nil, "atlas shipped")

	edge := func(targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility) domain.MemoryEntityEdge {
		return domain.MemoryEntityEdge{
			EntityID:      entity.ID,
			TargetType:    targetType,
			TargetID:      targetID,
			Visibility:    visibility,
			Origin:        domain.MemoryOriginDialogue,
			SourceEventID: "evt-edge",
		}
	}
	batch := []domain.MemoryEntityEdge{
		edge(domain.MemoryTargetNote, userNote.ID, domain.MemoryVisibilityUser),   // narrowest endpoint
		edge(domain.MemoryTargetEvent, sharedEvent.ID, domain.MemoryVisibilityShared),
	}
	inserted, err := f.store.MemoryEntities().AddEdges(ctx, f.workspace.ID, batch)
	if err != nil {
		t.Fatalf("AddEdges: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 edges inserted, got %d", inserted)
	}

	// Re-mention links nothing twice; foreign-entity rows drop silently.
	replay := append([]domain.MemoryEntityEdge{edge(domain.MemoryTargetEvent, sharedEvent.ID, domain.MemoryVisibilityShared)}, edge(domain.MemoryTargetNote, "missing-note", domain.MemoryVisibilityShared))
	replay[1].EntityID = "not-an-entity"
	inserted, err = f.store.MemoryEntities().AddEdges(ctx, f.workspace.ID, replay)
	if err != nil {
		t.Fatalf("replayed AddEdges: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("expected the replay to insert nothing, got %d", inserted)
	}

	// Raw targets are not graph endpoints.
	if _, err := f.store.MemoryEntities().AddEdges(ctx, f.workspace.ID, []domain.MemoryEntityEdge{edge(domain.MemoryTargetRaw, "raw-1", domain.MemoryVisibilityShared)}); err == nil {
		t.Fatal("expected raw-target edges to be rejected")
	}

	// Edge reads gate through the live linked row: the owner sees both edges,
	// a stranger sees only the shared one, and the serving agent does not
	// reach the user-tier note edge.
	ownerEdges, err := f.store.MemoryEntities().ListEdgesForEntities(ctx, f.workspace.ID, f.user.ID, f.agent.ID, []string{entity.ID})
	if err != nil {
		t.Fatalf("owner ListEdgesForEntities: %v", err)
	}
	if len(ownerEdges) != 2 {
		t.Fatalf("expected the owner to see both edges, got %+v", ownerEdges)
	}
	strangerEdges, err := f.store.MemoryEntities().ListEdgesForEntities(ctx, f.workspace.ID, f.stranger.ID, "", []string{entity.ID})
	if err != nil {
		t.Fatalf("stranger ListEdgesForEntities: %v", err)
	}
	if len(strangerEdges) != 1 || strangerEdges[0].TargetID != sharedEvent.ID {
		t.Fatalf("expected the stranger to see only the shared edge, got %+v", strangerEdges)
	}

	// Tombstoning the linked note kills its edge at read time.
	if err := f.store.MemoryNotes().TombstoneNote(ctx, f.workspace.ID, userNote.ID); err != nil {
		t.Fatalf("TombstoneNote: %v", err)
	}
	ownerEdges, err = f.store.MemoryEntities().ListEdgesForEntities(ctx, f.workspace.ID, f.user.ID, f.agent.ID, []string{entity.ID})
	if err != nil {
		t.Fatalf("post-tombstone ListEdgesForEntities: %v", err)
	}
	if len(ownerEdges) != 1 || ownerEdges[0].TargetID != sharedEvent.ID {
		t.Fatalf("expected the dead note's edge to drop out, got %+v", ownerEdges)
	}

	// Empty id lists are an empty slice, not an error.
	empty, err := f.store.MemoryEntities().ListEdgesForEntities(ctx, f.workspace.ID, f.user.ID, "", nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected an empty slice for empty ids, got (%+v, %v)", empty, err)
	}
}

func TestFakeMemoryEntities_FoldPreservesEdgesAndProvenance(t *testing.T) {
	ctx := context.Background()
	f := newGraphFixture(t)

	newEntity := func(label string) *domain.MemoryEntity {
		e := &domain.MemoryEntity{
			WorkspaceID:     f.workspace.ID,
			Label:           label,
			NormalizedLabel: domain.NormalizeEntityLabel(label),
			Origin:          domain.MemoryOriginDialogue,
			SourceEventID:   "evt-1",
			LearnedAt:       time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
		}
		if err := f.store.MemoryEntities().ResolveEntity(ctx, e); err != nil {
			t.Fatalf("ResolveEntity(%q): %v", label, err)
		}
		return e
	}
	survivor := newEntity("Acme Corp")
	duplicate := newEntity("Acme Corp.") // near-duplicate label, distinct row

	sharedNote := f.seedNote(t, domain.MemoryVisibilityShared, nil, nil, "corporate fact")
	dupOnlyEvent := f.seedEvent(t, domain.MemoryVisibilityShared, nil, "duplicate-only fact")

	makeEdge := func(entityID string, targetType domain.MemoryTargetType, targetID string) domain.MemoryEntityEdge {
		return domain.MemoryEntityEdge{
			EntityID:      entityID,
			TargetType:    targetType,
			TargetID:      targetID,
			Visibility:    domain.MemoryVisibilityShared,
			Origin:        domain.MemoryOriginInfer,
			SourceEventID: "evt-edge-" + targetID,
		}
	}
	if _, err := f.store.MemoryEntities().AddEdges(ctx, f.workspace.ID, []domain.MemoryEntityEdge{
		makeEdge(survivor.ID, domain.MemoryTargetNote, sharedNote.ID),
		makeEdge(duplicate.ID, domain.MemoryTargetNote, sharedNote.ID), // collision: survivor already holds it
		makeEdge(duplicate.ID, domain.MemoryTargetEvent, dupOnlyEvent.ID),
	}); err != nil {
		t.Fatalf("AddEdges: %v", err)
	}

	moved, err := f.store.MemoryEntities().FoldEntity(ctx, f.workspace.ID, duplicate.ID, survivor.ID)
	if err != nil {
		t.Fatalf("FoldEntity: %v", err)
	}
	if moved != 1 {
		t.Fatalf("expected 1 edge re-pointed (the colliding one stays), got %d", moved)
	}

	// The re-pointed edge keeps its provenance stamp verbatim.
	survivorEdges, err := f.store.MemoryEntities().ListEdgesForEntities(ctx, f.workspace.ID, f.user.ID, f.agent.ID, []string{survivor.ID})
	if err != nil {
		t.Fatalf("ListEdgesForEntities after fold: %v", err)
	}
	if len(survivorEdges) != 2 {
		t.Fatalf("expected the survivor to hold both edges, got %+v", survivorEdges)
	}
	var movedEdge *domain.MemoryEntityEdge
	for i := range survivorEdges {
		if survivorEdges[i].TargetID == dupOnlyEvent.ID {
			movedEdge = &survivorEdges[i]
		}
	}
	if movedEdge == nil {
		t.Fatal("expected the duplicate-only edge on the survivor")
	}
	if movedEdge.Origin != domain.MemoryOriginInfer || movedEdge.SourceEventID != "evt-edge-"+dupOnlyEvent.ID || movedEdge.Visibility != domain.MemoryVisibilityShared {
		t.Errorf("fold must preserve provenance, got %+v", movedEdge)
	}

	// Guard rails: self-fold is invalid, unknown ids are not found.
	if _, err := f.store.MemoryEntities().FoldEntity(ctx, f.workspace.ID, survivor.ID, survivor.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for a self-fold, got %v", err)
	}
	if _, err := f.store.MemoryEntities().FoldEntity(ctx, f.workspace.ID, "missing", survivor.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for an unknown duplicate, got %v", err)
	}
	if _, err := f.store.MemoryEntities().FoldEntity(ctx, f.other.ID, duplicate.ID, survivor.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound across the tenant boundary, got %v", err)
	}

	// Re-folding a folded duplicate moves nothing but stays valid (zero
	// edges left).
	moved, err = f.store.MemoryEntities().FoldEntity(ctx, f.workspace.ID, duplicate.ID, survivor.ID)
	if err != nil {
		t.Fatalf("re-fold: %v", err)
	}
	if moved != 0 {
		t.Fatalf("expected the re-fold to move nothing, got %d", moved)
	}
}
