package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// TodoStore implementation (adopt-assistant-ui-elements D5). Mirrors the
// postgres adapter one for one: rows keyed by the (session_id, item_key)
// identity the SQL UNIQUE index pins, workspace/agent existence checked like
// the FKs, the whole Replace applied under one lock (the fake's transaction
// snapshot isolation via WithTx also covers it — the sub-port mutates only
// after every validation passes, so a rejected list never mutates state).
// -------------------------------------------------------------------------

// fakeTodoRow is one agent_todos row as the fake stores it. Value type: all
// fields are value types, so map copies clone rows for free.
type fakeTodoRow struct {
	workspaceID string
	agentID     string
	sessionID   string
	item        store.TodoItem
	revision    int64
	createdAt   time.Time
	updatedAt   time.Time
}

// todoRowKey is the row identity the SQL UNIQUE (session_id, item_key) pins.
func todoRowKey(sessionID, itemKey string) string {
	return sessionID + ":" + itemKey
}

type todoStore struct {
	s *fakeStore
}

// GetBySession returns the session's current list with its revision; a
// session with no stored todos reads back as an empty list with revision 0.
func (ts *todoStore) GetBySession(_ context.Context, workspaceID, agentID, sessionID string) (*store.TodoList, error) {
	if workspaceID == "" || agentID == "" || sessionID == "" {
		return nil, fmt.Errorf("%w: workspace, agent, and session are required", domain.ErrInvalid)
	}

	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()

	return ts.s.todoListLocked(workspaceID, agentID, sessionID), nil
}

// Replace validates the full list first, then rewrites the session's rows in
// one locked pass: upsert by key, delete vanished keys, bump the revision.
func (ts *todoStore) Replace(_ context.Context, list *store.TodoList) (*store.TodoList, error) {
	if list == nil {
		return nil, domain.ErrInvalid
	}
	if list.WorkspaceID == "" || list.AgentID == "" || list.SessionID == "" {
		return nil, fmt.Errorf("%w: workspace, agent, and session are required", domain.ErrInvalid)
	}
	// Validation BEFORE any write: a rejected list never mutates state
	// (adopt-assistant-ui-elements spec agent-todos).
	if err := store.ValidateTodoItems(list.Items); err != nil {
		return nil, err
	}

	ts.s.mu.Lock()
	defer ts.s.mu.Unlock()

	// FK parity: the workspace and the agent (within it) must exist.
	if _, exists := ts.s.workspaces[list.WorkspaceID]; !exists {
		return nil, fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	agent, exists := ts.s.agents[list.AgentID]
	if !exists || agent.WorkspaceID != list.WorkspaceID {
		return nil, fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
	}

	// The session's rows and its current revision (MAX over them).
	sessionRows := make(map[string]*fakeTodoRow)
	var maxRevision int64
	for key, row := range ts.s.agentTodos {
		if row.workspaceID == list.WorkspaceID && row.agentID == list.AgentID && row.sessionID == list.SessionID {
			sessionRows[key] = row
			if row.revision > maxRevision {
				maxRevision = row.revision
			}
		}
	}
	nextRevision := maxRevision + 1

	now := time.Now().UTC()
	want := make(map[string]store.TodoItem, len(list.Items))
	for _, item := range list.Items {
		want[item.ItemKey] = item
	}

	// Upsert by key: existing keys restyle in place (created_at birth stamp
	// survives), keys absent from the call are deleted.
	for key, row := range sessionRows {
		item, keep := want[key]
		if !keep {
			delete(ts.s.agentTodos, key)
			continue
		}
		row.item = item
		row.revision = nextRevision
		row.updatedAt = now
	}
	// New keys are inserted.
	for key, item := range want {
		if _, exists := sessionRows[key]; exists {
			continue
		}
		ts.s.agentTodos[key] = &fakeTodoRow{
			workspaceID: list.WorkspaceID,
			agentID:     list.AgentID,
			sessionID:   list.SessionID,
			item:        item,
			revision:    nextRevision,
			createdAt:   now,
			updatedAt:   now,
		}
	}

	return ts.s.todoListLocked(list.WorkspaceID, list.AgentID, list.SessionID), nil
}

// OpenItems returns the agent's open (pending or active) items across its
// sessions in the workspace, most recently touched first.
func (ts *todoStore) OpenItems(_ context.Context, workspaceID, agentID string) ([]store.OpenTodoItem, error) {
	if workspaceID == "" || agentID == "" {
		return nil, fmt.Errorf("%w: workspace and agent are required", domain.ErrInvalid)
	}

	ts.s.mu.RLock()
	defer ts.s.mu.RUnlock()

	open := make([]store.OpenTodoItem, 0)
	for _, row := range ts.s.agentTodos {
		if row.workspaceID != workspaceID || row.agentID != agentID {
			continue
		}
		if row.item.Status != store.TodoStatusPending && row.item.Status != store.TodoStatusActive {
			continue
		}
		open = append(open, store.OpenTodoItem{
			SessionID: row.sessionID,
			ItemKey:   row.item.ItemKey,
			ItemText:  row.item.ItemText,
			Status:    row.item.Status,
			Reason:    row.item.Reason,
			UpdatedAt: row.updatedAt,
		})
	}
	sort.Slice(open, func(i, j int) bool {
		if !open[i].UpdatedAt.Equal(open[j].UpdatedAt) {
			return open[i].UpdatedAt.After(open[j].UpdatedAt)
		}
		if open[i].SessionID != open[j].SessionID {
			return open[i].SessionID < open[j].SessionID
		}
		return open[i].ItemKey < open[j].ItemKey
	})
	return open, nil
}

// todoListLocked assembles the session's current list: rows in key order,
// revision = MAX over the rows (0 when the session has none). Callers hold
// the store lock.
func (s *fakeStore) todoListLocked(workspaceID, agentID, sessionID string) *store.TodoList {
	list := &store.TodoList{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		SessionID:   sessionID,
		Items:       []store.TodoItem{},
	}
	for _, row := range s.agentTodos {
		if row.workspaceID != workspaceID || row.agentID != agentID || row.sessionID != sessionID {
			continue
		}
		if row.revision > list.Revision {
			list.Revision = row.revision
		}
		list.Items = append(list.Items, row.item)
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].ItemKey < list.Items[j].ItemKey
	})
	return list
}

// todoRowsBelongingToAgentLocked reports the map keys of every todo row of
// one agent (the ON DELETE CASCADE sweep). Callers hold the store lock.
func todoRowsBelongingToAgentLocked(rows map[string]*fakeTodoRow, workspaceID, agentID string) []string {
	keys := make([]string, 0)
	for key, row := range rows {
		if row.workspaceID == workspaceID && row.agentID == agentID {
			keys = append(keys, key)
		}
	}
	return keys
}
