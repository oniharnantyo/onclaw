package memory

// The nightly entity hygiene (wave3 tasks 6.1–6.2, D10): the consolidator
// folds entities whose stored normalized labels match (copy-out — edges
// re-point, provenance preserved, nothing deleted) and counts the merges in
// the morning report, next to the embedding-failure delta.

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// foldStubEntities simulates the duplicate-label anomaly the fold defends
// against: the stores' ResolveEntity is idempotent (duplicates cannot arise
// through it), so this stub seeds two rows with equal stored normalized
// labels directly and records what the consolidator folds.
type foldStubEntities struct {
	mu       sync.Mutex
	entities []domain.MemoryEntity
	edges    map[string][]domain.MemoryEntityEdge // entityID -> its edges
	folds    []string                             // "duplicate->survivor" records
}

func (f *foldStubEntities) ResolveEntity(_ context.Context, _ *domain.MemoryEntity) error { return nil }

func (f *foldStubEntities) GetEntityByLabel(_ context.Context, workspaceID, normalizedLabel string) (*domain.MemoryEntity, error) {
	for i := range f.entities {
		if f.entities[i].WorkspaceID == workspaceID && f.entities[i].NormalizedLabel == normalizedLabel {
			e := f.entities[i]
			return &e, nil
		}
	}
	return nil, nil
}

func (f *foldStubEntities) FindEntitiesByLabelPrefix(_ context.Context, _, _ string, _ int) ([]domain.MemoryEntity, error) {
	return []domain.MemoryEntity{}, nil
}

func (f *foldStubEntities) ListEntities(_ context.Context, workspaceID string, limit int) ([]domain.MemoryEntity, error) {
	out := make([]domain.MemoryEntity, 0, len(f.entities))
	for _, e := range f.entities {
		if e.WorkspaceID == workspaceID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NormalizedLabel != out[j].NormalizedLabel {
			return out[i].NormalizedLabel < out[j].NormalizedLabel
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *foldStubEntities) AddEdges(_ context.Context, _ string, edges []domain.MemoryEntityEdge) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.edges == nil {
		f.edges = map[string][]domain.MemoryEntityEdge{}
	}
	for _, e := range edges {
		f.edges[e.EntityID] = append(f.edges[e.EntityID], e)
	}
	return int64(len(edges)), nil
}

func (f *foldStubEntities) ListEdgesForEntities(_ context.Context, _ string, _, _ string, entityIDs []string) ([]domain.MemoryEntityEdge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]struct{}{}
	for _, id := range entityIDs {
		want[id] = struct{}{}
	}
	out := []domain.MemoryEntityEdge{}
	for id, edges := range f.edges {
		if _, ok := want[id]; !ok {
			continue
		}
		out = append(out, edges...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// FoldEntity re-points the duplicate's edges onto the survivor, copy-out:
// an edge the survivor already holds (same target) stays on the duplicate,
// and every moved edge keeps its stamps.
func (f *foldStubEntities) FoldEntity(_ context.Context, workspaceID, duplicateID, intoID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var dup, survivor *domain.MemoryEntity
	for i := range f.entities {
		if f.entities[i].ID == duplicateID && f.entities[i].WorkspaceID == workspaceID {
			dup = &f.entities[i]
		}
		if f.entities[i].ID == intoID && f.entities[i].WorkspaceID == workspaceID {
			survivor = &f.entities[i]
		}
	}
	if dup == nil || survivor == nil {
		return 0, domain.ErrNotFound
	}
	held := map[string]struct{}{}
	for _, e := range f.edges[intoID] {
		held[string(e.TargetType)+":"+e.TargetID] = struct{}{}
	}
	var moved int64
	kept := f.edges[duplicateID][:0]
	for _, e := range f.edges[duplicateID] {
		if _, exists := held[string(e.TargetType)+":"+e.TargetID]; exists {
			kept = append(kept, e) // never duplicated, never deleted
			continue
		}
		e.EntityID = intoID
		f.edges[intoID] = append(f.edges[intoID], e)
		moved++
	}
	f.edges[duplicateID] = kept
	f.folds = append(f.folds, duplicateID+"->"+intoID)
	return moved, nil
}

// seedDuplicateEntities wires the stub with two "acme" rows: the oldest is
// the deterministic survivor; the duplicate holds one movable edge and one
// edge the survivor already holds.
func seedDuplicateEntities() *foldStubEntities {
	now := time.Now().UTC()
	older := domain.MemoryEntity{
		ID: "ent-older", WorkspaceID: testWorkspaceID, Label: "Acme", NormalizedLabel: "acme",
		Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-1", LearnedAt: now.Add(-2 * time.Hour),
	}
	newer := domain.MemoryEntity{
		ID: "ent-newer", WorkspaceID: testWorkspaceID, Label: "ACME Corp", NormalizedLabel: "acme",
		Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-2", LearnedAt: now.Add(-1 * time.Hour),
	}
	stub := &foldStubEntities{entities: []domain.MemoryEntity{older, newer}, edges: map[string][]domain.MemoryEntityEdge{}}
	stub.edges["ent-older"] = []domain.MemoryEntityEdge{{
		ID: "edge-1", EntityID: "ent-older", TargetType: domain.MemoryTargetNote, TargetID: "note-x",
		Visibility: domain.MemoryVisibilityUser, Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-1",
	}}
	stub.edges["ent-newer"] = []domain.MemoryEntityEdge{
		{ID: "edge-2", EntityID: "ent-newer", TargetType: domain.MemoryTargetNote, TargetID: "note-x",
			Visibility: domain.MemoryVisibilityShared, Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-2"},
		{ID: "edge-3", EntityID: "ent-newer", TargetType: domain.MemoryTargetEvent, TargetID: "event-y",
			Visibility: domain.MemoryVisibilityShared, Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-2"},
	}
	return stub
}

// TestConsolidatorFoldsDuplicateEntities (spec scenario "duplicates fold
// overnight"): edges re-point onto the surviving row, evidence stamps stay
// on the moved edges, nothing is deleted, and the report counts the merge.
func TestConsolidatorFoldsDuplicateEntities(t *testing.T) {
	s := seedWorld(t)
	stub := seedDuplicateEntities()

	c := NewConsolidator(
		s.MemoryNotes(), s.MemoryReports(), s.Workspaces(), stub, s.Providers(), nil, nil,
		func() IngestStats { return IngestStats{} }, testLogger,
	)
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if report.EntityMerges != 1 {
		t.Fatalf("the morning report must count the merge, got %+v", report)
	}
	if len(stub.folds) != 1 || stub.folds[0] != "ent-newer->ent-older" {
		t.Fatalf("the oldest row must survive the fold, got %v", stub.folds)
	}

	// The moved edge lands on the survivor with its provenance intact.
	moved := stub.edges["ent-older"]
	if len(moved) != 2 {
		t.Fatalf("the survivor must hold both of its edges, got %+v", moved)
	}
	var foundEventEdge *domain.MemoryEntityEdge
	for i := range moved {
		if moved[i].ID == "edge-3" {
			foundEventEdge = &moved[i]
		}
	}
	if foundEventEdge == nil {
		t.Fatalf("the movable edge must re-point onto the survivor, got %+v", moved)
	}
	if foundEventEdge.EntityID != "ent-older" || foundEventEdge.Visibility != domain.MemoryVisibilityShared ||
		foundEventEdge.Origin != domain.MemoryOriginDialogue || foundEventEdge.SourceEventID != "evt-2" {
		t.Fatalf("a fold must preserve provenance, got %+v", foundEventEdge)
	}

	// The colliding edge stays on the duplicate — never duplicated, never
	// deleted (copy-out).
	kept := stub.edges["ent-newer"]
	if len(kept) != 1 || kept[0].ID != "edge-2" {
		t.Fatalf("the survivor-held edge must stay on the duplicate, got %+v", kept)
	}

	// The count rides the persisted report the Memory pane reads.
	saved := savedReport(t, s)
	if saved == nil {
		t.Fatal("expected a persisted report")
	}
	var wire MorningReport
	if err := json.Unmarshal(saved.Report, &wire); err != nil {
		t.Fatalf("decode saved report: %v", err)
	}
	if wire.EntityMerges != 1 {
		t.Fatalf("the persisted report must carry the entity merge count, got %+v", wire)
	}
}

// TestConsolidatorNoFalseEntityMerges: distinct normalized labels fold
// nothing — idempotent resolution means the real store never produces
// duplicates, and the pass must not invent merges.
func TestConsolidatorNoFalseEntityMerges(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, label := range []string{"Acme", "Beacon"} {
		entity := &domain.MemoryEntity{
			WorkspaceID: testWorkspaceID, Label: label, NormalizedLabel: domain.NormalizeEntityLabel(label),
			Origin: domain.MemoryOriginDialogue, SourceEventID: "evt-1", LearnedAt: now,
		}
		if err := s.MemoryEntities().ResolveEntity(ctx, entity); err != nil {
			t.Fatalf("resolve %s: %v", label, err)
		}
	}

	c := NewConsolidator(
		s.MemoryNotes(), s.MemoryReports(), s.Workspaces(), s.MemoryEntities(), s.Providers(), nil, nil,
		func() IngestStats { return IngestStats{} }, testLogger,
	)
	report, err := c.RunNow(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if report.EntityMerges != 0 {
		t.Fatalf("distinct labels must never merge, got %+v", report)
	}
}

// TestConsolidatorReportsEmbeddingFailures (wave3 D3 visibility): the
// morning report carries the embedding-failure delta between passes.
func TestConsolidatorReportsEmbeddingFailures(t *testing.T) {
	s := seedWorld(t)
	current := IngestStats{EmbedFailures: 3}
	c := NewConsolidator(
		s.MemoryNotes(), s.MemoryReports(), s.Workspaces(), s.MemoryEntities(), s.Providers(), nil, nil,
		func() IngestStats { return current }, testLogger,
	)
	ctx := context.Background()

	first, err := c.RunNow(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("first RunNow: %v", err)
	}
	// The first pass reports everything since process start (the counters
	// are monotonic and the workspace has no last-seen snapshot yet).
	if first.EmbeddingFailures != 3 {
		t.Fatalf("the first pass reports the full counter, got %+v", first)
	}

	current = IngestStats{EmbedFailures: 5}
	second, err := c.RunNow(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("second RunNow: %v", err)
	}
	if second.EmbeddingFailures != 2 {
		t.Fatalf("expected the delta 2, got %+v", second)
	}
	// The extraction-failure field of the SAME pass reads the SAME snapshot
	// — a per-field snapshot would have eaten the delta.
	if second.ExtractionFailures != 0 {
		t.Fatalf("extraction failures untouched, got %+v", second)
	}
}
