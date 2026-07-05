package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// sqliteStagedWriteStore implements memory.StagedWriteStore.
type sqliteStagedWriteStore struct {
	db *sql.DB
}

// NewStagedWriteStore creates a new StagedWriteStore backed by SQLite.
func NewStagedWriteStore(db *sql.DB) memory.StagedWriteStore {
	return &sqliteStagedWriteStore{db: db}
}

func (s *sqliteStagedWriteStore) StageWrite(ctx context.Context, agent string, op string, target string, content string) (int64, error) {
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO staged_memory_writes (agent, operation, target, content, status, created_at)
		VALUES (?, ?, ?, ?, 'pending', ?)`,
		agent, op, target, content, now,
	)
	if err != nil {
		return 0, fmt.Errorf("stage write: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

func (s *sqliteStagedWriteStore) ListStaged(ctx context.Context, agent string) ([]*memory.StagedWrite, error) {
	var query string
	var args []interface{}
	if agent == "" {
		query = `
			SELECT id, agent, operation, target, content, status, created_at
			FROM staged_memory_writes
			WHERE status = 'pending'
			ORDER BY created_at ASC`
	} else {
		query = `
			SELECT id, agent, operation, target, content, status, created_at
			FROM staged_memory_writes
			WHERE agent = ? AND status = 'pending'
			ORDER BY created_at ASC`
		args = append(args, agent)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list staged writes: %w", err)
	}
	defer rows.Close()

	var writes []*memory.StagedWrite
	for rows.Next() {
		var sw memory.StagedWrite
		if err := rows.Scan(&sw.ID, &sw.Agent, &sw.Operation, &sw.Target, &sw.Content, &sw.Status, &sw.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan staged write: %w", err)
		}
		writes = append(writes, &sw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate staged writes: %w", err)
	}
	return writes, nil
}

func (s *sqliteStagedWriteStore) GetStagedWrite(ctx context.Context, id int64) (*memory.StagedWrite, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent, operation, target, content, status, created_at
		FROM staged_memory_writes
		WHERE id = ?`, id)
	var sw memory.StagedWrite
	if err := row.Scan(&sw.ID, &sw.Agent, &sw.Operation, &sw.Target, &sw.Content, &sw.Status, &sw.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("staged write %d not found", id)
		}
		return nil, fmt.Errorf("get staged write %d: %w", id, err)
	}
	return &sw, nil
}

func (s *sqliteStagedWriteStore) ApproveWrite(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE staged_memory_writes
		SET status = 'approved'
		WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("approve write: %w", err)
	}
	return nil
}

func (s *sqliteStagedWriteStore) RejectWrite(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE staged_memory_writes
		SET status = 'rejected'
		WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("reject write: %w", err)
	}
	return nil
}
