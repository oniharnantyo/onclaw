//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// seedTodoFixtures creates one workspace with one agent so todo rows have
// their two hard FKs satisfied.
func seedTodoFixtures(t *testing.T, s store.Store) (wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "pg-todos", Name: "PG Todos"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, a.ID
}

func todoItem(key, text, status string) store.TodoItem {
	return store.TodoItem{ItemKey: key, ItemText: text, Status: store.TodoStatus(status)}
}

func TestIntegration_TodoStore_RewriteReplacesAtomically(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedTodoFixtures(t, s)
	todos := s.Todos()
	const session = "sess_todo_1"

	// Seed the session's list: A, B, C.
	stored, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		todoItem("a", "Do A", "pending"),
		todoItem("b", "Do B", "pending"),
		todoItem("c", "Do C", "pending"),
	}})
	if err != nil {
		t.Fatalf("seed replace: %v", err)
	}
	if stored.Revision != 1 || len(stored.Items) != 3 {
		t.Fatalf("expected revision 1 with 3 items, got revision %d with %d items", stored.Revision, len(stored.Items))
	}

	// Rewrite: A done, B failed with reason, C dropped, D added — the spec's
	// atomic-replace scenario.
	rewrite := &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		todoItem("a", "Do A", "done"),
		{ItemKey: "b", ItemText: "Do B", Status: store.TodoStatusFailed, Reason: "blocked on creds"},
		todoItem("d", "Do D", "active"),
	}}
	stored, err = todos.Replace(ctx, rewrite)
	if err != nil {
		t.Fatalf("rewrite replace: %v", err)
	}
	if stored.Revision != 2 {
		t.Fatalf("expected revision 2 after rewrite, got %d", stored.Revision)
	}
	if len(stored.Items) != 3 {
		t.Fatalf("expected 3 items after rewrite, got %d", len(stored.Items))
	}
	byKey := map[string]store.TodoItem{}
	for _, item := range stored.Items {
		byKey[item.ItemKey] = item
	}
	if _, exists := byKey["c"]; exists {
		t.Fatal("expected key c to be deleted by the rewrite")
	}
	if got := byKey["a"]; got.Status != store.TodoStatusDone {
		t.Fatalf("expected a to restyle to done, got %s", got.Status)
	}
	if got := byKey["b"]; got.Reason != "blocked on creds" {
		t.Fatalf("expected b to carry its reason, got %q", got.Reason)
	}
	if got := byKey["d"]; got.Status != store.TodoStatusActive {
		t.Fatalf("expected d inserted active, got %s", got.Status)
	}

	// The read path sees the same state (plan survives and reloads).
	read, err := todos.GetBySession(ctx, wsID, agentID, session)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read.Revision != 2 || len(read.Items) != 3 {
		t.Fatalf("expected revision 2 with 3 items on read, got revision %d with %d items", read.Revision, len(read.Items))
	}

	// A brand-new session reads back empty with revision 0 — absence is a
	// normal state, never an error.
	empty, err := todos.GetBySession(ctx, wsID, agentID, "sess_unknown")
	if err != nil {
		t.Fatalf("get unknown session: %v", err)
	}
	if empty.Revision != 0 || len(empty.Items) != 0 {
		t.Fatalf("expected empty list with revision 0, got revision %d with %d items", empty.Revision, len(empty.Items))
	}
}

func TestIntegration_TodoStore_InvalidListRejectedWithoutMutation(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedTodoFixtures(t, s)
	todos := s.Todos()
	const session = "sess_todo_2"

	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		todoItem("a", "Do A", "pending"),
	}}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	// Unknown status, missing text, duplicate key — every rejection leaves
	// the stored list unchanged.
	invalid := []store.TodoList{
		{Items: []store.TodoItem{todoItem("a", "Do A", "finished")}},
		{Items: []store.TodoItem{todoItem("a", "", "done")}},
		{Items: []store.TodoItem{todoItem("", "No key", "done")}},
		{Items: []store.TodoItem{todoItem("a", "Do A", "done"), todoItem("a", "Dup", "pending")}},
	}
	for i, list := range invalid {
		list.WorkspaceID, list.AgentID, list.SessionID = wsID, agentID, session
		if _, err := todos.Replace(ctx, &list); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid list %d: expected ErrInvalid, got %v", i, err)
		}
	}

	read, err := todos.GetBySession(ctx, wsID, agentID, session)
	if err != nil {
		t.Fatalf("get after rejections: %v", err)
	}
	if read.Revision != 1 || len(read.Items) != 1 || read.Items[0].Status != store.TodoStatusPending {
		t.Fatalf("expected the seeded list untouched (revision 1, one pending item), got revision %d with %d items", read.Revision, len(read.Items))
	}
}

func TestIntegration_TodoStore_ScopingAndOpenItems(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedTodoFixtures(t, s)
	todos := s.Todos()

	// Two sessions of the same agent, plus a second agent's rows: scope and
	// the open-items query partition on all three coordinates.
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_a", Items: []store.TodoItem{
		todoItem("a1", "Open A1", "pending"),
		todoItem("a2", "Busy A2", "active"),
		todoItem("a3", "Closed A3", "done"),
	}}); err != nil {
		t.Fatalf("seed session a: %v", err)
	}
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_b", Items: []store.TodoItem{
		todoItem("b1", "Open B1", "active"),
	}}); err != nil {
		t.Fatalf("seed session b: %v", err)
	}

	// Another agent in the same workspace never leaks into the query.
	p := &domain.ProviderConfig{WorkspaceID: wsID, Type: "openai", Name: "P2", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider 2: %v", err)
	}
	other := &domain.Agent{WorkspaceID: wsID, Slug: "beacon", Name: "Beacon", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, other); err != nil {
		t.Fatalf("create agent 2: %v", err)
	}
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: other.ID, SessionID: "sess_x", Items: []store.TodoItem{
		todoItem("x1", "Foreign X1", "pending"),
	}}); err != nil {
		t.Fatalf("seed other agent: %v", err)
	}

	open, err := todos.OpenItems(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("open items: %v", err)
	}
	if len(open) != 3 {
		t.Fatalf("expected 3 open items (done excluded, foreign agent excluded), got %d", len(open))
	}
	for _, item := range open {
		if item.Status != store.TodoStatusPending && item.Status != store.TodoStatusActive {
			t.Fatalf("open items must only carry pending/active, got %s", item.Status)
		}
		if item.SessionID != "sess_a" && item.SessionID != "sess_b" {
			t.Fatalf("open item from unexpected session %q", item.SessionID)
		}
	}

	// Session scoping: a session never reads another session's items.
	read, err := todos.GetBySession(ctx, wsID, agentID, "sess_b")
	if err != nil {
		t.Fatalf("get session b: %v", err)
	}
	if len(read.Items) != 1 || read.Items[0].ItemKey != "b1" {
		t.Fatalf("expected only b1 in session b, got %+v", read.Items)
	}

	// The workspace gate: a foreign workspace's agent id reads empty/invalid.
	if _, err := todos.OpenItems(ctx, "", agentID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty workspace scope, got %v", err)
	}

	// FK parity: an unknown agent fails the replace with ErrNotFound.
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: "00000000-0000-0000-0000-000000000000", SessionID: "sess_y", Items: []store.TodoItem{
		todoItem("y1", "Ghost Y1", "pending"),
	}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	// Cascade: deleting the agent takes its todo rows with it.
	if err := s.Agents().Delete(ctx, wsID, other.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	readX, err := todos.GetBySession(ctx, wsID, other.ID, "sess_x")
	if err != nil {
		t.Fatalf("get after cascade: %v", err)
	}
	if len(readX.Items) != 0 {
		t.Fatalf("expected the agent's todos to cascade-delete, got %d rows", len(readX.Items))
	}
}

func TestIntegration_TodoStore_EmptyListClears(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedTodoFixtures(t, s)
	todos := s.Todos()
	const session = "sess_todo_3"

	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		todoItem("a", "Do A", "pending"),
	}}); err != nil {
		t.Fatalf("seed replace: %v", err)
	}

	stored, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session})
	if err != nil {
		t.Fatalf("clear replace: %v", err)
	}
	if len(stored.Items) != 0 {
		t.Fatalf("expected the clear to leave no rows, got %d", len(stored.Items))
	}
	open, err := todos.OpenItems(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("open after clear: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("expected no open items after clear, got %d", len(open))
	}
}
