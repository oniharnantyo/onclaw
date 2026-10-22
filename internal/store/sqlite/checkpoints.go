package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type checkpointStore struct{ e executor }

func (c *checkpointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	var data []byte
	var createdAt, updatedAt string

	err := c.e.QueryRowContext(ctx,
		`SELECT data, created_at, updated_at FROM checkpoints WHERE checkpoint_id = ?`,
		checkpointID).Scan(&data, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, readErr("get checkpoint", err)
	}
	return data, true, nil
}

func (c *checkpointStore) Set(ctx context.Context, checkpointID string, data []byte) error {
	now := nowUTC()

	_, err := c.e.ExecContext(ctx, `
INSERT INTO checkpoints (checkpoint_id, data, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (checkpoint_id) DO UPDATE SET
data = excluded.data,
updated_at = excluded.updated_at`,
		checkpointID, data, formatTime(now), formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("set checkpoint %s: %w", checkpointID, err)
	}
	return nil
}

func (c *checkpointStore) Delete(ctx context.Context, checkpointID string) error {
	res, err := c.e.ExecContext(ctx, `DELETE FROM checkpoints WHERE checkpoint_id = ?`, checkpointID)
	if err != nil {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete checkpoint %s: %w", checkpointID, store.ErrNotFound)
	}
	return nil
}
