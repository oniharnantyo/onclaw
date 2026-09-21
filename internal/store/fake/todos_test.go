package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// seedTodoStore creates one workspace, a provider, and one agent so todo
// rows have their FKs satisfied.
func seedTodoStore(t *testing.T, s store.Store) (wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "fake-todos", Name: "Fake Todos"}
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

// TestFakeTodoStore_ReplaceSemantics pins the rewrite contract
// (adopt-assistant-ui-elements spec agent-todos): kept keys restyle in
// place, dropped keys vanish, new keys insert, and the revision increments —
// atomically, with no partial application.
func TestFakeTodoStore_ReplaceSemantics(t *testing.T) {
	s := New()
	ctx := context.Background()
	wsID, agentID := seedTodoStore(t, s)
	todos := s.Todos()
	const session = "sess_rw"

	stored, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		{ItemKey: "c", ItemText: "Do C", Status: store.TodoStatusPending},
		{ItemKey: "a", ItemText: "Do A", Status: store.TodoStatusPending},
		{ItemKey: "b", ItemText: "Do B", Status: store.TodoStatusPending},
	}})
	if err != nil {
		t.Fatalf("seed replace: %v", err)
	}
	if stored.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", stored.Revision)
	}
	// Key order is the documented read order regardless of call order.
	if stored.Items[0].ItemKey != "a" || stored.Items[1].ItemKey != "b" || stored.Items[2].ItemKey != "c" {
		t.Fatalf("expected key-ordered items, got %v", stored.Items)
	}

	// The spec's atomic-replace scenario: A done, B failed with reason, C
	// dropped, D added.
	stored, err = todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		{ItemKey: "a", ItemText: "Do A", Status: store.TodoStatusDone},
		{ItemKey: "b", ItemText: "Do B", Status: store.TodoStatusFailed, Reason: "blocked"},
		{ItemKey: "d", ItemText: "Do D", Status: store.TodoStatusActive},
	}})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if stored.Revision != 2 || len(stored.Items) != 3 {
		t.Fatalf("expected revision 2 with 3 items, got revision %d with %d items", stored.Revision, len(stored.Items))
	}
	byKey := map[string]store.TodoItem{}
	for _, item := range stored.Items {
		byKey[item.ItemKey] = item
	}
	if _, gone := byKey["c"]; gone {
		t.Fatal("expected dropped key c to be deleted")
	}
	if byKey["a"].Status != store.TodoStatusDone || byKey["b"].Reason != "blocked" || byKey["d"].Status != store.TodoStatusActive {
		t.Fatalf("unexpected restyled items: %+v", stored.Items)
	}

	// Read-back parity: the stored state matches the echoed state.
	read, err := todos.GetBySession(ctx, wsID, agentID, session)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read.Revision != 2 || len(read.Items) != 3 {
		t.Fatalf("read-back mismatch: revision %d with %d items", read.Revision, len(read.Items))
	}
}

// TestFakeTodoStore_ValidationNoMutation pins the rejection contract: an
// invalid list is refused with domain.ErrInvalid and the stored state is
// untouched — revision included.
func TestFakeTodoStore_ValidationNoMutation(t *testing.T) {
	s := New()
	ctx := context.Background()
	wsID, agentID := seedTodoStore(t, s)
	todos := s.Todos()
	const session = "sess_vm"

	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: session, Items: []store.TodoItem{
		{ItemKey: "a", ItemText: "Do A", Status: store.TodoStatusActive},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cases := []store.TodoList{
		{Items: []store.TodoItem{{ItemKey: "x", ItemText: "X", Status: store.TodoStatus("archived")}}},
		{Items: []store.TodoItem{{ItemKey: "x", ItemText: "  ", Status: store.TodoStatusPending}}},
		{Items: []store.TodoItem{{ItemKey: " ", ItemText: "X", Status: store.TodoStatusPending}}},
		{Items: []store.TodoItem{
			{ItemKey: "x", ItemText: "X", Status: store.TodoStatusPending},
			{ItemKey: "x", ItemText: "Dup", Status: store.TodoStatusDone},
		}},
	}
	for i, list := range cases {
		list.WorkspaceID, list.AgentID, list.SessionID = wsID, agentID, session
		if _, err := todos.Replace(ctx, &list); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("case %d: expected ErrInvalid, got %v", i, err)
		}
	}

	read, err := todos.GetBySession(ctx, wsID, agentID, session)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read.Revision != 1 || len(read.Items) != 1 || read.Items[0].Status != store.TodoStatusActive {
		t.Fatalf("expected untouched state (revision 1, one active item), got revision %d with %d items", read.Revision, len(read.Items))
	}
}

// TestFakeTodoStore_ScopingAndFKParity covers tenant isolation, session
// scoping, the open-items query, FK-shaped rejections, and the agent-delete
// cascade.
func TestFakeTodoStore_ScopingAndFKParity(t *testing.T) {
	s := New()
	ctx := context.Background()
	wsID, agentID := seedTodoStore(t, s)
	todos := s.Todos()

	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_1", Items: []store.TodoItem{
		{ItemKey: "a", ItemText: "A", Status: store.TodoStatusPending},
		{ItemKey: "b", ItemText: "B", Status: store.TodoStatusDone},
	}}); err != nil {
		t.Fatalf("seed sess_1: %v", err)
	}
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_2", Items: []store.TodoItem{
		{ItemKey: "c", ItemText: "C", Status: store.TodoStatusActive},
	}}); err != nil {
		t.Fatalf("seed sess_2: %v", err)
	}

	// Session scoping: sess_1 never sees sess_2's rows.
	read, err := todos.GetBySession(ctx, wsID, agentID, "sess_1")
	if err != nil {
		t.Fatalf("get sess_1: %v", err)
	}
	if len(read.Items) != 2 || read.Items[0].ItemKey != "a" || read.Items[1].ItemKey != "b" {
		t.Fatalf("expected a and b in sess_1, got %+v", read.Items)
	}

	// Open items: done rows excluded, other sessions included.
	open, err := todos.OpenItems(ctx, wsID, agentID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 open items across sessions, got %d", len(open))
	}

	// FK parity: unknown workspace and unknown agent both fail the write.
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: "nope", AgentID: agentID, SessionID: "s", Items: []store.TodoItem{
		{ItemKey: "x", ItemText: "X", Status: store.TodoStatusPending},
	}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}
	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: "nope", SessionID: "s", Items: []store.TodoItem{
		{ItemKey: "x", ItemText: "X", Status: store.TodoStatusPending},
	}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}

	// Empty scope inputs are invalid, never empty results.
	if _, err := todos.GetBySession(ctx, "", agentID, "sess_1"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty workspace, got %v", err)
	}

	// Cascade: the agent's rows die with the agent.
	if err := s.Agents().Delete(ctx, wsID, agentID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	read1, err := todos.GetBySession(ctx, wsID, agentID, "sess_1")
	if err != nil {
		t.Fatalf("get after cascade: %v", err)
	}
	read2, err := todos.GetBySession(ctx, wsID, agentID, "sess_2")
	if err != nil {
		t.Fatalf("get after cascade: %v", err)
	}
	if len(read1.Items) != 0 || len(read2.Items) != 0 {
		t.Fatalf("expected cascade to clear the agent's todos, got %d and %d rows", len(read1.Items), len(read2.Items))
	}
}

// TestFakeTodoStore_EmptyListClears pins the clear-the-plan rewrite: every
// row deletes and the returned state is empty.
func TestFakeTodoStore_EmptyListClears(t *testing.T) {
	s := New()
	ctx := context.Background()
	wsID, agentID := seedTodoStore(t, s)
	todos := s.Todos()

	if _, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess", Items: []store.TodoItem{
		{ItemKey: "a", ItemText: "A", Status: store.TodoStatusPending},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stored, err := todos.Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess"})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(stored.Items) != 0 {
		t.Fatalf("expected no rows after clear, got %d", len(stored.Items))
	}
	read, err := todos.GetBySession(ctx, wsID, agentID, "sess")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read.Revision != 0 || len(read.Items) != 0 {
		t.Fatalf("expected empty list with revision 0 after clear, got revision %d with %d items", read.Revision, len(read.Items))
	}
}
