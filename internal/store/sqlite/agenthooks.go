package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type agentHookStore struct{ e executor }

func (h *agentHookStore) Assign(ctx context.Context, agentID, hookID string, position int) error {
	// The EXISTS guards double as the FK check: a pair referencing an
	// absent agent or hook inserts nothing, surfacing as the store
	// sentinel instead of a driver constraint error.
	res, err := h.e.ExecContext(ctx, `
INSERT INTO agent_hooks (agent_id, hook_id, position)
SELECT ?, ?, ?
WHERE EXISTS (SELECT 1 FROM agents WHERE id = ?)
  AND EXISTS (SELECT 1 FROM hooks WHERE id = ?)
ON CONFLICT (agent_id, hook_id) DO UPDATE SET
position = excluded.position`,
		agentID, hookID, position, agentID, hookID,
	)
	if err != nil {
		return fmt.Errorf("assign hook %s to agent %s: %w", hookID, agentID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("assign hook %s to agent %s: %w", hookID, agentID, store.ErrNotFound)
	}
	return nil
}

func (h *agentHookStore) Unassign(ctx context.Context, agentID, hookID string) error {
	res, err := h.e.ExecContext(ctx,
		`DELETE FROM agent_hooks WHERE agent_id = ? AND hook_id = ?`, agentID, hookID)
	if err != nil {
		return fmt.Errorf("unassign hook %s from agent %s: %w", hookID, agentID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unassign hook %s from agent %s: %w", hookID, agentID, store.ErrNotFound)
	}
	return nil
}

func (h *agentHookStore) Assignments(ctx context.Context, agentID string) ([]store.AgentHook, error) {
	rows, err := h.e.QueryContext(ctx,
		`SELECT agent_id, hook_id, position FROM agent_hooks WHERE agent_id = ?
ORDER BY position, hook_id`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list assignments of agent %s: %w", agentID, err)
	}
	defer rows.Close()

	out := make([]store.AgentHook, 0)
	for rows.Next() {
		var a store.AgentHook
		if err := rows.Scan(&a.AgentID, &a.HookID, &a.Position); err != nil {
			return nil, fmt.Errorf("list assignments of agent %s: %w", agentID, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assignments of agent %s: %w", agentID, err)
	}
	return out, nil
}

func (h *agentHookStore) UnassignedHooks(ctx context.Context) ([]store.Hook, error) {
	// NOT EXISTS, not a LEFT JOIN: a hook assigned to any agent leaves the
	// global tier for everyone (design D4).
	rows, err := h.e.QueryContext(ctx,
		`SELECT `+hookColumns+` FROM hooks
WHERE NOT EXISTS (SELECT 1 FROM agent_hooks WHERE agent_hooks.hook_id = hooks.id)
ORDER BY position, name`)
	if err != nil {
		return nil, fmt.Errorf("list unassigned hooks: %w", err)
	}
	defer rows.Close()

	hooks := make([]store.Hook, 0)
	for rows.Next() {
		hook, err := scanHook(rows)
		if err != nil {
			return nil, fmt.Errorf("list unassigned hooks: %w", err)
		}
		hooks = append(hooks, *hook)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list unassigned hooks: %w", err)
	}
	return hooks, nil
}

func (h *agentHookStore) HooksForAgent(ctx context.Context, agentID string) ([]store.Hook, error) {
	// Columns are table-qualified: agent_hooks also has a position column,
	// so the shared hookColumns list would be ambiguous in the join.
	rows, err := h.e.QueryContext(ctx,
		`SELECT hooks.id, hooks.name, hooks.event, hooks.matcher, hooks.handler_type,
hooks.config, hooks.timeout_ms, hooks.on_failure, hooks.enabled, hooks.position,
hooks.status, hooks.status_error, hooks.created_at, hooks.updated_at
FROM hooks
JOIN agent_hooks ON agent_hooks.hook_id = hooks.id
WHERE agent_hooks.agent_id = ?
ORDER BY agent_hooks.position, hooks.name`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list hooks of agent %s: %w", agentID, err)
	}
	defer rows.Close()

	hooks := make([]store.Hook, 0)
	for rows.Next() {
		hook, err := scanHook(rows)
		if err != nil {
			return nil, fmt.Errorf("list hooks of agent %s: %w", agentID, err)
		}
		hooks = append(hooks, *hook)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list hooks of agent %s: %w", agentID, err)
	}
	return hooks, nil
}
