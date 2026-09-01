package postgres

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// agentUserMemoryStore implements storeport.AgentUserMemoryStore for PostgreSQL.
type agentUserMemoryStore struct {
	db Executor
}

// NewAgentUserMemoryStore creates a new AgentUserMemoryStore with the given database executor.
func NewAgentUserMemoryStore(db Executor) storeport.AgentUserMemoryStore {
	return &agentUserMemoryStore{db: db}
}

func (ms *agentUserMemoryStore) Get(ctx context.Context, workspaceID, agentID, userID string) (*domain.AgentUserMemory, error) {
	if workspaceID == "" || agentID == "" || userID == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT agent_id, user_id, workspace_id, content, created_at, updated_at
		FROM agent_user_memories
		WHERE workspace_id = $1 AND agent_id = $2 AND user_id = $3
	`
	var m domain.AgentUserMemory
	err := ms.db.QueryRow(ctx, query, workspaceID, agentID, userID).Scan(
		&m.AgentID,
		&m.UserID,
		&m.WorkspaceID,
		&m.Content,
		&m.CreatedAt,
		&m.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &m, nil
}

func (ms *agentUserMemoryStore) Upsert(ctx context.Context, memory *domain.AgentUserMemory) error {
	if memory == nil || memory.WorkspaceID == "" || memory.AgentID == "" || memory.UserID == "" {
		return domain.ErrInvalid
	}

	now := time.Now().UTC()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	memory.UpdatedAt = now

	query := `
		INSERT INTO agent_user_memories (agent_id, user_id, workspace_id, content, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, user_id)
		DO UPDATE SET
			content = EXCLUDED.content,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`
	err := ms.db.QueryRow(ctx, query,
		memory.AgentID,
		memory.UserID,
		memory.WorkspaceID,
		memory.Content,
		memory.CreatedAt,
		memory.UpdatedAt,
	).Scan(
		&memory.CreatedAt,
		&memory.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ms *agentUserMemoryStore) Delete(ctx context.Context, workspaceID, agentID, userID string) error {
	if workspaceID == "" || agentID == "" || userID == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM agent_user_memories
		WHERE workspace_id = $1 AND agent_id = $2 AND user_id = $3
	`
	tag, err := ms.db.Exec(ctx, query, workspaceID, agentID, userID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
