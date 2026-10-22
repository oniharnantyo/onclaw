package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type hookStore struct{ e executor }

const hookColumns = `id, name, event, matcher, handler_type, config, timeout_ms, on_failure,
enabled, position, status, status_error, created_at, updated_at`

func (h *hookStore) Create(ctx context.Context, hook *store.Hook) error {
	if hook.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create hook: %w", err)
		}
		hook.ID = id
	}
	stampCreated(&hook.CreatedAt, &hook.UpdatedAt)

	_, err := h.e.ExecContext(ctx, `
INSERT INTO hooks (`+hookColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hook.ID, hook.Name, hook.Event, hook.Matcher, hook.HandlerType, hook.Config,
		hook.TimeoutMS, hook.OnFailure, boolInt(hook.Enabled), hook.Position,
		hook.Status, hook.StatusError, formatTime(hook.CreatedAt), formatTime(hook.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create hook: %w", err)
	}
	return nil
}

func (h *hookStore) List(ctx context.Context) ([]store.Hook, error) {
	rows, err := h.e.QueryContext(ctx,
		`SELECT `+hookColumns+` FROM hooks ORDER BY position, name`)
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	defer rows.Close()

	hooks := make([]store.Hook, 0)
	for rows.Next() {
		hook, err := scanHook(rows)
		if err != nil {
			return nil, fmt.Errorf("list hooks: %w", err)
		}
		hooks = append(hooks, *hook)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	return hooks, nil
}

func (h *hookStore) Update(ctx context.Context, hook *store.Hook) error {
	hook.UpdatedAt = nowUTC()

	res, err := h.e.ExecContext(ctx, `
UPDATE hooks SET name = ?, event = ?, matcher = ?, handler_type = ?, config = ?,
timeout_ms = ?, on_failure = ?, enabled = ?, position = ?, status = ?, status_error = ?,
updated_at = ?
WHERE id = ?`,
		hook.Name, hook.Event, hook.Matcher, hook.HandlerType, hook.Config, hook.TimeoutMS,
		hook.OnFailure, boolInt(hook.Enabled), hook.Position, hook.Status, hook.StatusError,
		formatTime(hook.UpdatedAt), hook.ID,
	)
	if err != nil {
		return fmt.Errorf("update hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update hook %s: %w", hook.ID, store.ErrNotFound)
	}
	return nil
}

func (h *hookStore) Delete(ctx context.Context, id string) error {
	res, err := h.e.ExecContext(ctx, `DELETE FROM hooks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete hook %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanHook(row scanner) (*store.Hook, error) {
	var hook store.Hook
	var createdAt, updatedAt string

	err := row.Scan(&hook.ID, &hook.Name, &hook.Event, &hook.Matcher, &hook.HandlerType,
		&hook.Config, &hook.TimeoutMS, &hook.OnFailure, &hook.Enabled, &hook.Position,
		&hook.Status, &hook.StatusError, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if hook.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if hook.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &hook, nil
}
