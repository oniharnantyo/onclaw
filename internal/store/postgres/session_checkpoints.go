package postgres

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// sessionCheckpointStore implements storeport.SessionCheckpointStore for PostgreSQL.
type sessionCheckpointStore struct {
	db Executor
}

// NewSessionCheckpointStore creates a new SessionCheckpointStore with the given database executor.
func NewSessionCheckpointStore(db Executor) storeport.SessionCheckpointStore {
	return &sessionCheckpointStore{db: db}
}

// Get retrieves a checkpoint by ID. Returns (data, true, nil) if found, (nil, false, nil) if not found.
func (s *sessionCheckpointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	if checkpointID == "" {
		return nil, false, domain.ErrInvalid
	}

	var data []byte
	err := s.db.QueryRow(ctx,
		`SELECT data FROM session_checkpoints WHERE checkpoint_id = $1`,
		checkpointID,
	).Scan(&data)
	if err != nil {
		converted := convertError(err)
		if converted == domain.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, converted
	}
	return data, true, nil
}

// Set upserts a checkpoint by ID.
func (s *sessionCheckpointStore) Set(ctx context.Context, checkpointID string, data []byte) error {
	if checkpointID == "" {
		return domain.ErrInvalid
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO session_checkpoints (checkpoint_id, data)
		 VALUES ($1, $2)
		 ON CONFLICT (checkpoint_id) DO UPDATE SET data = EXCLUDED.data`,
		checkpointID, data,
	)
	return convertError(err)
}

// Delete removes a checkpoint by ID. Returns ErrNotFound if absent.
func (s *sessionCheckpointStore) Delete(ctx context.Context, checkpointID string) error {
	if checkpointID == "" {
		return domain.ErrNotFound
	}

	tag, err := s.db.Exec(ctx,
		`DELETE FROM session_checkpoints WHERE checkpoint_id = $1`,
		checkpointID,
	)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
