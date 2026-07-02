package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type sqliteHookStore struct {
	db *sql.DB
}

// NewHookStore creates a new HookStore backed by SQLite.
func NewHookStore(db *sql.DB) store.HookStore {
	return &sqliteHookStore{db: db}
}

func (s *sqliteHookStore) AddHook(ctx context.Context, h *store.Hook) error {
	if h.ID == "" {
		return fmt.Errorf("hook ID must not be empty")
	}
	if h.Name == "" {
		return fmt.Errorf("hook name must not be empty")
	}
	if h.CreatedAt == "" {
		h.CreatedAt = now()
	}
	h.UpdatedAt = now()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO agent_hooks (id, name, scope, event, handler_type, config, matcher, timeout_ms, on_timeout, priority, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		h.ID, h.Name, h.Scope, h.Event, h.HandlerType, h.Config, h.Matcher, h.TimeoutMS, h.OnTimeout, h.Priority, h.Enabled, h.CreatedAt, h.UpdatedAt,
	)
	return err
}

func (s *sqliteHookStore) GetHook(ctx context.Context, id string) (*store.Hook, error) {
	var h store.Hook
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, scope, event, handler_type, config, matcher, timeout_ms, on_timeout, priority, enabled, created_at, updated_at FROM agent_hooks WHERE id = ?",
		id,
	).Scan(&h.ID, &h.Name, &h.Scope, &h.Event, &h.HandlerType, &h.Config, &h.Matcher, &h.TimeoutMS, &h.OnTimeout, &h.Priority, &h.Enabled, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *sqliteHookStore) ListHooks(ctx context.Context) ([]*store.Hook, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, scope, event, handler_type, config, matcher, timeout_ms, on_timeout, priority, enabled, created_at, updated_at FROM agent_hooks ORDER BY priority DESC, created_at ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hooks []*store.Hook
	for rows.Next() {
		var h store.Hook
		err := rows.Scan(&h.ID, &h.Name, &h.Scope, &h.Event, &h.HandlerType, &h.Config, &h.Matcher, &h.TimeoutMS, &h.OnTimeout, &h.Priority, &h.Enabled, &h.CreatedAt, &h.UpdatedAt)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, &h)
	}
	return hooks, rows.Err()
}

func (s *sqliteHookStore) ListHooksByScopeAndEvent(ctx context.Context, scope string, event string) ([]*store.Hook, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, scope, event, handler_type, config, matcher, timeout_ms, on_timeout, priority, enabled, created_at, updated_at FROM agent_hooks WHERE scope = ? AND event = ? ORDER BY priority DESC, created_at ASC",
		scope, event,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hooks []*store.Hook
	for rows.Next() {
		var h store.Hook
		err := rows.Scan(&h.ID, &h.Name, &h.Scope, &h.Event, &h.HandlerType, &h.Config, &h.Matcher, &h.TimeoutMS, &h.OnTimeout, &h.Priority, &h.Enabled, &h.CreatedAt, &h.UpdatedAt)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, &h)
	}
	return hooks, rows.Err()
}

func (s *sqliteHookStore) UpdateHook(ctx context.Context, h *store.Hook) error {
	h.UpdatedAt = now()
	_, err := s.db.ExecContext(ctx,
		"UPDATE agent_hooks SET name = ?, scope = ?, event = ?, handler_type = ?, config = ?, matcher = ?, timeout_ms = ?, on_timeout = ?, priority = ?, enabled = ?, updated_at = ? WHERE id = ?",
		h.Name, h.Scope, h.Event, h.HandlerType, h.Config, h.Matcher, h.TimeoutMS, h.OnTimeout, h.Priority, h.Enabled, h.UpdatedAt, h.ID,
	)
	return err
}

func (s *sqliteHookStore) RemoveHook(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM agent_hooks WHERE id = ?", id)
	return err
}

func (s *sqliteHookStore) ToggleHook(ctx context.Context, id string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := s.db.ExecContext(ctx, "UPDATE agent_hooks SET enabled = ?, updated_at = ? WHERE id = ?", val, now(), id)
	return err
}

type sqliteHookExecutionStore struct {
	db *sql.DB
}

// NewHookExecutionStore creates a new HookExecutionStore backed by SQLite.
func NewHookExecutionStore(db *sql.DB) store.HookExecutionStore {
	return &sqliteHookExecutionStore{db: db}
}

func (s *sqliteHookExecutionStore) AppendExecution(ctx context.Context, exec *store.HookExecution) error {
	if exec.CreatedAt == "" {
		exec.CreatedAt = now()
	}
	var hookID sql.NullString
	if exec.HookID != "" {
		hookID.String = exec.HookID
		hookID.Valid = true
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO hook_executions (hook_id, event, handler_type, decision, duration_ms, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		hookID, exec.Event, exec.HandlerType, exec.Decision, exec.DurationMS, exec.Error, exec.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		exec.ID = id
	}
	return nil
}

func (s *sqliteHookExecutionStore) ListExecutions(ctx context.Context) ([]*store.HookExecution, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, hook_id, event, handler_type, decision, duration_ms, error, created_at FROM hook_executions ORDER BY created_at DESC, id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var execs []*store.HookExecution
	for rows.Next() {
		var exec store.HookExecution
		var hookID sql.NullString
		err := rows.Scan(&exec.ID, &hookID, &exec.Event, &exec.HandlerType, &exec.Decision, &exec.DurationMS, &exec.Error, &exec.CreatedAt)
		if err != nil {
			return nil, err
		}
		if hookID.Valid {
			exec.HookID = hookID.String
		}
		execs = append(execs, &exec)
	}
	return execs, rows.Err()
}
