package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type hookExecutionStore struct{ e executor }

const hookExecutionColumns = `id, hook_id, agent_id, session_id, turn_id, event, outcome,
error, duration_ms, occurred_at`

func (h *hookExecutionStore) Record(ctx context.Context, execution *store.HookExecution) error {
	if execution.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("record hook execution: %w", err)
		}
		execution.ID = id
	}
	if execution.OccurredAt.IsZero() {
		execution.OccurredAt = nowUTC()
	}

	_, err := h.e.ExecContext(ctx, `
INSERT INTO hook_executions (`+hookExecutionColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		execution.ID, execution.HookID, execution.AgentID, execution.SessionID, execution.TurnID,
		execution.Event, execution.Outcome, execution.Error, execution.DurationMS,
		formatTime(execution.OccurredAt),
	)
	if err != nil {
		return fmt.Errorf("record hook execution: %w", err)
	}
	return nil
}

func (h *hookExecutionStore) List(ctx context.Context) ([]store.HookExecution, error) {
	rows, err := h.e.QueryContext(ctx,
		`SELECT `+hookExecutionColumns+` FROM hook_executions
ORDER BY occurred_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("list hook executions: %w", err)
	}
	defer rows.Close()

	out := make([]store.HookExecution, 0)
	for rows.Next() {
		var e store.HookExecution
		var occurredAt string
		if err := rows.Scan(&e.ID, &e.HookID, &e.AgentID, &e.SessionID, &e.TurnID,
			&e.Event, &e.Outcome, &e.Error, &e.DurationMS, &occurredAt); err != nil {
			return nil, fmt.Errorf("list hook executions: %w", err)
		}
		if e.OccurredAt, err = parseTime(occurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list hook executions: %w", err)
	}
	return out, nil
}
