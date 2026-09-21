package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memoryEntityStore implements storeport.MemoryEntityStore for PostgreSQL
// over memory_entities + memory_entity_edges (wave3-memory-vectors-and-graph
// D7/D8/D10). Entity identity is (workspace, normalized label) — the unique
// index makes ResolveEntity idempotent — and every edge read takes its scope
// from the LIVE linked row, the same predicate the direct read path uses, so
// traversal cannot reach anything the caller could not read directly. Rows
// are never deleted: folds re-point edges and leave the folded duplicate in
// place (copy-out).
type memoryEntityStore struct {
	db Executor
}

// NewMemoryEntityStore creates a new MemoryEntityStore with the given database executor.
func NewMemoryEntityStore(db Executor) storeport.MemoryEntityStore {
	return &memoryEntityStore{db: db}
}

const memoryEntityColumns = `id, workspace_id, label, normalized_label, origin, source_event_id, learned_at, created_at`

func scanMemoryEntity(row pgx.Row) (*domain.MemoryEntity, error) {
	var e domain.MemoryEntity
	var origin string
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.Label, &e.NormalizedLabel, &origin,
		&e.SourceEventID, &e.LearnedAt, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	e.Origin = domain.MemoryOrigin(origin)
	return &e, nil
}

// ResolveEntity inserts the proposed entity when its normalized label is new
// to the workspace and loads the stored row otherwise (D7 idempotency): the
// single statement makes the first label's spelling win at birth and never
// rewritten, and concurrent resolvers serialize on the unique index.
func (ns *memoryEntityStore) ResolveEntity(ctx context.Context, entity *domain.MemoryEntity) error {
	if err := domain.ValidateMemoryEntity(entity); err != nil {
		return err
	}
	if entity.ID == "" {
		entity.ID = uuid.NewString()
	}

	const query = `
		WITH inserted AS (
			INSERT INTO memory_entities (
				id, workspace_id, label, normalized_label, origin, source_event_id, learned_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (workspace_id, normalized_label) DO NOTHING
			RETURNING ` + memoryEntityColumns + `
		)
		SELECT ` + memoryEntityColumns + `, true FROM inserted
		UNION ALL
		SELECT e.id, e.workspace_id, e.label, e.normalized_label, e.origin, e.source_event_id, e.learned_at, e.created_at, false
		FROM memory_entities e
		WHERE e.workspace_id = $2 AND e.normalized_label = $4
		  AND NOT EXISTS (SELECT 1 FROM inserted)
		LIMIT 1
	`
	var stored domain.MemoryEntity
	var origin string
	var inserted bool // true when this call created the row; discarded
	err := ns.db.QueryRow(ctx, query,
		entity.ID, entity.WorkspaceID, entity.Label, entity.NormalizedLabel,
		string(entity.Origin), entity.SourceEventID, entity.LearnedAt).
		Scan(&stored.ID, &stored.WorkspaceID, &stored.Label, &stored.NormalizedLabel, &origin,
			&stored.SourceEventID, &stored.LearnedAt, &stored.CreatedAt, &inserted)
	if err != nil {
		return convertError(err)
	}
	stored.Origin = domain.MemoryOrigin(origin)
	*entity = stored
	return nil
}

func (ns *memoryEntityStore) GetEntityByLabel(ctx context.Context, workspaceID, normalizedLabel string) (*domain.MemoryEntity, error) {
	if workspaceID == "" || normalizedLabel == "" {
		return nil, nil
	}

	const query = `
		SELECT ` + memoryEntityColumns + `
		FROM memory_entities
		WHERE workspace_id = $1 AND normalized_label = $2
	`
	e, err := scanMemoryEntity(ns.db.QueryRow(ctx, query, workspaceID, normalizedLabel))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// FindEntitiesByLabelPrefix is the D8 seed fallback: exact normalized match
// first, then normalized-label prefix, then trigram similarity on the label
// (the pg_trgm coverage migration 000060 lays down). Ties resolve oldest
// entity first, id last — deterministic.
func (ns *memoryEntityStore) FindEntitiesByLabelPrefix(ctx context.Context, workspaceID, label string, limit int) ([]domain.MemoryEntity, error) {
	if workspaceID == "" || strings.TrimSpace(label) == "" {
		return []domain.MemoryEntity{}, nil
	}
	normalized := domain.NormalizeEntityLabel(label)

	clause := `
		SELECT ` + memoryEntityColumns + `
		FROM memory_entities
		WHERE workspace_id = $1
		  AND (normalized_label = $2 OR normalized_label LIKE $2 || '%' OR similarity(label, $3) > 0.3)
		ORDER BY (normalized_label = $2) DESC,
		         (normalized_label LIKE $2 || '%') DESC,
		         similarity(label, $3) DESC,
		         learned_at ASC, id ASC
	`
	args := []any{workspaceID, normalized, label}
	if limit > 0 {
		args = append(args, limit)
		clause += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := ns.db.Query(ctx, clause, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	entities := make([]domain.MemoryEntity, 0)
	for rows.Next() {
		e, err := scanMemoryEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return entities, nil
}

// ListEntities lists the workspace's entities ordered by normalized label
// then id — the consolidator's hygiene pass (wave3 D10) reads it to group
// duplicate normalized labels before folding. limit <= 0 means no limit.
func (ns *memoryEntityStore) ListEntities(ctx context.Context, workspaceID string, limit int) ([]domain.MemoryEntity, error) {
	if workspaceID == "" {
		return []domain.MemoryEntity{}, nil
	}

	clause := `
		SELECT ` + memoryEntityColumns + `
		FROM memory_entities
		WHERE workspace_id = $1
		ORDER BY normalized_label ASC, id ASC
	`
	args := []any{workspaceID}
	if limit > 0 {
		args = append(args, limit)
		clause += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := ns.db.Query(ctx, clause, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	entities := make([]domain.MemoryEntity, 0)
	for rows.Next() {
		e, err := scanMemoryEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return entities, nil
}

// AddEdges inserts one ingestion job's edge batch in one statement. Rows
// whose entity is absent from the workspace are dropped by the guard join —
// the pipeline resolves entities first, so a drop means a bug upstream that
// the returned count makes visible — and the unique (entity, target) index
// keeps re-mentions idempotent.
func (ns *memoryEntityStore) AddEdges(ctx context.Context, workspaceID string, edges []domain.MemoryEntityEdge) (int64, error) {
	if workspaceID == "" {
		return 0, domain.ErrInvalid
	}
	if len(edges) == 0 {
		return 0, nil
	}
	// Validate every edge (a malformed proposal fails the batch, naming its
	// index) and collect the rows that can reference anything: an entity id
	// that is not even a uuid is dropped exactly like a foreign-entity edge
	// (the documented drop — the pipeline resolves entities first).
	rows := make([]*domain.MemoryEntityEdge, 0, len(edges))
	for i := range edges {
		if err := domain.ValidateMemoryEntityEdge(&edges[i]); err != nil {
			return 0, fmt.Errorf("edge %d: %w", i, err)
		}
		if _, err := uuid.Parse(edges[i].EntityID); err != nil {
			continue
		}
		if edges[i].ID == "" {
			edges[i].ID = uuid.NewString()
		}
		rows = append(rows, &edges[i])
	}
	if len(rows) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO memory_entity_edges (
			id, entity_id, target_type, target_id, visibility, origin, source_event_id
		)
		SELECT v.id, v.entity_id, v.target_type, v.target_id, v.visibility, v.origin, v.source_event_id
		FROM (VALUES `)
	args := make([]any, 0, len(rows)*7+1)
	args = append(args, workspaceID)
	for i := range rows {
		e := rows[i]
		if i > 0 {
			sb.WriteByte(',')
		}
		// $1 is the workspace; each edge claims the next seven placeholders.
		base := i * 7
		fmt.Fprintf(&sb, "($%d::uuid, $%d::uuid, $%d, $%d, $%d, $%d, $%d)",
			base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		args = append(args, e.ID, e.EntityID, string(e.TargetType), e.TargetID,
			string(e.Visibility), string(e.Origin), e.SourceEventID)
	}
	sb.WriteString(`) AS v(id, entity_id, target_type, target_id, visibility, origin, source_event_id)
		JOIN memory_entities ent ON ent.id = v.entity_id AND ent.workspace_id = $1
		ON CONFLICT (entity_id, target_type, target_id) DO NOTHING
	`)
	tag, err := ns.db.Exec(ctx, sb.String(), args...)
	if err != nil {
		return 0, convertError(err)
	}
	return tag.RowsAffected(), nil
}

// ListEdgesForEntities returns the caller-visible live edges of the given
// entities. Tier and owners come from the linked row per edge (shared rows
// are world-visible within the workspace; user/agent tiers must match the
// viewer/serving agent through the LIVE note or event — promotion widens
// through the row, never through an edge), and edges whose target row is
// missing or tombstoned are dead pointers, excluded at read time. NULLIF
// maps an empty identity to NULL (the memoryScopeClause convention).
func (ns *memoryEntityStore) ListEdgesForEntities(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, entityIDs []string) ([]domain.MemoryEntityEdge, error) {
	if workspaceID == "" || len(entityIDs) == 0 {
		return []domain.MemoryEntityEdge{}, nil
	}

	const query = `
		SELECT ed.id, ed.entity_id, ed.target_type, ed.target_id, ed.visibility,
		       ed.origin, ed.source_event_id, ed.created_at
		FROM memory_entity_edges ed
		JOIN memory_entities ent ON ent.id = ed.entity_id AND ent.workspace_id = $1
		LEFT JOIN memory_notes n ON ed.target_type = 'note' AND n.id::text = ed.target_id
		LEFT JOIN memory_events e ON ed.target_type = 'event' AND e.id::text = ed.target_id
		WHERE ed.entity_id = ANY($2::uuid[])
		  AND (
			(ed.target_type = 'note' AND n.id IS NOT NULL AND n.tombstoned_at IS NULL AND (
				n.visibility = 'shared'
				OR (n.visibility = 'user' AND n.user_id = NULLIF($3::text, '')::uuid)
				OR (n.visibility = 'agent' AND n.agent_id = NULLIF($4::text, '')::uuid)
			))
			OR (ed.target_type = 'event' AND e.id IS NOT NULL AND e.tombstoned_at IS NULL AND (
				e.visibility = 'shared'
				OR (e.visibility = 'user' AND e.user_id = NULLIF($3::text, '')::uuid)
				OR (e.visibility = 'agent' AND e.agent_id = NULLIF($4::text, '')::uuid)
			))
		  )
		ORDER BY ed.entity_id, ed.target_type, ed.target_id, ed.id
	`
	rows, err := ns.db.Query(ctx, query, workspaceID, entityIDs, viewerUserID, servingAgentID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	edges := make([]domain.MemoryEntityEdge, 0)
	for rows.Next() {
		var edge domain.MemoryEntityEdge
		var targetType, visibility, origin string
		if err := rows.Scan(&edge.ID, &edge.EntityID, &targetType, &edge.TargetID,
			&visibility, &origin, &edge.SourceEventID, &edge.CreatedAt); err != nil {
			return nil, convertError(err)
		}
		edge.TargetType = domain.MemoryTargetType(targetType)
		edge.Visibility = domain.MemoryVisibility(visibility)
		edge.Origin = domain.MemoryOrigin(origin)
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return edges, nil
}

// FoldEntity re-points every edge of duplicateID onto intoID (D10 copy-out):
// the single guarded statement locks both entities, moves only the duplicate's
// edges, and keeps an edge the survivor already holds on the duplicate (the
// unique index refuses a second copy and DO NOTHING preserves the row) — no
// edge or provenance row is ever deleted, and the folded duplicate stays in
// place as an inert pointer.
func (ns *memoryEntityStore) FoldEntity(ctx context.Context, workspaceID, duplicateID, intoID string) (int64, error) {
	if workspaceID == "" || duplicateID == "" || intoID == "" {
		return 0, domain.ErrInvalid
	}
	if duplicateID == intoID {
		return 0, fmt.Errorf("%w: an entity cannot be folded into itself", domain.ErrInvalid)
	}

	// UPDATE has no ON CONFLICT: the NOT EXISTS guard excludes the targets
	// the survivor already links, so those edge rows stay on the duplicate
	// (never duplicated, never deleted — D10 copy-out) and the statement
	// cannot collide with the unique index.
	const query = `
		WITH survivor AS (
			SELECT id FROM memory_entities
			WHERE id = $3 AND workspace_id = $1
			FOR UPDATE
		), duplicate AS (
			SELECT id FROM memory_entities
			WHERE id = $2 AND workspace_id = $1
			FOR UPDATE
		), moved AS (
			UPDATE memory_entity_edges ed
			SET entity_id = (SELECT id FROM survivor)
			WHERE ed.entity_id = (SELECT id FROM duplicate)
			  AND EXISTS (SELECT 1 FROM survivor)
			  AND NOT EXISTS (
				SELECT 1 FROM memory_entity_edges keep
				WHERE keep.entity_id = (SELECT id FROM survivor)
				  AND keep.target_type = ed.target_type
				  AND keep.target_id = ed.target_id
			  )
			RETURNING ed.id
		)
		SELECT count(*) FROM moved
	`
	var moved int
	if err := ns.db.QueryRow(ctx, query, workspaceID, duplicateID, intoID).Scan(&moved); err != nil {
		return 0, convertError(err)
	}
	if moved == 0 {
		// A fold with zero re-pointed edges is a valid outcome (the duplicate
		// held no edges, or every one already exists on the survivor) — but
		// unknown or foreign entities fail the call instead.
		const existence = `
			SELECT count(*) FROM memory_entities
			WHERE workspace_id = $1 AND id = ANY($2::uuid[])
		`
		var known int
		if err := ns.db.QueryRow(ctx, existence, workspaceID, []string{duplicateID, intoID}).Scan(&known); err != nil {
			return 0, convertError(err)
		}
		if known != 2 {
			return 0, domain.ErrNotFound
		}
	}
	return int64(moved), nil
}
