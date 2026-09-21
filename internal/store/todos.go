package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TodoStatus is the lifecycle state of one todo item (adopt-assistant-ui-
// elements D5): pending (not started), active (in progress), done, failed.
type TodoStatus string

const (
	TodoStatusPending TodoStatus = "pending"
	TodoStatusActive  TodoStatus = "active"
	TodoStatusDone    TodoStatus = "done"
	TodoStatusFailed  TodoStatus = "failed"
)

// ValidTodoStatus reports whether s is one of the four allowed statuses.
func ValidTodoStatus(s TodoStatus) bool {
	switch s {
	case TodoStatusPending, TodoStatusActive, TodoStatusDone, TodoStatusFailed:
		return true
	}
	return false
}

// TodoItem is one item of an agent's session todo list, keyed by the stable
// item key the model assigns: rewrites restyle rows by key (status/text move
// in place) rather than remounting them (adopt-assistant-ui-elements D5).
// Reason carries the failure explanation for failed items; empty otherwise.
type TodoItem struct {
	ItemKey  string
	ItemText string
	Status   TodoStatus
	Reason   string
}

// TodoList is one session's current todo state: the items plus the session's
// revision counter, which increments on every Replace.
type TodoList struct {
	WorkspaceID string
	AgentID     string
	SessionID   string
	Revision    int64
	Items       []TodoItem
}

// OpenTodoItem is one open (pending or active) todo row as the workspace-
// scoped read returns it, carrying the session it belongs to so cross-session
// surfaces can attribute items.
type OpenTodoItem struct {
	SessionID string
	ItemKey   string
	ItemText  string
	Status    TodoStatus
	Reason    string
	UpdatedAt time.Time
}

// ValidateTodoItems checks a full item list before any write touches state
// (adopt-assistant-ui-elements spec agent-todos): every item needs a non-blank
// key and text, a status among the four allowed values, and keys must be
// unique — a duplicate key could not satisfy the table's UNIQUE (session_id,
// item_key). A wrapped domain.ErrInvalid names the first fault.
func ValidateTodoItems(items []TodoItem) error {
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		if strings.TrimSpace(item.ItemKey) == "" {
			return fmt.Errorf("%w: item %d: key is required", domain.ErrInvalid, i+1)
		}
		if strings.TrimSpace(item.ItemText) == "" {
			return fmt.Errorf("%w: item %q: text is required", domain.ErrInvalid, item.ItemKey)
		}
		if !ValidTodoStatus(item.Status) {
			return fmt.Errorf("%w: item %q: status must be one of pending, active, done, failed, got %q", domain.ErrInvalid, item.ItemKey, item.Status)
		}
		if _, dup := seen[item.ItemKey]; dup {
			return fmt.Errorf("%w: duplicate item key %q", domain.ErrInvalid, item.ItemKey)
		}
		seen[item.ItemKey] = struct{}{}
	}
	return nil
}

// TodoStore manages the durable per-session todo lists backing the todo_write
// and todo_read tools (adopt-assistant-ui-elements D5): one agent_todos row
// per (session, item), the transcript keeping the revision history while this
// store holds the current state. Every read and write is workspace-scoped —
// no query runs without the workspace partition — and rows cascade with their
// workspace or agent.
//
// Session identity is the text session_id (logical reference to
// agent_sessions.session_id, the session_events house convention).
type TodoStore interface {
	// GetBySession returns the session's current list with its revision.
	// Absence is a normal state, never an error: a session with no stored
	// todos reads back as an empty list with revision 0 (the returned list is
	// never nil). Items are ordered by item key — the stable identity rewrites
	// restyle in place, so key order is the deterministic read order for both
	// adapters.
	GetBySession(ctx context.Context, workspaceID, agentID, sessionID string) (*TodoList, error)
	// Replace atomically rewrites the session's list with the given items
	// (adopt-assistant-ui-elements spec agent-todos): rows keyed by existing
	// item keys update in place (text, status, reason, updated_at — the
	// created_at birth stamp survives so stable keys restyle rather than
	// remount), keys absent from the call are deleted, new keys are inserted,
	// and the session's revision increments. Replace returns items in the
	// same key order GetBySession reads. The whole rewrite is ONE
	// transaction — no partial application survives a failure — and the list
	// is validated BEFORE any write, so a rejected list never mutates stored
	// state (wrapped domain.ErrInvalid naming the fault). An empty item list
	// clears the session's plan (every row deleted); with no rows left the
	// visible revision counter restarts from zero on the next write — the
	// transcript remains the revision history. Returns the stored state as it
	// now reads: the items in storage order plus the session's current
	// revision.
	Replace(ctx context.Context, list *TodoList) (*TodoList, error)
	// OpenItems returns the agent's open (pending or active) todo items
	// across all its sessions in the workspace, most recently touched first —
	// the indexed query the open-items surfaces ride (adopt-assistant-ui-
	// elements spec agent-todos: "a query, not a scan"). Done and failed
	// items never appear. An empty result is an empty slice, not nil.
	OpenItems(ctx context.Context, workspaceID, agentID string) ([]OpenTodoItem, error)
}
