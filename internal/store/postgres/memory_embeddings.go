package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memoryEmbeddingStore implements storeport.MemoryEmbeddingStore for
// PostgreSQL over memory_embeddings (wave3-memory-vectors-and-graph D1/D5).
//
// Vectors travel as halfvec text literals ("[0.1,0.2,0.3]") with an explicit
// $n::halfvec cast rather than through client-side type registration: pgx
// type registration is per-connection state and hard-fails on databases
// where the pgvector extension is absent, which would break the store's
// additive-and-inert rollback property, while the text codec makes every
// parameter a plain string the server casts. The extension's types live in
// pg_catalog (see migration 000059), so casts and the cosine operator
// resolve under any search_path. Search returns scope fields and target
// pointers only — half-precision readback is lossy and ordering happens
// server-side, so no vector is ever decoded.
type memoryEmbeddingStore struct {
	db Executor
}

// NewMemoryEmbeddingStore creates a new MemoryEmbeddingStore with the given database executor.
func NewMemoryEmbeddingStore(db Executor) storeport.MemoryEmbeddingStore {
	return &memoryEmbeddingStore{db: db}
}

// memoryEmbeddingColumns is the read column list — the vector is
// deliberately absent (see the type comment). The me. qualification keeps
// every column unambiguous under the search's live-row joins.
const memoryEmbeddingColumns = `me.id, me.workspace_id, me.target_type, me.target_id, me.dimension,
	me.visibility, me.user_id, me.agent_id, me.source_event_id, me.learned_at, me.created_at`

// encodeHalfvec renders the halfvec text literal for v: "[a,b,c]" with the
// shortest exact float32 representations.
func encodeHalfvec(v []float32) string {
	var buf strings.Builder
	buf.Grow(2 + 16*len(v))
	buf.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	buf.WriteByte(']')
	return buf.String()
}

func scanMemoryEmbedding(row pgx.Row) (*domain.MemoryEmbedding, error) {
	var e domain.MemoryEmbedding
	var targetType, visibility string
	err := row.Scan(&e.ID, &e.WorkspaceID, &targetType, &e.TargetID, &e.Dimension,
		&visibility, &e.UserID, &e.AgentID, &e.SourceEventID, &e.LearnedAt, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, convertError(err)
	}
	e.TargetType = domain.MemoryTargetType(targetType)
	e.Visibility = domain.MemoryVisibility(visibility)
	return &e, nil
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

	// One multi-row statement per ingestion job; the upsert keeps a re-embed
	// at the same dimension idempotent (vector and scope refresh, birth
	// identity and created_at stay as stored).
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO memory_embeddings (
			id, workspace_id, target_type, target_id, dimension, embedding,
			visibility, user_id, agent_id, source_event_id, learned_at
		) VALUES `)
	args := make([]any, 0, len(embeddings)*11)
	for i := range embeddings {
		e := &embeddings[i]
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * 11
		fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d, $%d, $%d::halfvec, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11)
		args = append(args, e.ID, e.WorkspaceID, string(e.TargetType), e.TargetID, e.Dimension,
			encodeHalfvec(e.Embedding), string(e.Visibility), e.UserID, e.AgentID,
			e.SourceEventID, e.LearnedAt)
	}
	sb.WriteString(`
		ON CONFLICT (workspace_id, target_type, target_id, dimension) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			visibility = EXCLUDED.visibility,
			user_id = EXCLUDED.user_id,
			agent_id = EXCLUDED.agent_id,
			source_event_id = EXCLUDED.source_event_id,
			learned_at = EXCLUDED.learned_at
	`)
	if _, err := es.db.Exec(ctx, sb.String(), args...); err != nil {
		return convertError(err)
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

	const query = `
		DELETE FROM memory_embeddings
		WHERE workspace_id = $1 AND target_type = $2 AND target_id = $3
	`
	tag, err := es.db.Exec(ctx, query, workspaceID, string(targetType), targetID)
	if err != nil {
		return 0, convertError(err)
	}
	return tag.RowsAffected(), nil
}

func (es *memoryEmbeddingStore) SearchByVector(ctx context.Context, workspaceID, viewerUserID, servingAgentID string, targetTypes []domain.MemoryTargetType, dimension int, query []float32, filters storeport.MemoryEmbeddingFilters, limit int) ([]domain.MemoryEmbedding, error) {
	if workspaceID == "" {
		return []domain.MemoryEmbedding{}, nil
	}
	if dimension <= 0 {
		return nil, fmt.Errorf("%w: memory embedding dimension must be positive", domain.ErrInvalid)
	}
	if len(query) != dimension {
		return nil, fmt.Errorf("%w: query vector length %d does not match dimension %d", domain.ErrInvalid, len(query), dimension)
	}

	// The structural scope (D4/D8) is computed per target kind: raw rows
	// carry their own snapshot and owner columns; note/event rows take BOTH
	// the tier and the owners from the LIVE linked row — so a promoted
	// note's embeddings widen exactly as far as the note itself. Dead
	// targets are excluded at read time (missing or tombstoned linked row);
	// raw rows have no tombstone. NULLIF maps an empty identity to NULL so
	// it contributes no rows of its tier (the memoryScopeClause convention).
	clause := `
		FROM memory_embeddings me
		LEFT JOIN memory_notes n ON me.target_type = 'note' AND n.id::text = me.target_id
		LEFT JOIN memory_events e ON me.target_type = 'event' AND e.id::text = me.target_id
		WHERE me.workspace_id = $1
		  AND me.dimension = $2
		  AND (
			(me.target_type = 'raw' AND (
				me.visibility = 'shared'
				OR (me.visibility = 'user' AND me.user_id = NULLIF($3::text, '')::uuid)
				OR (me.visibility = 'agent' AND me.agent_id = NULLIF($4::text, '')::uuid)
			))
			OR (me.target_type = 'note' AND n.id IS NOT NULL AND n.tombstoned_at IS NULL AND (
				n.visibility = 'shared'
				OR (n.visibility = 'user' AND n.user_id = NULLIF($3::text, '')::uuid)
				OR (n.visibility = 'agent' AND n.agent_id = NULLIF($4::text, '')::uuid)
			))
			OR (me.target_type = 'event' AND e.id IS NOT NULL AND e.tombstoned_at IS NULL AND (
				e.visibility = 'shared'
				OR (e.visibility = 'user' AND e.user_id = NULLIF($3::text, '')::uuid)
				OR (e.visibility = 'agent' AND e.agent_id = NULLIF($4::text, '')::uuid)
			))
		  )
	`
	args := []any{workspaceID, dimension, viewerUserID, servingAgentID}

	if len(targetTypes) > 0 {
		kinds := make([]string, len(targetTypes))
		for i, t := range targetTypes {
			kinds[i] = string(t)
		}
		args = append(args, kinds)
		clause += fmt.Sprintf(` AND me.target_type = ANY($%d)`, len(args))
	}
	if filters.Visibility != "" {
		if !domain.ValidMemoryVisibility(filters.Visibility) {
			return nil, fmt.Errorf("%w: unknown memory visibility %q", domain.ErrInvalid, filters.Visibility)
		}
		args = append(args, string(filters.Visibility))
		clause += fmt.Sprintf(` AND me.visibility = $%d`, len(args))
	}

	// Nearest first; kind/id tiebreak keeps equal-distance rows deterministic.
	args = append(args, encodeHalfvec(query))
	clause += fmt.Sprintf(` ORDER BY me.embedding <=> $%d::halfvec, me.target_type, me.target_id`, len(args))
	if limit > 0 {
		args = append(args, limit)
		clause += fmt.Sprintf(` LIMIT $%d`, len(args))
	}

	rows, err := es.db.Query(ctx, `SELECT `+memoryEmbeddingColumns+` `+clause, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	embeddings := make([]domain.MemoryEmbedding, 0)
	for rows.Next() {
		e, err := scanMemoryEmbedding(rows)
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return embeddings, nil
}
