package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// seedTodoTools creates a workspace and agent in the real fake store and
// returns the two todo tools bound to one session of that run identity — the
// same wiring the tool registry performs per construction.
func seedTodoTools(t *testing.T) (write, read tool.BaseTool, wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	st := fake.New()
	ws := &domain.Workspace{Slug: "todo-tools", Name: "Todo Tools"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	w, err := NewTodoWrite(st.Todos(), ws.ID, a.ID, "sess_main")
	if err != nil {
		t.Fatalf("new todo_write: %v", err)
	}
	r, err := NewTodoRead(st.Todos(), ws.ID, a.ID, "sess_main")
	if err != nil {
		t.Fatalf("new todo_read: %v", err)
	}
	return w, r, ws.ID, a.ID
}

// invoke runs one tool call through the InvokableTool surface the
// constructors return as BaseTool.
func invoke(t *testing.T, base tool.BaseTool, args string) (string, error) {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	if !ok {
		t.Fatalf("tool %T does not implement InvokableTool", base)
	}
	return invokable.InvokableRun(context.Background(), args)
}

// decodeTodoResult decodes the tools' shared {items, revision} echo.
func decodeTodoResult(t *testing.T, out string) store.TodoList {
	t.Helper()
	var decoded struct {
		Items []store.TodoItem `json:"items"`
		Rev   int64            `json:"revision"`
	}
	// Status arrives as a plain string; decode into the store shape by hand.
	var raw struct {
		Items []struct {
			Key    string `json:"key"`
			Text   string `json:"text"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"items"`
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	decoded.Rev = raw.Revision
	for _, item := range raw.Items {
		decoded.Items = append(decoded.Items, store.TodoItem{
			ItemKey:  item.Key,
			ItemText: item.Text,
			Status:   store.TodoStatus(item.Status),
			Reason:   item.Reason,
		})
	}
	return store.TodoList{Items: decoded.Items, Revision: decoded.Rev}
}

// TestTodoTool_RoundTrip exercises the full write → read → rewrite loop
// (adopt-assistant-ui-elements tasks 5.7): the rewrite with a dropped key
// bumps the revision and the dropped key is gone.
func TestTodoTool_RoundTrip(t *testing.T) {
	write, read, wsID, agentID := seedTodoTools(t)

	// 1. Write the seed plan.
	out, err := invoke(t, write, `{"items":[
		{"key":"a","text":"Do A","status":"pending"},
		{"key":"b","text":"Do B","status":"pending"},
		{"key":"c","text":"Do C","status":"pending"}
	]}`)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	stored := decodeTodoResult(t, out)
	if stored.Revision != 1 || len(stored.Items) != 3 {
		t.Fatalf("expected revision 1 with 3 items, got revision %d with %d items", stored.Revision, len(stored.Items))
	}

	// 2. Read sees the same state.
	out, err = invoke(t, read, `{}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := decodeTodoResult(t, out); got.Revision != 1 || len(got.Items) != 3 {
		t.Fatalf("read mismatch: revision %d with %d items", got.Revision, len(got.Items))
	}

	// 3. Rewrite with a dropped key: c dropped, a done, b failed+reason,
	// d added.
	out, err = invoke(t, write, `{"items":[
		{"key":"a","text":"Do A","status":"done"},
		{"key":"b","text":"Do B","status":"failed","reason":"blocked on creds"},
		{"key":"d","text":"Do D","status":"active"}
	]}`)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	stored = decodeTodoResult(t, out)
	if stored.Revision != 2 {
		t.Fatalf("expected revision bump to 2, got %d", stored.Revision)
	}
	keys := map[string]bool{}
	for _, item := range stored.Items {
		keys[item.ItemKey] = true
	}
	if keys["c"] {
		t.Fatal("expected dropped key c to be gone after rewrite")
	}
	if !keys["a"] || !keys["b"] || !keys["d"] {
		t.Fatalf("expected a, b, d after rewrite, got %v", stored.Items)
	}

	// 4. todo_read (and the store) agree: c is gone, revision is 2.
	out, err = invoke(t, read, `{}`)
	if err != nil {
		t.Fatalf("read after rewrite: %v", err)
	}
	final := decodeTodoResult(t, out)
	if final.Revision != 2 || len(final.Items) != 3 {
		t.Fatalf("expected revision 2 with 3 items, got revision %d with %d items", final.Revision, len(final.Items))
	}
	for _, item := range final.Items {
		if item.ItemKey == "c" {
			t.Fatal("dropped key c survived in the store")
		}
	}
	if wsID == "" || agentID == "" {
		t.Fatal("fixture identity must be set")
	}
}

// TestTodoTool_ValidationRejectsAndLeavesState pins the tool-level rejection
// contract: unknown status, missing key/text, duplicate keys, unknown fields
// — every rejection is an error naming the fault, and the stored list is
// byte-for-byte unchanged.
func TestTodoTool_ValidationRejectsAndLeavesState(t *testing.T) {
	write, read, _, _ := seedTodoTools(t)

	if _, err := invoke(t, write, `{"items":[{"key":"a","text":"Do A","status":"active"}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rejections := []string{
		`{"items":[{"key":"a","text":"Do A","status":"finished"}]}`,                                       // unknown status
		`{"items":[{"key":"a","text":"Do A","status":"DONE"}]}`,                                           // case-sensitive enum
		`{"items":[{"key":"","text":"No key","status":"done"}]}`,                                          // missing key
		`{"items":[{"key":"x","text":"","status":"done"}]}`,                                               // missing text
		`{"items":[{"key":"a","text":"Dup","status":"done"},{"key":"a","text":"A2","status":"pending"}]}`, // duplicate key
		`{"items":[],"note":"extra field"}`,                                                               // unknown top-level field
		`{"items":"three"}`,                                                                               // wrong shape
	}
	for i, args := range rejections {
		if _, err := invoke(t, write, args); err == nil {
			t.Fatalf("rejection %d: expected an error for %s", i, args)
		}
	}

	out, err := invoke(t, read, `{}`)
	if err != nil {
		t.Fatalf("read after rejections: %v", err)
	}
	state := decodeTodoResult(t, out)
	if state.Revision != 1 || len(state.Items) != 1 || state.Items[0].Status != store.TodoStatusActive {
		t.Fatalf("expected untouched stored state, got revision %d with %+v", state.Revision, state.Items)
	}
}

// TestTodoTool_SessionIdentityIsStructural pins the scoping contract: a tool
// bound to one session can never read or write another session's plan.
func TestTodoTool_SessionIdentityIsStructural(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "todo-scoping", Name: "Todo Scoping"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	sessionA, err := NewTodoWrite(st.Todos(), ws.ID, a.ID, "sess_a")
	if err != nil {
		t.Fatalf("new session A tool: %v", err)
	}
	sessionB, err := NewTodoWrite(st.Todos(), ws.ID, a.ID, "sess_b")
	if err != nil {
		t.Fatalf("new session B tool: %v", err)
	}

	if _, err := invoke(t, sessionA, `{"items":[{"key":"a","text":"A","status":"pending"}]}`); err != nil {
		t.Fatalf("write session A: %v", err)
	}
	if _, err := invoke(t, sessionB, `{"items":[{"key":"b","text":"B","status":"pending"}]}`); err != nil {
		t.Fatalf("write session B: %v", err)
	}

	// The full-list rewrite through session B must not touch session A's rows.
	if _, err := invoke(t, sessionB, `{"items":[{"key":"b","text":"B","status":"done"}]}`); err != nil {
		t.Fatalf("rewrite session B: %v", err)
	}
	listA, err := st.Todos().GetBySession(ctx, ws.ID, a.ID, "sess_a")
	if err != nil {
		t.Fatalf("get session A: %v", err)
	}
	if len(listA.Items) != 1 || listA.Items[0].ItemKey != "a" || listA.Items[0].Status != store.TodoStatusPending {
		t.Fatalf("session A's plan leaked or mutated: %+v", listA.Items)
	}
}

// TestTodoTool_EmptyListClears pins the clear semantics at the tool level:
// an empty item list is a valid rewrite that empties the plan.
func TestTodoTool_EmptyListClears(t *testing.T) {
	write, read, _, _ := seedTodoTools(t)

	if _, err := invoke(t, write, `{"items":[{"key":"a","text":"A","status":"pending"}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, err := invoke(t, write, `{"items":[]}`)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	state := decodeTodoResult(t, out)
	if len(state.Items) != 0 {
		t.Fatalf("expected an empty plan after clear, got %+v", state.Items)
	}
	out, err = invoke(t, read, `{}`)
	if err != nil {
		t.Fatalf("read after clear: %v", err)
	}
	if got := decodeTodoResult(t, out); len(got.Items) != 0 {
		t.Fatalf("expected the store to read back empty, got %+v", got.Items)
	}
}
