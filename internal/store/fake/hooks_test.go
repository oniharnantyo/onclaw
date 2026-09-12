package fake_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// hookSeedWorkspace creates a plain workspace for hook store tests.
func hookSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Hook WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// hookSeedAgent creates a workspace-scoped agent for agent hook tests.
func hookSeedAgent(t *testing.T, ctx context.Context, s store.Store, slug string) (*domain.Workspace, *domain.Agent) {
	t.Helper()
	ws := hookSeedWorkspace(t, ctx, s, slug)
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "hook-agent", Name: "Hook Agent", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	return ws, a
}

// validManagedInstanceHook returns a valid managed instance hook under key.
func validManagedInstanceHook(key string) *domain.InstanceHook {
	return &domain.InstanceHook{
		Key:     key,
		Source:  domain.HookSourceManaged,
		Version: 1,
		HookBase: domain.HookBase{
			Name:        "Managed " + key,
			Event:       domain.HookEventRunStarted,
			Matcher:     "scheduler",
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"https://hooks.example.com/observe"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

// validBuiltinInstanceHook returns a valid builtin instance hook under key at
// version with a version-distinct name.
func validBuiltinInstanceHook(key string, version int) *domain.InstanceHook {
	h := validManagedInstanceHook(key)
	h.Source = domain.HookSourceBuiltin
	h.Version = version
	h.Name = "Builtin " + key
	return h
}

func validWorkspaceHook(workspaceID, name string) *domain.WorkspaceHook {
	return &domain.WorkspaceHook{
		WorkspaceID: workspaceID,
		HookBase: domain.HookBase{
			Name:        name,
			Event:       domain.HookEventPreToolUse,
			Matcher:     "web.fetch",
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"https://hooks.example.com/gate"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

func validAgentHook(workspaceID, agentID, name string) *domain.AgentHook {
	return &domain.AgentHook{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		HookBase: domain.HookBase{
			Name:        name,
			Event:       domain.HookEventPostToolUse,
			Matcher:     `^web\.`,
			HandlerType: domain.HookHandlerCommand,
			Config:      json.RawMessage(`{"command":"/usr/local/bin/notify"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

func TestHookStore_InstanceCRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// Builtin-sourced rows cannot be created through CRUD (D15: pipeline only).
	if err := s.Hooks().CreateInstanceHook(ctx, validBuiltinInstanceHook("gate", 1)); !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("expected builtin-source create rejection, got %v", err)
	}

	first := validManagedInstanceHook("gate")
	if err := s.Hooks().CreateInstanceHook(ctx, first); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatal("expected id and timestamps to be assigned")
	}
	if first.Position != 0 || first.Status != domain.HookStatusOK || first.OnFailure != domain.HookFailureAllow {
		t.Fatalf("expected appended position and schema-default enums, got %+v", first.HookBase)
	}

	second := validManagedInstanceHook("audit")
	if err := s.Hooks().CreateInstanceHook(ctx, second); err != nil {
		t.Fatalf("unexpected create second error: %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("expected second hook appended at position 1, got %d", second.Position)
	}

	// Duplicate key conflicts; duplicate explicit id conflicts.
	if err := s.Hooks().CreateInstanceHook(ctx, validManagedInstanceHook("gate")); !isConflict(err) {
		t.Fatalf("expected ErrConflict for duplicate key, got %v", err)
	}
	dupID := validManagedInstanceHook("other")
	dupID.ID = first.ID
	if err := s.Hooks().CreateInstanceHook(ctx, dupID); !isConflict(err) {
		t.Fatalf("expected ErrConflict for duplicate id, got %v", err)
	}

	// List preserves create order (position, then created_at, then id).
	list, err := s.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("expected [gate, audit] in list order, got %+v", list)
	}

	// Get roundtrips; absent and empty ids are (nil, nil), not errors.
	got, err := s.Hooks().GetInstanceHook(ctx, first.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: %+v, %v", got, err)
	}
	if got.Key != "gate" || got.Source != domain.HookSourceManaged || got.Matcher != "scheduler" {
		t.Fatalf("unexpected hook: %+v", got)
	}
	if absent, err := s.Hooks().GetInstanceHook(ctx, "does-not-exist"); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for unknown id, got (%v, %v)", absent, err)
	}
	if absent, err := s.Hooks().GetInstanceHook(ctx, ""); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for empty id, got (%v, %v)", absent, err)
	}

	// Update replaces definition + health fields, preserves identity and
	// position; builtin rows are read-only.
	builtin := validBuiltinInstanceHook("builtin-gate", 1)
	if err := s.Hooks().UpsertBuiltinHook(ctx, builtin); err != nil {
		t.Fatalf("unexpected builtin upsert error: %v", err)
	}
	builtinRead, _ := s.Hooks().GetInstanceHook(ctx, builtin.ID)
	builtinRead.Name = "Hijacked"
	if err := s.Hooks().UpdateInstanceHook(ctx, builtinRead); !isInvalid(err) {
		t.Fatalf("expected builtin update rejection, got %v", err)
	}
	if err := s.Hooks().DeleteInstanceHook(ctx, builtin.ID); !isInvalid(err) {
		t.Fatalf("expected builtin delete rejection, got %v", err)
	}

	got.Name = "Gate v2"
	got.Enabled = false
	got.Status = domain.HookStatusError
	got.StatusError = "dial tcp: refused"
	origCreated := got.CreatedAt
	if err := s.Hooks().UpdateInstanceHook(ctx, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, _ := s.Hooks().GetInstanceHook(ctx, first.ID)
	if reloaded.Name != "Gate v2" || reloaded.Enabled {
		t.Fatalf("unexpected definition after update: %+v", reloaded.HookBase)
	}
	if reloaded.Status != domain.HookStatusError || reloaded.StatusError != "dial tcp: refused" {
		t.Fatalf("expected health fields written, got %+v", reloaded.HookBase)
	}
	if !reloaded.CreatedAt.Equal(origCreated) || reloaded.Position != 0 || reloaded.Key != "gate" || reloaded.Version != 1 {
		t.Fatalf("expected identity/position preserved, got %+v", reloaded)
	}

	// Unknown update is ErrNotFound.
	ghost := validManagedInstanceHook("ghost")
	ghost.ID = "missing-id"
	if err := s.Hooks().UpdateInstanceHook(ctx, ghost); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for unknown update, got %v", err)
	}

	// Delete managed works; builtin stays protected; unknown is ErrNotFound.
	if err := s.Hooks().DeleteInstanceHook(ctx, second.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, err := s.Hooks().GetInstanceHook(ctx, second.ID); err != nil || got != nil {
		t.Fatalf("expected hook gone after delete, got (%v, %v)", got, err)
	}
	if err := s.Hooks().DeleteInstanceHook(ctx, builtin.ID); !isInvalid(err) {
		t.Fatalf("expected builtin delete rejection after managed delete, got %v", err)
	}
	if err := s.Hooks().DeleteInstanceHook(ctx, "missing"); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}
}

func TestHookStore_BuiltinSync(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// Insert.
	v1 := validBuiltinInstanceHook("sec-gate", 1)
	v1.Position = 0
	if err := s.Hooks().UpsertBuiltinHook(ctx, v1); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	stored, _ := s.Hooks().GetInstanceHook(ctx, v1.ID)
	if stored == nil || stored.Version != 1 || stored.Name != "Builtin sec-gate" || stored.Source != domain.HookSourceBuiltin {
		t.Fatalf("expected builtin row inserted, got %+v", stored)
	}
	firstID := stored.ID
	createdAt := stored.CreatedAt

	// Re-syncing the same version is an idempotent no-op: the definition is
	// untouched even when the caller's payload differs.
	sameVersion := validBuiltinInstanceHook("sec-gate", 1)
	sameVersion.Name = "Builtin sec-gate REWRITE"
	if err := s.Hooks().UpsertBuiltinHook(ctx, sameVersion); err != nil {
		t.Fatalf("unexpected same-version upsert error: %v", err)
	}
	stored, _ = s.Hooks().GetInstanceHook(ctx, firstID)
	if stored.Name != "Builtin sec-gate" || stored.Version != 1 {
		t.Fatalf("expected same-version upsert to be a no-op, got %+v", stored)
	}
	if !stored.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at untouched by no-op re-sync")
	}
	if sameVersion.ID != firstID || sameVersion.Name != "Builtin sec-gate" {
		t.Fatalf("expected hook to reflect stored row after no-op, got %+v", sameVersion)
	}

	// Version bump applies; identity and created_at persist.
	v2 := validBuiltinInstanceHook("sec-gate", 2)
	v2.Name = "Builtin sec-gate v2"
	if err := s.Hooks().UpsertBuiltinHook(ctx, v2); err != nil {
		t.Fatalf("unexpected v2 upsert error: %v", err)
	}
	stored, _ = s.Hooks().GetInstanceHook(ctx, firstID)
	if stored.Version != 2 || stored.Name != "Builtin sec-gate v2" {
		t.Fatalf("expected version bump applied, got %+v", stored)
	}
	if stored.ID != firstID || !stored.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected identity preserved across version bump, got %+v", stored)
	}
	if stored.Status != domain.HookStatusOK {
		t.Fatalf("expected health reset on new definition, got %+v", stored.HookBase)
	}

	// Downgrade attempts never land and never error.
	old := validBuiltinInstanceHook("sec-gate", 1)
	old.Name = "Builtin sec-gate OLD"
	if err := s.Hooks().UpsertBuiltinHook(ctx, old); err != nil {
		t.Fatalf("expected downgrade attempt to be a silent no-op, got %v", err)
	}
	stored, _ = s.Hooks().GetInstanceHook(ctx, firstID)
	if stored.Version != 2 || stored.Name != "Builtin sec-gate v2" {
		t.Fatalf("expected stored row to keep v2 after downgrade attempt, got %+v", stored)
	}
	if old.Version != 2 || old.Name != "Builtin sec-gate v2" {
		t.Fatalf("expected hook to reflect stored row after blocked downgrade, got %+v", old)
	}

	// Record an execution against the builtin, then remove it from the keep
	// set: the row goes away, the audit record survives (D15/D16).
	exec := &domain.HookExecution{
		HookID:      &firstID,
		HookName:    stored.Name,
		HookLevel:   domain.HookLevelInstance,
		WorkspaceID: "ws-1",
		Event:       domain.HookEventPreToolUse,
		Decision:    "allow",
	}
	if err := s.Hooks().RecordHookExecution(ctx, exec); err != nil {
		t.Fatalf("unexpected record error: %v", err)
	}

	if err := s.Hooks().DeleteMissingBuiltinHooks(ctx, []string{"sec-gate"}); err != nil {
		t.Fatalf("unexpected delete-missing error: %v", err)
	}
	if got, _ := s.Hooks().GetInstanceHook(ctx, firstID); got == nil {
		t.Fatal("expected kept builtin to survive")
	}
	if err := s.Hooks().DeleteMissingBuiltinHooks(ctx, []string{"another"}); err != nil {
		t.Fatalf("unexpected delete-missing error: %v", err)
	}
	if got, _ := s.Hooks().GetInstanceHook(ctx, firstID); got != nil {
		t.Fatal("expected removed builtin to be deleted")
	}
	history, err := s.Hooks().ListHookExecutions(ctx, "ws-1", nil, 0)
	if err != nil || len(history) != 1 {
		t.Fatalf("expected execution to survive deletion, got %+v, %v", history, err)
	}
	if history[0].HookID != nil || history[0].HookName != "Builtin sec-gate v2" {
		t.Fatalf("expected detached execution with denormalized name, got %+v", history[0])
	}

	// An empty keep list removes every builtin row.
	if err := s.Hooks().UpsertBuiltinHook(ctx, validBuiltinInstanceHook("one", 1)); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	if err := s.Hooks().UpsertBuiltinHook(ctx, validBuiltinInstanceHook("two", 1)); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	if err := s.Hooks().DeleteMissingBuiltinHooks(ctx, nil); err != nil {
		t.Fatalf("unexpected delete-missing error: %v", err)
	}
	remaining, err := s.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	for _, h := range remaining {
		if h.Source == domain.HookSourceBuiltin {
			t.Fatalf("expected no builtin rows after empty keep list, got %+v", h)
		}
	}
}

func TestHookStore_WorkspaceCRUD_Isolation(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws1 := hookSeedWorkspace(t, ctx, s, "hooks-ws-1")
	ws2 := hookSeedWorkspace(t, ctx, s, "hooks-ws-2")

	// Unknown workspace is ErrNotFound.
	orphan := validWorkspaceHook("missing-ws", "Orphan")
	if err := s.Hooks().CreateWorkspaceHook(ctx, orphan); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for unknown workspace, got %v", err)
	}

	a := validWorkspaceHook(ws1.ID, "Gate A")
	b := validWorkspaceHook(ws1.ID, "Gate B")
	c := validWorkspaceHook(ws2.ID, "Gate C")
	for _, h := range []*domain.WorkspaceHook{a, b, c} {
		if err := s.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if a.Position != 0 || b.Position != 1 || c.Position != 0 {
		t.Fatalf("expected per-workspace appended positions, got %d %d %d", a.Position, b.Position, c.Position)
	}

	// Duplicate name within the workspace conflicts; same name elsewhere is fine.
	if err := s.Hooks().CreateWorkspaceHook(ctx, validWorkspaceHook(ws1.ID, "Gate A")); !isConflict(err) {
		t.Fatalf("expected ErrConflict for duplicate name, got %v", err)
	}

	// Cross-tenant reads are indistinguishable from absence (nil, nil).
	if got, err := s.Hooks().GetWorkspaceHook(ctx, ws2.ID, a.ID); err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for cross-tenant get, got (%v, %v)", got, err)
	}

	// Lists are workspace-scoped and ordered.
	list1, err := s.Hooks().ListWorkspaceHooks(ctx, ws1.ID)
	if err != nil || len(list1) != 2 || list1[0].ID != a.ID || list1[1].ID != b.ID {
		t.Fatalf("expected ws1 [Gate A, Gate B], got %+v, %v", list1, err)
	}
	list2, _ := s.Hooks().ListWorkspaceHooks(ctx, ws2.ID)
	if len(list2) != 1 || list2[0].ID != c.ID {
		t.Fatalf("expected ws2 [Gate C], got %+v", list2)
	}
	if empty, err := s.Hooks().ListWorkspaceHooks(ctx, ""); err != nil || len(empty) != 0 {
		t.Fatalf("expected empty result for empty scope, got %+v, %v", empty, err)
	}

	// Reposition is the only reorder path; unknown ids are ignored.
	if err := s.Hooks().RepositionWorkspaceHooks(ctx, ws1.ID, []string{b.ID, "missing-id", a.ID}); err != nil {
		t.Fatalf("unexpected reposition error: %v", err)
	}
	list1, _ = s.Hooks().ListWorkspaceHooks(ctx, ws1.ID)
	if list1[0].ID != b.ID || list1[1].ID != a.ID {
		t.Fatalf("expected reordered [Gate B, Gate A], got %+v", list1)
	}
	if list1[0].Position != 0 || list1[1].Position != 1 {
		t.Fatalf("expected positions rewritten to slice index, got %d %d", list1[0].Position, list1[1].Position)
	}
	// Cross-tenant reposition is a no-op for ws1 rows.
	if err := s.Hooks().RepositionWorkspaceHooks(ctx, ws2.ID, []string{a.ID}); err != nil {
		t.Fatalf("unexpected cross-tenant reposition error: %v", err)
	}
	list1, _ = s.Hooks().ListWorkspaceHooks(ctx, ws1.ID)
	if list1[0].ID != b.ID || list1[1].ID != a.ID {
		t.Fatalf("expected ws1 order untouched by other workspace's reposition, got %+v", list1)
	}

	// Update: scoped; cross-tenant is ErrNotFound; position preserved.
	read, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID)
	read.Name = "Gate A v2"
	read.Enabled = false
	if err := s.Hooks().UpdateWorkspaceHook(ctx, read); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID)
	if reloaded.Name != "Gate A v2" || reloaded.Enabled || reloaded.Position != 1 {
		t.Fatalf("unexpected hook after update: %+v", reloaded)
	}
	cross := *read
	cross.WorkspaceID = ws2.ID
	if err := s.Hooks().UpdateWorkspaceHook(ctx, &cross); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	// Rename onto ws1's other hook's name conflicts.
	read.Name = "Gate B"
	if err := s.Hooks().UpdateWorkspaceHook(ctx, read); !isConflict(err) {
		t.Fatalf("expected ErrConflict for rename onto existing name, got %v", err)
	}

	// Delete: cross-tenant and unknown are ErrNotFound.
	if err := s.Hooks().DeleteWorkspaceHook(ctx, ws2.ID, a.ID); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Hooks().DeleteWorkspaceHook(ctx, ws1.ID, a.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID); got != nil {
		t.Fatal("expected hook deleted")
	}
}

func TestHookStore_AgentCRUD_Isolation(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws1, agent1 := hookSeedAgent(t, ctx, s, "hooks-agent-1")
	ws2, agent2 := hookSeedAgent(t, ctx, s, "hooks-agent-2")

	// Unknown agent and cross-workspace agent are ErrNotFound.
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, "missing-agent", "Notify")); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, agent2.ID, "Cross Tenant")); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}

	h := validAgentHook(ws1.ID, agent1.ID, "Notify")
	if err := s.Hooks().CreateAgentHook(ctx, h); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if h.Position != 0 {
		t.Fatalf("expected appended position 0, got %d", h.Position)
	}
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, agent1.ID, "Notify")); !isConflict(err) {
		t.Fatalf("expected ErrConflict for duplicate name, got %v", err)
	}

	// Scoped reads: the workspace predicate is mandatory even with agent id.
	got, err := s.Hooks().GetAgentHook(ctx, ws1.ID, agent1.ID, h.ID)
	if err != nil || got == nil || got.Name != "Notify" {
		t.Fatalf("unexpected get: %+v, %v", got, err)
	}
	if absent, err := s.Hooks().GetAgentHook(ctx, ws2.ID, agent1.ID, h.ID); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for cross-tenant get, got (%v, %v)", absent, err)
	}
	if absent, err := s.Hooks().GetAgentHook(ctx, ws1.ID, agent2.ID, h.ID); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for cross-agent get, got (%v, %v)", absent, err)
	}

	// Lists are scoped by workspace AND agent.
	other := validAgentHook(ws2.ID, agent2.ID, "Notify")
	if err := s.Hooks().CreateAgentHook(ctx, other); err != nil {
		t.Fatalf("unexpected create other error: %v", err)
	}
	list1, _ := s.Hooks().ListAgentHooks(ctx, ws1.ID, agent1.ID)
	if len(list1) != 1 || list1[0].ID != h.ID {
		t.Fatalf("expected only agent1's hook, got %+v", list1)
	}
	if empty, _ := s.Hooks().ListAgentHooks(ctx, "", agent1.ID); len(empty) != 0 {
		t.Fatalf("expected empty result for empty workspace scope, got %+v", empty)
	}

	// Update and delete are tri-scoped.
	read, _ := s.Hooks().GetAgentHook(ctx, ws1.ID, agent1.ID, h.ID)
	read.Name = "Notify v2"
	if err := s.Hooks().UpdateAgentHook(ctx, read); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	cross := *read
	cross.WorkspaceID = ws2.ID
	if err := s.Hooks().UpdateAgentHook(ctx, &cross); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	if err := s.Hooks().DeleteAgentHook(ctx, ws2.ID, agent1.ID, h.ID); !isNotFound(err) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Hooks().DeleteAgentHook(ctx, ws1.ID, agent1.ID, h.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, _ := s.Hooks().GetAgentHook(ctx, ws1.ID, agent1.ID, h.ID); got != nil {
		t.Fatal("expected agent hook deleted")
	}
}

func TestHookStore_Executions(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws1 := hookSeedWorkspace(t, ctx, s, "hooks-exec-1")
	ws2 := hookSeedWorkspace(t, ctx, s, "hooks-exec-2")

	hookA := validWorkspaceHook(ws1.ID, "Gate A")
	if err := s.Hooks().CreateWorkspaceHook(ctx, hookA); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	hookB := validWorkspaceHook(ws1.ID, "Gate B")
	if err := s.Hooks().CreateWorkspaceHook(ctx, hookB); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	mkExec := func(hookID *string, name string, wsID string, at time.Time, decision, detail string) *domain.HookExecution {
		return &domain.HookExecution{
			HookID:      hookID,
			HookName:    name,
			HookLevel:   domain.HookLevelWorkspace,
			WorkspaceID: wsID,
			Event:       domain.HookEventPreToolUse,
			Decision:    decision,
			DurationMS:  42,
			Detail:      detail,
			Origin:      "user",
			CreatedAt:   at,
		}
	}
	idA := hookA.ID
	idB := hookB.ID
	recs := []*domain.HookExecution{
		mkExec(&idA, "Gate A", ws1.ID, base, "allow", strings.Repeat("x", 300)),
		mkExec(&idB, "Gate B", ws1.ID, base.Add(time.Minute), "block", "denied"),
		mkExec(&idA, "Gate A", ws1.ID, base.Add(2*time.Minute), "allow", "ok"),
		mkExec(nil, "Orphan Observer", ws2.ID, base, "allow", "ok"),
	}
	for _, e := range recs {
		if err := s.Hooks().RecordHookExecution(ctx, e); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
	}

	// Detail truncates to 256 at write time.
	if len(recs[0].Detail) != 256 {
		t.Fatalf("expected detail truncated to 256 chars, got %d", len(recs[0].Detail))
	}

	// Newest first across the whole workspace; workspace isolation holds.
	all, err := s.Hooks().ListHookExecutions(ctx, ws1.ID, nil, 0)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 ws1 records, got %d", len(all))
	}
	if all[0].HookName != "Gate A" || all[1].HookName != "Gate B" || all[2].HookName != "Gate A" {
		t.Fatalf("expected newest-first ordering, got %+v", all)
	}
	if all[0].CreatedAt.Before(all[2].CreatedAt) {
		t.Fatal("expected created_at DESC ordering")
	}

	// hookID filter.
	onlyB, _ := s.Hooks().ListHookExecutions(ctx, ws1.ID, &idB, 0)
	if len(onlyB) != 1 || onlyB[0].HookName != "Gate B" {
		t.Fatalf("expected only Gate B records, got %+v", onlyB)
	}

	// Limit applies after ordering.
	limited, _ := s.Hooks().ListHookExecutions(ctx, ws1.ID, nil, 2)
	if len(limited) != 2 || limited[0].HookName != "Gate A" || limited[1].HookName != "Gate B" {
		t.Fatalf("expected 2 newest records, got %+v", limited)
	}

	// Empty workspace scope never queries.
	if empty, err := s.Hooks().ListHookExecutions(ctx, "", nil, 0); err != nil || len(empty) != 0 {
		t.Fatalf("expected empty result for empty workspace, got %+v, %v", empty, err)
	}
	otherWS, _ := s.Hooks().ListHookExecutions(ctx, ws2.ID, nil, 0)
	if len(otherWS) != 1 || otherWS[0].HookName != "Orphan Observer" {
		t.Fatalf("expected ws2 isolation with 1 record, got %+v", otherWS)
	}

	// Deleting the hook keeps its history with hook_id detached.
	if err := s.Hooks().DeleteWorkspaceHook(ctx, ws1.ID, hookB.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	after, _ := s.Hooks().ListHookExecutions(ctx, ws1.ID, nil, 0)
	if len(after) != 3 {
		t.Fatalf("expected history to survive hook deletion, got %d", len(after))
	}
	var bRecord *domain.HookExecution
	for i := range after {
		if after[i].HookName == "Gate B" {
			bRecord = &after[i]
		}
	}
	if bRecord == nil || bRecord.HookID != nil {
		t.Fatalf("expected detached record with denormalized name, got %+v", bRecord)
	}

	if err := s.Hooks().RecordHookExecution(ctx, nil); !isInvalid(err) {
		t.Fatalf("expected ErrInvalid for nil execution, got %v", err)
	}
}

func TestHookStore_SetHookDeliveryStatus(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, agent := hookSeedAgent(t, ctx, s, "hooks-status-1")

	instance := validManagedInstanceHook("status-gate")
	if err := s.Hooks().CreateInstanceHook(ctx, instance); err != nil {
		t.Fatalf("unexpected create instance error: %v", err)
	}
	wsHook := validWorkspaceHook(ws.ID, "Status Gate")
	if err := s.Hooks().CreateWorkspaceHook(ctx, wsHook); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	agentHook := validAgentHook(ws.ID, agent.ID, "Status Observer")
	if err := s.Hooks().CreateAgentHook(ctx, agentHook); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}

	// Error status round-trips through every level, addressed by id.
	for _, tc := range []struct {
		level domain.HookLevel
		id    string
		get   func() (domain.HookStatus, string)
	}{
		{domain.HookLevelInstance, instance.ID, func() (domain.HookStatus, string) {
			h, _ := s.Hooks().GetInstanceHook(ctx, instance.ID)
			return h.Status, h.StatusError
		}},
		{domain.HookLevelWorkspace, wsHook.ID, func() (domain.HookStatus, string) {
			h, _ := s.Hooks().GetWorkspaceHook(ctx, ws.ID, wsHook.ID)
			return h.Status, h.StatusError
		}},
		{domain.HookLevelAgent, agentHook.ID, func() (domain.HookStatus, string) {
			h, _ := s.Hooks().GetAgentHook(ctx, ws.ID, agent.ID, agentHook.ID)
			return h.Status, h.StatusError
		}},
	} {
		if err := s.Hooks().SetHookDeliveryStatus(ctx, tc.level, tc.id, domain.HookStatusError, "connect refused"); err != nil {
			t.Fatalf("%s: unexpected status error: %v", tc.level, err)
		}
		gotStatus, gotErr := tc.get()
		if gotStatus != domain.HookStatusError || gotErr != "connect refused" {
			t.Fatalf("%s: status = (%q, %q), want (error, connect refused)", tc.level, gotStatus, gotErr)
		}
		// Recovery back to ok.
		if err := s.Hooks().SetHookDeliveryStatus(ctx, tc.level, tc.id, domain.HookStatusOK, ""); err != nil {
			t.Fatalf("%s: unexpected ok write error: %v", tc.level, err)
		}
		gotStatus, gotErr = tc.get()
		if gotStatus != domain.HookStatusOK || gotErr != "" {
			t.Fatalf("%s: status = (%q, %q), want (ok, empty)", tc.level, gotStatus, gotErr)
		}
	}

	// Unknown ids and empty ids are idempotent no-ops; unknown levels are invalid.
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevelWorkspace, "no-such-hook", domain.HookStatusError, "x"); err != nil {
		t.Fatalf("expected no-op for unknown id, got %v", err)
	}
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevelWorkspace, "", domain.HookStatusError, "x"); err != nil {
		t.Fatalf("expected no-op for empty id, got %v", err)
	}
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevel("galactic"), wsHook.ID, domain.HookStatusError, "x"); !isInvalid(err) {
		t.Fatalf("expected ErrInvalid for unknown level, got %v", err)
	}
}

// TestHookStore_MatcherAndIfRoundTrip covers the D19 matcher string tiers and
// the D20 `if` condition through create/read/update, mirroring the postgres
// integration coverage.
func TestHookStore_MatcherAndIfRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws := hookSeedWorkspace(t, ctx, s, "hooks-matcher")

	// Every matcher tier round-trips: "" and "*" are match-all, "a|b" is the
	// list tier, "web.*" a family entry, and anything charset-breaking is the
	// regex tier. An if condition is optional and empty persists as "".
	matchers := []string{"", "*", "web.fetch|cron", "web.*", `^grafana\.`}
	for i, matcher := range matchers {
		h := validWorkspaceHook(ws.ID, fmt.Sprintf("Gate %d", i))
		h.Matcher = matcher
		if i%2 == 0 {
			h.If = `web.fetch("secret")`
		}
		if err := s.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("unexpected create error for matcher %q: %v", matcher, err)
		}
		got, err := s.Hooks().GetWorkspaceHook(ctx, ws.ID, h.ID)
		if err != nil || got == nil {
			t.Fatalf("unexpected get for matcher %q: %+v, %v", matcher, got, err)
		}
		if got.Matcher != matcher {
			t.Errorf("matcher roundtrip: got %q want %q", got.Matcher, matcher)
		}
		if got.If != h.If {
			t.Errorf("if roundtrip for matcher %q: got %q want %q", matcher, got.If, h.If)
		}
	}

	// Update rewrites matcher and if; clearing if persists as empty.
	h := validWorkspaceHook(ws.ID, "Gate Update")
	if err := s.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	h.Matcher = "db.*|cache.*"
	h.If = `db.query(DROP|DELETE)`
	if err := s.Hooks().UpdateWorkspaceHook(ctx, h); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	got, _ := s.Hooks().GetWorkspaceHook(ctx, ws.ID, h.ID)
	if got.Matcher != "db.*|cache.*" || got.If != `db.query(DROP|DELETE)` {
		t.Fatalf("expected updated matcher/if, got matcher %q if %q", got.Matcher, got.If)
	}
	got.If = ""
	if err := s.Hooks().UpdateWorkspaceHook(ctx, got); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	got, _ = s.Hooks().GetWorkspaceHook(ctx, ws.ID, h.ID)
	if got.If != "" {
		t.Fatalf("expected if cleared to empty string, got %q", got.If)
	}
}

// isConflict reports whether err wraps domain.ErrConflict.
func isConflict(err error) bool {
	return errors.Is(err, domain.ErrConflict)
}

// isNotFound reports whether err wraps domain.ErrNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}

// isInvalid reports whether err wraps domain.ErrInvalid.
func isInvalid(err error) bool {
	return errors.Is(err, domain.ErrInvalid)
}
