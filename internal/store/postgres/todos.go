package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// todoStore implements storeport.TodoStore for PostgreSQL over agent_todos
// (adopt-assistant-ui-elements D5). Replace is ONE transaction — validation
// first, then upsert-by-key, delete-vanished, revision bump share the
// snapshot — so a rejected list never mutates state and no partial rewrite
// survives a failure. Every query carries the workspace partition.
type todoStore struct {
	db Executor
}

// NewTodoStore creates a new TodoStore with the given database executor.
func NewTodoStore(db Executor) storeport.TodoStore {
	return &todoStore{db: db}
}

const todoColumns = `workspace_id, agent_id, session_id, item_key, item_text, status, reason, revision`

// todoScopeGuard rejects empty scope inputs before they reach a uuid cast —
// an empty workspace id is invalid input, never an empty result.
func todoScopeGuard(workspaceID, agentID, sessionID string) error {
	if workspaceID == "" || agentID == "" || sessionID == "" {
		return fmt.Errorf("%w: workspace, agent, and session are required", domain.ErrInvalid)
	}
	return nil
}

// scanTodoRow reads one full-select row (todoColumns order) into an item,
// folding the revision into the running list maximum.
func scanTodoRow(rows pgx.Rows, list *storeport.TodoList) error {
	var (
		item     storeport.TodoItem
		status   string
		reason   *string
		revision int64
	)
	if err := rows.Scan(&list.WorkspaceID, &list.AgentID, &list.SessionID,
		&item.ItemKey, &item.ItemText, &status, &reason, &revision); err != nil {
		return convertError(err)
	}
	item.Status = storeport.TodoStatus(status)
	if reason != nil {
		item.Reason = *reason
	}
	if revision > list.Revision {
		list.Revision = revision
	}
	list.Items = append(list.Items, item)
	return nil
}

func (ts *todoStore) GetBySession(ctx context.Context, workspaceID, agentID, sessionID string) (*storeport.TodoList, error) {
	if err := todoScopeGuard(workspaceID, agentID, sessionID); err != nil {
		return nil, err
	}

	// Key order — the stable identity rewrites restyle in place (the port's
	// documented read order). Absence is a normal state: an empty list with
	// revision 0.
	rows, err := ts.db.Query(ctx, `
		SELECT `+todoColumns+`
		FROM agent_todos
		WHERE workspace_id = $1 AND agent_id = $2 AND session_id = $3
		ORDER BY item_key ASC
	`, workspaceID, agentID, sessionID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	list := &storeport.TodoList{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		SessionID:   sessionID,
		Items:       []storeport.TodoItem{},
	}
	for rows.Next() {
		if err := scanTodoRow(rows, list); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return list, nil
}

func (ts *todoStore) Replace(ctx context.Context, list *storeport.TodoList) (*storeport.TodoList, error) {
	if list == nil {
		return nil, domain.ErrInvalid
	}
	if err := todoScopeGuard(list.WorkspaceID, list.AgentID, list.SessionID); err != nil {
		return nil, err
	}
	// Validation BEFORE any write: a rejected list never mutates state
	// (adopt-assistant-ui-elements spec agent-todos).
	if err := storeport.ValidateTodoItems(list.Items); err != nil {
		return nil, err
	}

	// One transaction: upsert, delete, and the revision bump share one
	// snapshot (the scheduler store's txBeginner precedent — nested Executor
	// transactions ride savepoints).
	tx, err := ts.db.(txBeginner).Begin(ctx)
	if err != nil {
		return nil, convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// FK parity: unknown workspace or agent references fail before any row is
	// touched, mirroring the fake — the explicit check names the fault.
	var wsExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1)`, list.WorkspaceID).Scan(&wsExists); err != nil {
		return nil, convertError(err)
	}
	if !wsExists {
		return nil, fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	var agentExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agents WHERE id = $1 AND workspace_id = $2)`, list.AgentID, list.WorkspaceID).Scan(&agentExists); err != nil {
		return nil, convertError(err)
	}
	if !agentExists {
		return nil, fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	// The revision bump: the session's current MAX + 1, read before the
	// deletes so the increment computes even on a full clear.
	var maxRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0)
		FROM agent_todos
		WHERE workspace_id = $1 AND agent_id = $2 AND session_id = $3
	`, list.WorkspaceID, list.AgentID, list.SessionID).Scan(&maxRevision); err != nil {
		return nil, convertError(err)
	}
	nextRevision := maxRevision + 1

	// Upsert by key: existing keys restyle in place (the created_at birth
	// stamp survives, updated_at refreshes), new keys insert. The empty
	// reason normalizes to NULL exactly as it reads back.
	for _, item := range list.Items {
		var reason *string
		if item.Reason != "" {
			reason = &item.Reason
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_todos (workspace_id, agent_id, session_id, item_key, item_text, status, reason, revision)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (session_id, item_key) DO UPDATE SET
				item_text = EXCLUDED.item_text,
				status = EXCLUDED.status,
				reason = EXCLUDED.reason,
				revision = EXCLUDED.revision,
				updated_at = now()
		`, list.WorkspaceID, list.AgentID, list.SessionID, item.ItemKey, item.ItemText, string(item.Status), reason, nextRevision); err != nil {
			return nil, convertError(err)
		}
	}

	// Delete the keys absent from the call; an empty list clears the plan.
	if len(list.Items) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM agent_todos
			WHERE workspace_id = $1 AND agent_id = $2 AND session_id = $3
			  AND item_key <> ALL($4::text[])
		`, list.WorkspaceID, list.AgentID, list.SessionID, todoKeys(list.Items)); err != nil {
			return nil, convertError(err)
		}
	} else if _, err := tx.Exec(ctx, `
		DELETE FROM agent_todos
		WHERE workspace_id = $1 AND agent_id = $2 AND session_id = $3
	`, list.WorkspaceID, list.AgentID, list.SessionID); err != nil {
		return nil, convertError(err)
	}

	// The stored state as it now reads — inside the same transaction, so the
	// echoed list is the committed truth.
	rows, err := tx.Query(ctx, `
		SELECT `+todoColumns+`
		FROM agent_todos
		WHERE workspace_id = $1 AND agent_id = $2 AND session_id = $3
		ORDER BY item_key ASC
	`, list.WorkspaceID, list.AgentID, list.SessionID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	stored := &storeport.TodoList{
		WorkspaceID: list.WorkspaceID,
		AgentID:     list.AgentID,
		SessionID:   list.SessionID,
		Items:       []storeport.TodoItem{},
	}
	for rows.Next() {
		if err := scanTodoRow(rows, stored); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return nil, convertError(err)
	}
	return stored, nil
}

func (ts *todoStore) OpenItems(ctx context.Context, workspaceID, agentID string) ([]storeport.OpenTodoItem, error) {
	if workspaceID == "" || agentID == "" {
		return nil, fmt.Errorf("%w: workspace and agent are required", domain.ErrInvalid)
	}

	// The indexed open-items query (spec agent-todos: "a query, not a scan")
	// — idx_agent_todos_open_items covers the (workspace, agent, status)
	// prefix. Text ids (session_id, item_key) break updated_at ties.
	rows, err := ts.db.Query(ctx, `
		SELECT session_id, item_key, item_text, status, reason, updated_at
		FROM agent_todos
		WHERE workspace_id = $1 AND agent_id = $2 AND status IN ('pending', 'active')
		ORDER BY updated_at DESC, session_id ASC, item_key ASC
	`, workspaceID, agentID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	open := make([]storeport.OpenTodoItem, 0)
	for rows.Next() {
		var (
			item    storeport.OpenTodoItem
			status  string
			reason  *string
			itemKey string
		)
		if err := rows.Scan(&item.SessionID, &itemKey, &item.ItemText, &status, &reason, &item.UpdatedAt); err != nil {
			return nil, convertError(err)
		}
		item.ItemKey = itemKey
		item.Status = storeport.TodoStatus(status)
		if reason != nil {
			item.Reason = *reason
		}
		open = append(open, item)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return open, nil
}

// todoKeys extracts the item keys of a replace call for the ALL($) delete.
func todoKeys(items []storeport.TodoItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.ItemKey)
	}
	return keys
}
