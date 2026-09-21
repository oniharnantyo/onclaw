package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func todoItemOf(key, text, status string) store.TodoItem {
	return store.TodoItem{ItemKey: key, ItemText: text, Status: store.TodoStatus(status)}
}

// seedTodoRunner creates a fake store with one workspace and one agent whose
// allowlist exposes the todo tools, plus a runner wired with that store.
func seedTodoRunner(t *testing.T) (runner *Runner, st store.Store, wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	st = fake.New()
	ws := &domain.Workspace{Slug: "todo-runner", Name: "Todo Runner"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		Tools:       []string{tools.NameTodoWrite, tools.NameTodoRead},
	}
	if err := st.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	runner = NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, st.GatewayLinks(),
		nil, nil, nil, []byte("k"), "/tmp/onclaw",
		WithTodoStore(st.Todos()),
	)
	return runner, st, ws.ID, a.ID
}

// TestRunner_TodoSummaryInjection pins the D6 contract: the one-line
// open-items summary composes only for agents exposing the todo tools, names
// the open counts and the revision, and disappears when nothing is open, the
// store is unwired, the agent lacks the tools, or the per-turn override
// strips them.
func TestRunner_TodoSummaryInjection(t *testing.T) {
	runner, st, wsID, agentID := seedTodoRunner(t)
	ctx := context.Background()

	if _, err := st.Todos().Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_1", Items: []store.TodoItem{
		todoItemOf("a", "A", "pending"),
		todoItemOf("b", "B", "pending"),
		todoItemOf("c", "C", "pending"),
		todoItemOf("d", "D", "active"),
		todoItemOf("e", "E", "done"),
	}}); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	agent := &domain.Agent{WorkspaceID: wsID, Slug: "atlas", Name: "Atlas", Tools: []string{tools.NameTodoWrite}}
	req := ExecRequest{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_1", UserID: "u"}

	got := runner.composeTodoSummary(ctx, req, agent)
	if !strings.Contains(got, "3 pending") || !strings.Contains(got, "1 active") || !strings.Contains(got, "revision 1") {
		t.Fatalf("expected counts and revision in the summary, got %q", got)
	}
	if !strings.Contains(got, "todo_read") {
		t.Fatalf("expected the summary to point at todo_read, got %q", got)
	}

	// An agent without the tools composes nothing — present-only.
	unexposed := &domain.Agent{WorkspaceID: wsID, Slug: "atlas", Name: "Atlas"}
	if got := runner.composeTodoSummary(ctx, req, unexposed); got != "" {
		t.Fatalf("unexposed agent must compose no summary, got %q", got)
	}

	// The per-turn override replaces the allowlist and can strip the tools.
	override := req
	override.AllowedTools = []string{"web.search"}
	if got := runner.composeTodoSummary(ctx, override, agent); got != "" {
		t.Fatalf("override without todo tools must compose no summary, got %q", got)
	}

	// Nothing open — done/failed only — composes nothing.
	if _, err := st.Todos().Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_1", Items: []store.TodoItem{
		todoItemOf("a", "A", "done"),
		todoItemOf("b", "B", "failed"),
	}}); err != nil {
		t.Fatalf("close all: %v", err)
	}
	if got := runner.composeTodoSummary(ctx, req, agent); got != "" {
		t.Fatalf("a fully closed plan must compose no summary, got %q", got)
	}

	// An unwired runner composes nothing.
	unwired := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, st.GatewayLinks(),
		nil, nil, nil, []byte("k"), "/tmp/onclaw")
	if _, err := st.Todos().Replace(ctx, &store.TodoList{WorkspaceID: wsID, AgentID: agentID, SessionID: "sess_1", Items: []store.TodoItem{
		todoItemOf("a", "A", "pending"),
	}}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := unwired.composeTodoSummary(ctx, req, agent); got != "" {
		t.Fatalf("unwired runner must compose no summary, got %q", got)
	}
}

// TestRunner_TodoRegistryAndCatalog pins the wiring seams: WithTodoTools
// registers both todo tools (resolvable with a ToolContext), unwired
// registries surface neither, and both carry complete toggleable catalog
// entries. The ui.* echo tools are gone (markdown-card-elements D1) — cards
// ride tagged code fences taught by the injected base prompt.
func TestRunner_TodoRegistryAndCatalog(t *testing.T) {
	st := fake.New()
	reg := NewDefaultToolRegistry(st.Memories(), WithTodoTools(st.Todos()))

	newNames := []string{
		tools.NameTodoWrite, tools.NameTodoRead,
	}
	for _, name := range newNames {
		ctor, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("expected %s registered", name)
		}
		if _, err := ctor(ToolContext{WorkspaceID: "ws", AgentID: "ag", SessionID: "sess"}); err != nil {
			t.Fatalf("construct %s: %v", name, err)
		}
	}

	// Unwired registries surface neither todo tool — the schedule-tool
	// precedent: absent, not broken.
	bare := NewDefaultToolRegistry(st.Memories())
	if _, ok := bare.Lookup(tools.NameTodoWrite); ok {
		t.Fatal("todo_write must not register without WithTodoTools")
	}
	if _, ok := bare.Lookup(tools.NameTodoRead); ok {
		t.Fatal("todo_read must not register without WithTodoTools")
	}
	for _, name := range []string{"ui.chart", "ui.timeline", "ui.preview"} {
		if _, ok := bare.Lookup(name); ok {
			t.Fatalf("removed echo tool %s must not be registered", name)
		}
	}

	for _, name := range newNames {
		entry, ok := ToolCatalogEntryByKey(name)
		if !ok {
			t.Fatalf("expected a catalog entry for %s", name)
		}
		if entry.DisplayName == "" || entry.Description == "" || entry.Group == "" || entry.IconKey == "" {
			t.Fatalf("expected complete catalog metadata for %s, got %+v", name, entry)
		}
		if entry.AlwaysOn {
			t.Fatalf("expected %s toggleable (not AlwaysOn)", name)
		}
	}
}
