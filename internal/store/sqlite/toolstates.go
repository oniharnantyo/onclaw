package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type toolStateStore struct{ e executor }

const toolStateColumns = `name, enabled, created_at, updated_at`

func (r *toolStateStore) SetEnabled(ctx context.Context, name string, enabled bool) error {
	now := nowUTC()

	_, err := r.e.ExecContext(ctx, `
INSERT INTO tool_states (`+toolStateColumns+`)
VALUES (?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
enabled = excluded.enabled,
updated_at = excluded.updated_at`,
		name, boolInt(enabled), formatTime(now), formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("set tool %s enabled: %w", name, err)
	}
	return nil
}

func (r *toolStateStore) Enabled(ctx context.Context, name string) (bool, error) {
	row := r.e.QueryRowContext(ctx, `SELECT enabled FROM tool_states WHERE name = ?`, name)
	var enabled int
	err := row.Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No stored row: the default-on state (see the port contract).
			return true, nil
		}
		return false, fmt.Errorf("get tool %s state: %w", name, err)
	}
	return enabled == 1, nil
}

func (r *toolStateStore) List(ctx context.Context) ([]store.ToolState, error) {
	rows, err := r.e.QueryContext(ctx, `SELECT `+toolStateColumns+` FROM tool_states ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tool states: %w", err)
	}
	defer rows.Close()

	states := make([]store.ToolState, 0)
	for rows.Next() {
		var state store.ToolState
		var createdAt, updatedAt string
		if err := rows.Scan(&state.Name, &state.Enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("list tool states: %w", err)
		}
		if state.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if state.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tool states: %w", err)
	}
	return states, nil
}
