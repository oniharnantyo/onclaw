package fake

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// MemoryEmbeddingStore / MemoryEntityStore implementation
// (wave3-memory-vectors-and-graph D1/D5/D7/D8/D10). Mirrors the postgres
// adapters: every read applies the structural visibility predicate
// (domain.MemoryVisibleTo — the same rule the postgres WHERE clause encodes)
// under the store mutex, with note/event embeddings and edges taking scope
// from the LIVE linked row so promotion widens through the row, never
// through a snapshot. Writes validate through the domain layer; the upsert,
// idempotent edge link, and fold-with-conflict-skip semantics match the SQL
// unique indexes one for one.
// -------------------------------------------------------------------------

type memoryEmbeddingStore struct {
	s *fakeStore
}

type memoryEntityStore struct {
	s *fakeStore
}

// cloneStringPtr deep-copies an optional string column.
func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneMemoryEmbedding(e *domain.MemoryEmbedding) *domain.MemoryEmbedding {
	if e == nil {
		return nil
	}
	cp := *e
	if e.Embedding != nil {
		cp.Embedding = make([]float32, len(e.Embedding))
		copy(cp.Embedding, e.Embedding)
	}
	cp.UserID = cloneStringPtr(e.UserID)
	cp.AgentID = cloneStringPtr(e.AgentID)
	return &cp
}

func cloneMemoryEntity(e *domain.MemoryEntity) *domain.MemoryEntity {
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

func cloneMemoryEntityEdge(e *domain.MemoryEntityEdge) *domain.MemoryEntityEdge {
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

// checkEmbeddingRefsLocked mirrors the postgres FKs: the workspace must
// exist and any set owner must resolve. Callers hold the store lock.
func (s *fakeStore) checkEmbeddingRefsLocked(e *domain.MemoryEmbedding) error {
	if _, exists := s.workspaces[e.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if e.UserID != nil {
		if _, exists := s.users[*e.UserID]; !exists {
			return fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
	}
	if e.AgentID != nil {
		if _, exists := s.agents[*e.AgentID]; !exists {
			return fmt.Errorf("%w: agent not found", domain.ErrNotFound)
		}
	}
	return nil
}

// embeddingUpsertKey is the natural identity the SQL unique index pins:
// one embedding per (workspace, target, dimension).
func embeddingUpsertKey(e *domain.MemoryEmbedding) string {
	return strings.Join([]string{e.WorkspaceID, string(e.TargetType), e.TargetID, fmt.Sprint(e.Dimension)}, ":")
}

// cosineDistance is the fake's stand-in for halfvec's '<=>' operator.
// A zero-norm side yields the maximum distance 1 (nothing in common).
func cosineDistance(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 1
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb))
}

// liveLinkedMemoryLocked resolves the live linked row for a note/event
// pointer: nil when the row is missing, foreign to the workspace, or
// tombstoned (dead target — read-time tombstone modeling). Callers hold the
// store lock.
func (s *fakeStore) liveLinkedMemoryLocked(targetType domain.MemoryTargetType, workspaceID, targetID string) (visibility domain.MemoryVisibility, userID, agentID *string, live bool) {
	switch targetType {
	case domain.MemoryTargetNote:
		n, exists := s.memoryNotes[targetID]
		if !exists || n.WorkspaceID != workspaceID || n.TombstonedAt != nil {
			return "", nil, nil, false
		}
		return n.Visibility, n.UserID, n.AgentID, true
	case domain.MemoryTargetEvent:
		e, exists := s.memoryEvents[targetID]
		if !exists || e.WorkspaceID != workspaceID || e.TombstonedAt != nil {
			return "", nil, nil, false
		}
		return e.Visibility, e.UserID, &e.AgentID, true
	default: // raw rows carry their own snapshot
		return "", nil, nil, true
	}
}

func (es *memoryEmbeddingStore) InsertEmbeddings(ctx context.Context, embeddings []domain.MemoryEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}
	for i := range embeddings {
		if err := domain.ValidateMemoryEmbedding(&embeddings[i]); err != nil {
			return fmt.Errorf("embedding %d: %w", i, err)
		}
		if embeddings[i].ID == "" {
			embeddings[i].ID = uuid.NewString()
		}
	}

	es.s.mu.Lock()
	defer es.s.mu.Unlock()

	for i := range embeddings {
		e := &embeddings[i]
		if err := es.s.checkEmbeddingRefsLocked(e); err != nil {
			return err
		}
		if stored, exists := es.s.memoryEmbeddingsByKey(embeddingUpsertKey(e)); exists {
			// Re-embed at the same dimension: refresh vector and scope, keep
			// birth identity and created_at (the SQL ON CONFLICT DO UPDATE).
			stored.Embedding = append([]float32(nil), e.Embedding...)
			stored.Visibility = e.Visibility
			stored.UserID = cloneStringPtr(e.UserID)
			stored.AgentID = cloneStringPtr(e.AgentID)
			stored.SourceEventID = e.SourceEventID
			stored.LearnedAt = e.LearnedAt
			continue
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now().UTC()
		}
		es.s.memoryEmbeddings[e.ID] = cloneMemoryEmbedding(e)
	}
	return nil
}

func (es *memoryEmbeddingStore) DeleteByTarget(ctx context.Context, workspaceID string, targetType domain.MemoryTargetType, targetID string) (int64, error) {
	if workspaceID == "" || targetID == "" {
		return 0, domain.ErrInvalid
	}
	if !domain.ValidMemoryTargetType(targetType) {
		return 0, fmt.Errorf("%w: unknown memory target type %q", domain.ErrInvalid, targetType)
	}

	es.s.mu.Lock()
	defer es.s.mu.Unlock()

	var removed int64
	for id, e := range es.s.memoryEmbeddings {
		if e.WorkspaceID == workspaceID && e.TargetType == targetType && e.TargetID == targetID {
			delete(es.s.memoryEmbeddings, id)
			removed++
		}
	}
	return removed, nil
}

func (es *memoryEmbeddingStore) SearchByVector(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, targetTypes []domain.MemoryTargetType, dimension int, query []float32, filters store.MemoryEmbeddingFilters, limit int) ([]domain.MemoryEmbedding, error) {
	if workspaceID == "" {
		return []domain.MemoryEmbedding{}, nil
	}
	if dimension <= 0 {
		return nil, fmt.Errorf("%w: memory embedding dimension must be positive", domain.ErrInvalid)
	}
	if len(query) != dimension {
		return nil, fmt.Errorf("%w: query vector length %d does not match dimension %d", domain.ErrInvalid, len(query), dimension)
	}
	if filters.Visibility != "" && !domain.ValidMemoryVisibility(filters.Visibility) {
		return nil, fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, filters.Visibility)
	}
	kindSet := make(map[domain.MemoryTargetType]struct{}, len(targetTypes))
	for _, t := range targetTypes {
		kindSet[t] = struct{}{}
	}

	es.s.mu.RLock()
	defer es.s.mu.RUnlock()

	type hit struct {
		embedding domain.MemoryEmbedding
		distance  float64
	}
	hits := make([]hit, 0)
	for _, e := range es.s.memoryEmbeddings {
		if e.WorkspaceID != workspaceID || e.Dimension != dimension {
			continue
		}
		if len(kindSet) > 0 {
			if _, ok := kindSet[e.TargetType]; !ok {
				continue
			}
		}
		if filters.Visibility != "" && e.Visibility != filters.Visibility {
			continue
		}
		// Structural scope: raw rows through their own snapshot, note/event
		// rows through the live linked row (dead targets are excluded).
		scopeVisibility, scopeUser, scopeAgent := e.Visibility, e.UserID, e.AgentID
		if e.TargetType != domain.MemoryTargetRaw {
			linkedVisibility, linkedUser, linkedAgent, live := es.s.liveLinkedMemoryLocked(e.TargetType, e.WorkspaceID, e.TargetID)
			if !live {
				continue
			}
			scopeVisibility, scopeUser, scopeAgent = linkedVisibility, linkedUser, linkedAgent
		}
		if !domain.MemoryVisibleTo(scopeVisibility, scopeUser, scopeAgent, viewerUserID, servingAgentID) {
			continue
		}
		result := cloneMemoryEmbedding(e)
		// Parity with the postgres read: results carry scope fields and the
		// target pointer, never the vector.
		result.Embedding = nil
		hits = append(hits, hit{embedding: *result, distance: cosineDistance(query, e.Embedding)})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].distance != hits[j].distance {
			return hits[i].distance < hits[j].distance
		}
		if hits[i].embedding.TargetType != hits[j].embedding.TargetType {
			return hits[i].embedding.TargetType < hits[j].embedding.TargetType
		}
		return hits[i].embedding.TargetID < hits[j].embedding.TargetID
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	results := make([]domain.MemoryEmbedding, 0, len(hits))
	for _, h := range hits {
		results = append(results, h.embedding)
	}
	return results, nil
}

func (ns *memoryEntityStore) ResolveEntity(ctx context.Context, entity *domain.MemoryEntity) error {
	if err := domain.ValidateMemoryEntity(entity); err != nil {
		return err
	}
	if entity.ID == "" {
		entity.ID = uuid.NewString()
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	if _, exists := ns.s.workspaces[entity.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if stored, exists := ns.s.entityByNormalizedLocked(entity.WorkspaceID, entity.NormalizedLabel); exists {
		// First label's spelling wins at birth; the stored row is returned.
		*entity = *cloneMemoryEntity(stored)
		return nil
	}
	if entity.CreatedAt.IsZero() {
		entity.CreatedAt = time.Now().UTC()
	}
	ns.s.memoryEntities[entity.ID] = cloneMemoryEntity(entity)
	ns.s.memoryEntitiesByNorm[entity.WorkspaceID+":"+entity.NormalizedLabel] = entity.ID
	return nil
}

func (ns *memoryEntityStore) GetEntityByLabel(ctx context.Context, workspaceID, normalizedLabel string) (*domain.MemoryEntity, error) {
	if workspaceID == "" || normalizedLabel == "" {
		return nil, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	e, exists := ns.s.entityByNormalizedLocked(workspaceID, normalizedLabel)
	if !exists {
		return nil, nil
	}
	return cloneMemoryEntity(e), nil
}

func (ns *memoryEntityStore) FindEntitiesByLabelPrefix(ctx context.Context, workspaceID, label string, limit int) ([]domain.MemoryEntity, error) {
	if workspaceID == "" || strings.TrimSpace(label) == "" {
		return []domain.MemoryEntity{}, nil
	}
	normalized := domain.NormalizeEntityLabel(label)

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	type candidate struct {
		entity domain.MemoryEntity
		rank   int // 0 exact, 1 prefix, 2 trigram-similar
	}
	candidates := make([]candidate, 0)
	for _, e := range ns.s.memoryEntities {
		if e.WorkspaceID != workspaceID {
			continue
		}
		var rank int
		switch {
		case e.NormalizedLabel == normalized:
			rank = 0
		case strings.HasPrefix(e.NormalizedLabel, normalized):
			rank = 1
		case memoryNoteSimilarity(label, e.Label) > 0.3:
			rank = 2
		default:
			continue
		}
		candidates = append(candidates, candidate{entity: *cloneMemoryEntity(e), rank: rank})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		if candidates[i].rank == 2 {
			si := memoryNoteSimilarity(label, candidates[i].entity.Label)
			sj := memoryNoteSimilarity(label, candidates[j].entity.Label)
			if si != sj {
				return si > sj
			}
		}
		if !candidates[i].entity.LearnedAt.Equal(candidates[j].entity.LearnedAt) {
			return candidates[i].entity.LearnedAt.Before(candidates[j].entity.LearnedAt)
		}
		return candidates[i].entity.ID < candidates[j].entity.ID
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	entities := make([]domain.MemoryEntity, 0, len(candidates))
	for _, c := range candidates {
		entities = append(entities, c.entity)
	}
	return entities, nil
}

// ListEntities lists the workspace's entities ordered by normalized label
// then id — the consolidator's hygiene pass reads it to group duplicate
// normalized labels. limit <= 0 means no limit.
func (ns *memoryEntityStore) ListEntities(ctx context.Context, workspaceID string, limit int) ([]domain.MemoryEntity, error) {
	if workspaceID == "" {
		return []domain.MemoryEntity{}, nil
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	entities := make([]domain.MemoryEntity, 0)
	for _, e := range ns.s.memoryEntities {
		if e.WorkspaceID != workspaceID {
			continue
		}
		entities = append(entities, *cloneMemoryEntity(e))
	}
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].NormalizedLabel != entities[j].NormalizedLabel {
			return entities[i].NormalizedLabel < entities[j].NormalizedLabel
		}
		return entities[i].ID < entities[j].ID
	})
	if limit > 0 && len(entities) > limit {
		entities = entities[:limit]
	}
	return entities, nil
}

func (ns *memoryEntityStore) AddEdges(ctx context.Context, workspaceID string, edges []domain.MemoryEntityEdge) (int64, error) {
	if workspaceID == "" {
		return 0, domain.ErrInvalid
	}
	if len(edges) == 0 {
		return 0, nil
	}
	for i := range edges {
		if err := domain.ValidateMemoryEntityEdge(&edges[i]); err != nil {
			return 0, fmt.Errorf("edge %d: %w", i, err)
		}
		if edges[i].ID == "" {
			edges[i].ID = uuid.NewString()
		}
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	var inserted int64
	for i := range edges {
		e := &edges[i]
		// The guard join: edges whose entity is foreign or absent drop.
		ent, exists := ns.s.memoryEntities[e.EntityID]
		if !exists || ent.WorkspaceID != workspaceID {
			continue
		}
		if ns.s.edgeExistsLocked(e.EntityID, e.TargetType, e.TargetID) {
			continue // unique (entity, target): re-mentions link nothing twice
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now().UTC()
		}
		ns.s.memoryEntityEdges[e.ID] = cloneMemoryEntityEdge(e)
		inserted++
	}
	return inserted, nil
}

func (ns *memoryEntityStore) ListEdgesForEntities(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, entityIDs []string) ([]domain.MemoryEntityEdge, error) {
	if workspaceID == "" || len(entityIDs) == 0 {
		return []domain.MemoryEntityEdge{}, nil
	}
	wanted := make(map[string]struct{}, len(entityIDs))
	for _, id := range entityIDs {
		wanted[id] = struct{}{}
	}

	ns.s.mu.RLock()
	defer ns.s.mu.RUnlock()

	edges := make([]domain.MemoryEntityEdge, 0)
	for _, edge := range ns.s.memoryEntityEdges {
		if _, ok := wanted[edge.EntityID]; !ok {
			continue
		}
		ent, exists := ns.s.memoryEntities[edge.EntityID]
		if !exists || ent.WorkspaceID != workspaceID {
			continue
		}
		// Scope and liveness come from the LIVE linked row.
		linkedVisibility, linkedUser, linkedAgent, live := ns.s.liveLinkedMemoryLocked(edge.TargetType, workspaceID, edge.TargetID)
		if !live {
			continue
		}
		if !domain.MemoryVisibleTo(linkedVisibility, linkedUser, linkedAgent, viewerUserID, servingAgentID) {
			continue
		}
		edges = append(edges, *cloneMemoryEntityEdge(edge))
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].EntityID != edges[j].EntityID {
			return edges[i].EntityID < edges[j].EntityID
		}
		if edges[i].TargetType != edges[j].TargetType {
			return edges[i].TargetType < edges[j].TargetType
		}
		if edges[i].TargetID != edges[j].TargetID {
			return edges[i].TargetID < edges[j].TargetID
		}
		return edges[i].ID < edges[j].ID
	})
	return edges, nil
}

func (ns *memoryEntityStore) FoldEntity(ctx context.Context, workspaceID, duplicateID, intoID string) (int64, error) {
	if workspaceID == "" || duplicateID == "" || intoID == "" {
		return 0, domain.ErrInvalid
	}
	if duplicateID == intoID {
		return 0, fmt.Errorf("%w: an entity cannot be folded into itself", domain.ErrInvalid)
	}

	ns.s.mu.Lock()
	defer ns.s.mu.Unlock()

	survivor, survivorExists := ns.s.memoryEntities[intoID]
	duplicate, duplicateExists := ns.s.memoryEntities[duplicateID]
	if !survivorExists || survivor.WorkspaceID != workspaceID ||
		!duplicateExists || duplicate.WorkspaceID != workspaceID {
		return 0, domain.ErrNotFound
	}

	var moved int64
	for _, edge := range ns.s.memoryEntityEdges {
		if edge.EntityID != duplicateID {
			continue
		}
		if ns.s.edgeExistsLocked(intoID, edge.TargetType, edge.TargetID) {
			// The survivor already links this target: the edge stays on the
			// duplicate — never duplicated, never deleted (D10 copy-out).
			continue
		}
		edge.EntityID = intoID
		moved++
	}
	return moved, nil
}

// -------------------------------------------------------------------------
// fakeStore lookup helpers for the graph stores. Callers hold the store
// lock (write-locked for the upsert scans, read-locked for lookups).
// -------------------------------------------------------------------------

// memoryEmbeddingsByKey finds the stored embedding at the SQL unique index's
// natural identity (workspace, target, dimension).
func (s *fakeStore) memoryEmbeddingsByKey(key string) (*domain.MemoryEmbedding, bool) {
	for _, e := range s.memoryEmbeddings {
		if embeddingUpsertKey(e) == key {
			return e, true
		}
	}
	return nil, false
}

// entityByNormalizedLocked resolves the (workspace, normalized label)
// identity the SQL unique index pins.
func (s *fakeStore) entityByNormalizedLocked(workspaceID, normalizedLabel string) (*domain.MemoryEntity, bool) {
	id, ok := s.memoryEntitiesByNorm[workspaceID+":"+normalizedLabel]
	if !ok {
		return nil, false
	}
	e, exists := s.memoryEntities[id]
	return e, exists
}

// edgeExistsLocked reports the unique (entity, target) edge identity.
func (s *fakeStore) edgeExistsLocked(entityID string, targetType domain.MemoryTargetType, targetID string) bool {
	for _, edge := range s.memoryEntityEdges {
		if edge.EntityID == entityID && edge.TargetType == targetType && edge.TargetID == targetID {
			return true
		}
	}
	return false
}
