package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memoryStore implements storeport.MemoryStore for PostgreSQL across the two
// memory documents: user_memories (workspace_id, user_id) and workspaces.memory
// (one column per workspace). Appends are single statements with an in-statement
// cap guard (design D6) — no read-modify-write race and no partial cap bypass.
type memoryStore struct {
	db Executor
}

// NewMemoryStore creates a new MemoryStore with the given database executor.
func NewMemoryStore(db Executor) storeport.MemoryStore {
	return &memoryStore{db: db}
}

func (ms *memoryStore) UserMemory(ctx context.Context, workspaceID, userID string) (*domain.Memory, error) {
	if workspaceID == "" || userID == "" {
		return nil, nil
	}

	const query = `
		SELECT content, updated_at
		FROM user_memories
		WHERE workspace_id = $1 AND user_id = $2
	`
	var m domain.Memory
	err := ms.db.QueryRow(ctx, query, workspaceID, userID).Scan(&m.Content, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, convertError(err)
	}
	return &m, nil
}

func (ms *memoryStore) UpsertUserMemory(ctx context.Context, workspaceID, userID, content string) error {
	if workspaceID == "" || userID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	const query = `
		INSERT INTO user_memories (workspace_id, user_id, content, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (workspace_id, user_id)
		DO UPDATE SET
			content = EXCLUDED.content,
			updated_at = EXCLUDED.updated_at
	`
	_, err := ms.db.Exec(ctx, query, workspaceID, userID, content)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ms *memoryStore) AppendUserMemory(ctx context.Context, workspaceID, userID, content string) error {
	if workspaceID == "" || userID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	// Single statement: insert the fragment when no row exists, otherwise
	// concatenate only if the resulting document stays under the cap. Zero
	// affected rows means the cap guard blocked the append.
	const query = `
		INSERT INTO user_memories (workspace_id, user_id, content, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (workspace_id, user_id)
		DO UPDATE SET
			content = user_memories.content || $3,
			updated_at = now()
		WHERE length(user_memories.content) + length($3) <= $4
	`
	tag, err := ms.db.Exec(ctx, query, workspaceID, userID, content, domain.MaxMemoryContentChars)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return memoryCapExceededErr()
	}
	return nil
}

func (ms *memoryStore) WorkspaceMemory(ctx context.Context, workspaceID string) (*domain.Memory, error) {
	if workspaceID == "" {
		return nil, nil
	}

	const query = `
		SELECT memory, updated_at
		FROM workspaces
		WHERE id = $1
	`
	var m domain.Memory
	err := ms.db.QueryRow(ctx, query, workspaceID).Scan(&m.Content, &m.UpdatedAt)
	if err != nil {
		return nil, convertError(err)
	}
	return &m, nil
}

func (ms *memoryStore) UpsertWorkspaceMemory(ctx context.Context, workspaceID, content string) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	// Targeted column update: memory never rides WorkspaceStore.Update's
	// column list, so settings saves and memory saves cannot clobber each
	// other (design D2).
	const query = `
		UPDATE workspaces
		SET memory = $1, updated_at = now()
		WHERE id = $2
	`
	tag, err := ms.db.Exec(ctx, query, content, workspaceID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (ms *memoryStore) AppendWorkspaceMemory(ctx context.Context, workspaceID, content string) error {
	if workspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateMemoryContent(content); err != nil {
		return err
	}

	// Targeted, guarded UPDATE: memory never rides WorkspaceStore.Update's
	// column list (design D2), and the length predicate makes the append a
	// single atomic statement (design D6). A workspace without stored memory
	// starts from '' (the column default), so concatenation still works.
	const query = `
		UPDATE workspaces
		SET memory = memory || $2, updated_at = now()
		WHERE id = $1 AND length(memory) + length($2) <= $3
	`
	tag, err := ms.db.Exec(ctx, query, workspaceID, content, domain.MaxMemoryContentChars)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		// Ambiguous between a cap block and an unknown workspace;
		// disambiguate and name the current size when known.
		var current string
		scanErr := ms.db.QueryRow(ctx, `SELECT memory FROM workspaces WHERE id = $1`, workspaceID).Scan(&current)
		if scanErr != nil {
			return convertError(scanErr)
		}
		return fmt.Errorf("%w: %d + %d chars exceeds the %d char cap; trim it via the memory editor in the UI",
			domain.ErrMemoryCapExceeded, len(current), len(content), domain.MaxMemoryContentChars)
	}
	return nil
}

// memoryCapExceededErr builds the generic cap error for in-statement guards,
// where the current stored size is not known to the Go layer. It still names
// the cap and the human-trim escape hatch.
func memoryCapExceededErr() error {
	return fmt.Errorf("%w: append would exceed the %d char cap; trim it via the memory editor in the UI", domain.ErrMemoryCapExceeded, domain.MaxMemoryContentChars)
}
