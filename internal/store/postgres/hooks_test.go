//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// hooksSeedWorkspace creates a plain workspace for hook store tests.
func hooksSeedWorkspace(t *testing.T, ctx context.Context, s store.Store, slug string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Hook WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace error: %v", err)
	}
	return ws
}

// hooksSeedAgent creates a workspace-scoped agent for agent hook tests.
func hooksSeedAgent(t *testing.T, ctx context.Context, s store.Store, slug string) (*domain.Workspace, *domain.Agent) {
	t.Helper()
	ws := hooksSeedWorkspace(t, ctx, s, slug)
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

func validManagedInstanceHook(key string) *domain.InstanceHook {
	return &domain.InstanceHook{
		Key:     key,
		Source:  domain.HookSourceManaged,
		Version: 1,
		HookBase: domain.HookBase{
			Name:        "Managed " + key,
			Event:       domain.HookEventRunStarted,
			Matcher:     "cron",
			HandlerType: domain.HookHandlerHTTP,
			Config:      json.RawMessage(`{"url":"https://hooks.example.com/observe"}`),
			TimeoutMS:   domain.DefaultHookTimeoutMS,
			Enabled:     true,
		},
	}
}

func validBuiltinInstanceHook(key string, version int) *domain.InstanceHook {
	h := validManagedInstanceHook(key)
	h.Source = domain.HookSourceBuiltin
	h.Version = version
	h.Name = fmt.Sprintf("Builtin %s v%d", key, version)
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

func TestIntegration_HookStore_InstanceCRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

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

	// Duplicate (source, key) conflicts.
	if err := s.Hooks().CreateInstanceHook(ctx, validManagedInstanceHook("gate")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate key, got %v", err)
	}

	list, err := s.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("expected [gate, audit] in list order, got %+v", list)
	}

	// Get roundtrips matcher and config jsonb; absent ids are (nil, nil).
	got, err := s.Hooks().GetInstanceHook(ctx, first.ID)
	if err != nil || got == nil {
		t.Fatalf("unexpected get result: %+v, %v", got, err)
	}
	if got.Key != "gate" || got.Source != domain.HookSourceManaged || got.Version != 1 {
		t.Fatalf("unexpected identity fields: %+v", got)
	}
	if got.Matcher != "cron" {
		t.Fatalf("unexpected matcher roundtrip: %+v", got.Matcher)
	}
	// jsonb normalizes whitespace, so compare semantically.
	var cfg map[string]any
	if err := json.Unmarshal(got.Config, &cfg); err != nil {
		t.Fatalf("unexpected config decode error: %v", err)
	}
	if cfg["url"] != "https://hooks.example.com/observe" {
		t.Fatalf("unexpected config roundtrip: %s", got.Config)
	}
	if absent, err := s.Hooks().GetInstanceHook(ctx, "00000000-0000-0000-0000-000000000000"); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for unknown id, got (%v, %v)", absent, err)
	}
	if absent, err := s.Hooks().GetInstanceHook(ctx, "invalid-uuid"); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for malformed id, got (%v, %v)", absent, err)
	}
	if absent, err := s.Hooks().GetInstanceHook(ctx, ""); err != nil || absent != nil {
		t.Fatalf("expected (nil, nil) for empty id, got (%v, %v)", absent, err)
	}

	// Builtin rows are read-only through CRUD.
	builtin := validBuiltinInstanceHook("builtin-gate", 1)
	if err := s.Hooks().UpsertBuiltinHook(ctx, builtin); err != nil {
		t.Fatalf("unexpected builtin upsert error: %v", err)
	}
	builtinRead, _ := s.Hooks().GetInstanceHook(ctx, builtin.ID)
	if builtinRead == nil {
		t.Fatal("expected builtin row readable")
	}
	builtinRead.Name = "Hijacked"
	if err := s.Hooks().UpdateInstanceHook(ctx, builtinRead); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected builtin update rejection, got %v", err)
	}
	if err := s.Hooks().DeleteInstanceHook(ctx, builtin.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected builtin delete rejection, got %v", err)
	}

	// Update replaces definition + health, preserves identity and position.
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
	if !reloaded.UpdatedAt.After(reloaded.CreatedAt) {
		t.Fatalf("expected updated_at to advance on update, got %v vs %v", reloaded.UpdatedAt, reloaded.CreatedAt)
	}

	// Unknown update/delete are ErrNotFound.
	ghost := validManagedInstanceHook("ghost")
	ghost.ID = "00000000-0000-0000-0000-000000000000"
	if err := s.Hooks().UpdateInstanceHook(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown update, got %v", err)
	}
	if err := s.Hooks().DeleteInstanceHook(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown delete, got %v", err)
	}

	if err := s.Hooks().DeleteInstanceHook(ctx, second.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, _ := s.Hooks().GetInstanceHook(ctx, second.ID); got != nil {
		t.Fatal("expected hook gone after delete")
	}
}

func TestIntegration_HookStore_WorkspaceCRUD_Isolation(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := hooksSeedWorkspace(t, ctx, s, "hooks-ws-1")
	ws2 := hooksSeedWorkspace(t, ctx, s, "hooks-ws-2")

	// Unknown workspace surfaces as ErrNotFound (FK violation).
	if err := s.Hooks().CreateWorkspaceHook(ctx, validWorkspaceHook("00000000-0000-0000-0000-000000000000", "Orphan")); !errors.Is(err, domain.ErrNotFound) {
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
	if err := s.Hooks().CreateWorkspaceHook(ctx, validWorkspaceHook(ws1.ID, "Gate A")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate name, got %v", err)
	}

	// Cross-tenant reads are indistinguishable from absence (nil, nil).
	if got, err := s.Hooks().GetWorkspaceHook(ctx, ws2.ID, a.ID); err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for cross-tenant get, got (%v, %v)", got, err)
	}

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

	// Workspace B cannot mutate workspace A's hooks.
	read, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID)
	crossUpdate := *read
	crossUpdate.WorkspaceID = ws2.ID
	crossUpdate.Name = "Hijacked"
	if err := s.Hooks().UpdateWorkspaceHook(ctx, &crossUpdate); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	if err := s.Hooks().DeleteWorkspaceHook(ctx, ws2.ID, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Hooks().RepositionWorkspaceHooks(ctx, ws2.ID, []string{a.ID, b.ID}); err != nil {
		t.Fatalf("unexpected cross-tenant reposition error: %v", err)
	}
	list1, _ = s.Hooks().ListWorkspaceHooks(ctx, ws1.ID)
	if list1[0].ID != a.ID || list1[1].ID != b.ID {
		t.Fatalf("expected ws1 order untouched by other workspace's reposition, got %+v", list1)
	}

	// Update preserves position and created_at; rename conflict is ErrConflict.
	read.Name = "Gate A v2"
	read.Enabled = false
	if err := s.Hooks().UpdateWorkspaceHook(ctx, read); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	reloaded, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID)
	if reloaded.Name != "Gate A v2" || reloaded.Enabled || reloaded.Position != 0 {
		t.Fatalf("unexpected hook after update: %+v", reloaded)
	}
	if !reloaded.CreatedAt.Equal(read.CreatedAt) {
		t.Fatalf("expected created_at preserved: %v vs %v", read.CreatedAt, reloaded.CreatedAt)
	}
	reloaded.Name = "Gate B"
	if err := s.Hooks().UpdateWorkspaceHook(ctx, reloaded); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for rename onto existing name, got %v", err)
	}

	if err := s.Hooks().DeleteWorkspaceHook(ctx, ws1.ID, a.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, _ := s.Hooks().GetWorkspaceHook(ctx, ws1.ID, a.ID); got != nil {
		t.Fatal("expected hook deleted")
	}
}

func TestIntegration_HookStore_AgentCRUD_Isolation(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1, agent1 := hooksSeedAgent(t, ctx, s, "hooks-agent-1")
	ws2, agent2 := hooksSeedAgent(t, ctx, s, "hooks-agent-2")

	// Unknown agent and cross-workspace agent are ErrNotFound.
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, "00000000-0000-0000-0000-000000000000", "Notify")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown agent, got %v", err)
	}
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, agent2.ID, "Cross Tenant")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-workspace agent, got %v", err)
	}

	h := validAgentHook(ws1.ID, agent1.ID, "Notify")
	if err := s.Hooks().CreateAgentHook(ctx, h); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if h.Position != 0 {
		t.Fatalf("expected appended position 0, got %d", h.Position)
	}
	if err := s.Hooks().CreateAgentHook(ctx, validAgentHook(ws1.ID, agent1.ID, "Notify")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate name, got %v", err)
	}

	// Tri-scoped reads: workspace predicate is mandatory even with agent id.
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
	if err := s.Hooks().UpdateAgentHook(ctx, &cross); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
	if err := s.Hooks().DeleteAgentHook(ctx, ws2.ID, agent1.ID, h.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	if err := s.Hooks().DeleteAgentHook(ctx, ws1.ID, agent1.ID, h.ID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if got, _ := s.Hooks().GetAgentHook(ctx, ws1.ID, agent1.ID, h.ID); got != nil {
		t.Fatal("expected agent hook deleted")
	}
}

func TestIntegration_HookStore_Reposition(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := hooksSeedWorkspace(t, ctx, s, "hooks-reposition")

	// Workspace level: three hooks created in order a, b, c.
	hooks := make([]*domain.WorkspaceHook, 0, 3)
	for _, name := range []string{"Gate A", "Gate B", "Gate C"} {
		h := validWorkspaceHook(ws.ID, name)
		if err := s.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("unexpected create error: %v", err)
		}
		hooks = append(hooks, h)
	}

	// Reorder to [c, bogus, a, b]: compaction skips the unknown id.
	if err := s.Hooks().RepositionWorkspaceHooks(ctx, ws.ID, []string{
		hooks[2].ID, "00000000-0000-0000-0000-000000000000", hooks[0].ID, hooks[1].ID,
	}); err != nil {
		t.Fatalf("unexpected reposition error: %v", err)
	}
	list, err := s.Hooks().ListWorkspaceHooks(ctx, ws.ID)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	wantOrder := []string{"Gate C", "Gate A", "Gate B"}
	for i, want := range wantOrder {
		if list[i].Name != want {
			t.Fatalf("expected position %d = %s, got %s (list %+v)", i, want, list[i].Name, list)
		}
		if list[i].Position != i {
			t.Fatalf("expected compacted position %d, got %d", i, list[i].Position)
		}
	}

	// Instance level: one managed + one builtin, reordered together.
	managed := validManagedInstanceHook("audit")
	if err := s.Hooks().CreateInstanceHook(ctx, managed); err != nil {
		t.Fatalf("unexpected instance create error: %v", err)
	}
	builtin := validBuiltinInstanceHook("builtin-order", 1)
	if err := s.Hooks().UpsertBuiltinHook(ctx, builtin); err != nil {
		t.Fatalf("unexpected builtin upsert error: %v", err)
	}
	if err := s.Hooks().RepositionInstanceHooks(ctx, []string{builtin.ID, managed.ID}); err != nil {
		t.Fatalf("unexpected instance reposition error: %v", err)
	}
	instanceList, err := s.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("unexpected instance list error: %v", err)
	}
	if len(instanceList) != 2 || instanceList[0].ID != builtin.ID || instanceList[1].ID != managed.ID {
		t.Fatalf("expected instance order [builtin, managed], got %+v", instanceList)
	}
	if instanceList[0].Position != 0 || instanceList[1].Position != 1 {
		t.Fatalf("expected compacted instance positions, got %d %d", instanceList[0].Position, instanceList[1].Position)
	}
}

func TestIntegration_HookStore_Executions(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := hooksSeedWorkspace(t, ctx, s, "hooks-exec-1")
	ws2 := hooksSeedWorkspace(t, ctx, s, "hooks-exec-2")

	hookA := validWorkspaceHook(ws1.ID, "Gate A")
	if err := s.Hooks().CreateWorkspaceHook(ctx, hookA); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	hookB := validWorkspaceHook(ws1.ID, "Gate B")
	if err := s.Hooks().CreateWorkspaceHook(ctx, hookB); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}

	base := time.Now().UTC().Add(-time.Hour)
	exitCode := 2
	httpStatus := 200
	tokenCount := int64(321)
	mkExec := func(hookID *string, name string, wsID string, at time.Time, decision, detail string) *domain.HookExecution {
		e := &domain.HookExecution{
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
		if decision == "block" {
			e.ExitCode = &exitCode
		} else {
			e.HTTPStatus = &httpStatus
			e.TokenCount = &tokenCount
		}
		return e
	}
	idA, idB := hookA.ID, hookB.ID
	recs := []*domain.HookExecution{
		mkExec(&idA, "Gate A", ws1.ID, base, "allow", strings.Repeat("x", 300)),
		mkExec(&idB, "Gate B", ws1.ID, base.Add(time.Minute), "block", "denied by policy"),
		mkExec(&idA, "Gate A", ws1.ID, base.Add(2*time.Minute), "allow", "ok"),
		mkExec(nil, "Instance Observer", ws2.ID, base, "allow", "ok"),
	}
	for _, e := range recs {
		if err := s.Hooks().RecordHookExecution(ctx, e); err != nil {
			t.Fatalf("unexpected record error: %v", err)
		}
		if e.ID == "" || e.CreatedAt.IsZero() {
			t.Fatal("expected id and created_at assigned on record")
		}
	}

	// Detail truncates to 256 characters at write time (D16).
	if len(recs[0].Detail) != 256 {
		t.Fatalf("expected detail truncated to 256 chars, got %d", len(recs[0].Detail))
	}

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
	if all[1].ExitCode == nil || *all[1].ExitCode != 2 {
		t.Fatalf("expected exit code roundtrip on blocked record, got %+v", all[1])
	}
	if all[0].HTTPStatus == nil || *all[0].HTTPStatus != 200 || all[0].TokenCount == nil || *all[0].TokenCount != 321 {
		t.Fatalf("expected http status/token count roundtrip, got %+v", all[0])
	}
	// all[2] is the oldest record — the one written with the 300-char detail.
	if len(all[2].Detail) != 256 {
		t.Fatalf("expected stored detail truncated to 256, got %d", len(all[2].Detail))
	}

	// hookID filter and limit.
	onlyB, _ := s.Hooks().ListHookExecutions(ctx, ws1.ID, &idB, 0)
	if len(onlyB) != 1 || onlyB[0].HookName != "Gate B" {
		t.Fatalf("expected only Gate B records, got %+v", onlyB)
	}
	limited, _ := s.Hooks().ListHookExecutions(ctx, ws1.ID, nil, 2)
	if len(limited) != 2 || limited[0].HookName != "Gate A" || limited[1].HookName != "Gate B" {
		t.Fatalf("expected 2 newest records, got %+v", limited)
	}

	// Empty workspace scope never queries; isolation holds per workspace.
	if empty, err := s.Hooks().ListHookExecutions(ctx, "", nil, 0); err != nil || len(empty) != 0 {
		t.Fatalf("expected empty result for empty workspace, got %+v, %v", empty, err)
	}
	otherWS, _ := s.Hooks().ListHookExecutions(ctx, ws2.ID, nil, 0)
	if len(otherWS) != 1 || otherWS[0].HookName != "Instance Observer" {
		t.Fatalf("expected ws2 isolation with 1 record, got %+v", otherWS)
	}
	if otherWS[0].HookID != nil {
		t.Fatalf("expected nil hook_id roundtrip for observer record, got %v", *otherWS[0].HookID)
	}

	// Deleting the hook keeps its history with hook_id detached (SET NULL
	// trigger) and the denormalized name intact.
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

	if err := s.Hooks().RecordHookExecution(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil execution, got %v", err)
	}
}

func TestIntegration_HookStore_SetHookDeliveryStatus(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, agent := hooksSeedAgent(t, ctx, s, "hooks-status-1")

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
		if err := s.Hooks().SetHookDeliveryStatus(ctx, tc.level, tc.id, domain.HookStatusError, "dial tcp: refused"); err != nil {
			t.Fatalf("%s: unexpected status error: %v", tc.level, err)
		}
		gotStatus, gotErr := tc.get()
		if gotStatus != domain.HookStatusError || gotErr != "dial tcp: refused" {
			t.Fatalf("%s: status = (%q, %q), want (error, dial tcp: refused)", tc.level, gotStatus, gotErr)
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
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevelWorkspace, "00000000-0000-0000-0000-000000000000", domain.HookStatusError, "x"); err != nil {
		t.Fatalf("expected no-op for unknown id, got %v", err)
	}
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevelWorkspace, "", domain.HookStatusError, "x"); err != nil {
		t.Fatalf("expected no-op for empty id, got %v", err)
	}
	if err := s.Hooks().SetHookDeliveryStatus(ctx, domain.HookLevel("galactic"), wsHook.ID, domain.HookStatusError, "x"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown level, got %v", err)
	}
}

func TestIntegration_HookStore_BuiltinSync_VersionGuard(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// Insert.
	v1 := validBuiltinInstanceHook("sec-gate", 1)
	if err := s.Hooks().UpsertBuiltinHook(ctx, v1); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}
	stored, _ := s.Hooks().GetInstanceHook(ctx, v1.ID)
	if stored == nil || stored.Version != 1 || stored.Name != "Builtin sec-gate v1" || stored.Source != domain.HookSourceBuiltin {
		t.Fatalf("expected builtin row inserted, got %+v", stored)
	}
	firstID := stored.ID
	createdAt := stored.CreatedAt

	// Re-syncing the same version is an idempotent no-op.
	sameVersion := validBuiltinInstanceHook("sec-gate", 1)
	sameVersion.Name = "Builtin sec-gate REWRITE"
	if err := s.Hooks().UpsertBuiltinHook(ctx, sameVersion); err != nil {
		t.Fatalf("unexpected same-version upsert error: %v", err)
	}
	stored, _ = s.Hooks().GetInstanceHook(ctx, firstID)
	if stored.Name != "Builtin sec-gate v1" || stored.Version != 1 {
		t.Fatalf("expected same-version upsert to be a no-op, got %+v", stored)
	}
	if !stored.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at untouched by no-op re-sync")
	}
	if sameVersion.ID != firstID || sameVersion.Name != "Builtin sec-gate v1" {
		t.Fatalf("expected hook to reflect stored row after no-op, got %+v", sameVersion)
	}

	// Version bump applies; identity and created_at persist; health resets.
	v2 := validBuiltinInstanceHook("sec-gate", 2)
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

	// Downgrade attempts never land and never error (D15).
	old := validBuiltinInstanceHook("sec-gate", 1)
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
	// set: the row goes away, the audit record survives detached (D15/D16).
	exec := &domain.HookExecution{
		HookID:      &firstID,
		HookName:    stored.Name,
		HookLevel:   domain.HookLevelInstance,
		WorkspaceID: "00000000-0000-0000-0000-0000000000aa",
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

	// A managed row must NOT be touched by the builtin sweep.
	managed := validManagedInstanceHook("sec-gate-managed")
	if err := s.Hooks().CreateInstanceHook(ctx, managed); err != nil {
		t.Fatalf("unexpected managed create error: %v", err)
	}
	if err := s.Hooks().DeleteMissingBuiltinHooks(ctx, []string{"another-key"}); err != nil {
		t.Fatalf("unexpected delete-missing error: %v", err)
	}
	if got, _ := s.Hooks().GetInstanceHook(ctx, firstID); got != nil {
		t.Fatal("expected removed builtin to be deleted")
	}
	if got, _ := s.Hooks().GetInstanceHook(ctx, managed.ID); got == nil {
		t.Fatal("expected managed row untouched by builtin sweep")
	}
	history, err := s.Hooks().ListHookExecutions(ctx, "00000000-0000-0000-0000-0000000000aa", nil, 0)
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

// TestIntegration_HookStore_BuiltinUpsertRace simulates concurrent server
// starts (design.md D15): eight pods upsert the same (source, key) with mixed
// versions over separate connections. The final row must be the max version,
// and no upsert may error.
func TestIntegration_HookStore_BuiltinUpsertRace(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)

	const goroutines = 8
	versions := []int{3, 7, 1, 8, 2, 5, 4, 6} // mixed order on purpose

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		version := versions[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each "pod" gets its own connection so the upserts genuinely
			// contend instead of serializing on one pooled connection.
			conn, err := pgx.Connect(ctx, schemaDSN)
			if err != nil {
				errCh <- fmt.Errorf("connect: %w", err)
				return
			}
			defer conn.Close(ctx)
			hooks := postgres.NewHookStore(conn)

			<-start
			hook := validBuiltinInstanceHook("race-gate", version)
			if err := hooks.UpsertBuiltinHook(ctx, hook); err != nil {
				errCh <- fmt.Errorf("upsert v%d: %w", version, err)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("unexpected concurrent upsert error: %v", err)
	}

	// The final row must carry the max version and its definition.
	final, err := s.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	var raceRow *domain.InstanceHook
	for i := range final {
		if final[i].Key == "race-gate" {
			if raceRow != nil {
				t.Fatal("expected exactly one row for the raced (source, key)")
			}
			raceRow = &final[i]
		}
	}
	if raceRow == nil {
		t.Fatal("expected raced builtin row to exist")
	}
	if raceRow.Version != 8 || raceRow.Name != "Builtin race-gate v8" {
		t.Fatalf("expected final row to be max version 8, got v%d (%q)", raceRow.Version, raceRow.Name)
	}

	// A late-arriving older pod must still be a silent no-op.
	late := validBuiltinInstanceHook("race-gate", 3)
	if err := s.Hooks().UpsertBuiltinHook(ctx, late); err != nil {
		t.Fatalf("expected late downgrade to be a silent no-op, got %v", err)
	}
	if raceRow.Version != 8 {
		t.Fatalf("expected version to remain 8 after late downgrade, got %d", raceRow.Version)
	}
}

// TestIntegration_HookStore_MatcherAndIfRoundTrip covers the D19 matcher
// string tiers and the D20 `if` condition (if_rule column, migrations
// 000028/000029) through every write path: workspace/agent/instance inserts,
// instance update, and the builtin upsert's insert and DO UPDATE arms. A raw
// column probe proves matcher/if_rule persist as plain text, not JSON.
func TestIntegration_HookStore_MatcherAndIfRoundTrip(t *testing.T) {
	s, schemaDSN, ctx := setupTestSchema(t)
	ws, agent := hooksSeedAgent(t, ctx, s, "hooks-matcher")

	conn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer conn.Close(ctx)

	// Workspace create: every matcher tier round-trips; if_rule defaults to ''
	// when unset and persists verbatim when set.
	matchers := []string{"", "*", "web.fetch|cron", "web.*", `^grafana\.`}
	for i, matcher := range matchers {
		h := validWorkspaceHook(ws.ID, fmt.Sprintf("Gate %d", i))
		h.Matcher = matcher
		if i%2 == 0 {
			h.If = `web.fetch("secret")`
		}
		if err := s.Hooks().CreateWorkspaceHook(ctx, h); err != nil {
			t.Fatalf("create matcher %q: %v", matcher, err)
		}
		got, err := s.Hooks().GetWorkspaceHook(ctx, ws.ID, h.ID)
		if err != nil || got == nil {
			t.Fatalf("get matcher %q: %+v, %v", matcher, got, err)
		}
		if got.Matcher != matcher || got.If != h.If {
			t.Errorf("roundtrip matcher %q: got (%q, %q) want (%q, %q)", matcher, got.Matcher, got.If, matcher, h.If)
		}
		// The column shape itself: plain text, no JSON envelope.
		var storedMatcher, storedIf string
		if err := conn.QueryRow(ctx,
			`SELECT matcher, if_rule FROM workspace_hooks WHERE id = $1`, h.ID,
		).Scan(&storedMatcher, &storedIf); err != nil {
			t.Fatalf("raw scan matcher %q: %v", matcher, err)
		}
		if storedMatcher != matcher || storedIf != h.If {
			t.Errorf("raw columns for matcher %q: got (%q, %q) want (%q, %q)", matcher, storedMatcher, storedIf, matcher, h.If)
		}
	}

	// Agent create carries an if condition through the INSERT with agent_id.
	agentHook := validAgentHook(ws.ID, agent.ID, "Notify")
	agentHook.If = `files.write(/etc)`
	if err := s.Hooks().CreateAgentHook(ctx, agentHook); err != nil {
		t.Fatalf("unexpected agent create error: %v", err)
	}
	gotAgent, err := s.Hooks().GetAgentHook(ctx, ws.ID, agent.ID, agentHook.ID)
	if err != nil || gotAgent == nil || gotAgent.Matcher != `^web\.` || gotAgent.If != `files.write(/etc)` {
		t.Fatalf("agent matcher/if roundtrip: %+v, %v", gotAgent, err)
	}

	// Instance create, then an update rewriting matcher and if together.
	instance := validManagedInstanceHook("matcher-gate")
	instance.Matcher = "cron|email.send"
	if err := s.Hooks().CreateInstanceHook(ctx, instance); err != nil {
		t.Fatalf("unexpected instance create error: %v", err)
	}
	instance.Event = domain.HookEventPreToolUse
	instance.Matcher = `^shell\.`
	instance.If = `shell.exec(rm\s+-rf)`
	if err := s.Hooks().UpdateInstanceHook(ctx, instance); err != nil {
		t.Fatalf("unexpected instance update error: %v", err)
	}
	gotInstance, _ := s.Hooks().GetInstanceHook(ctx, instance.ID)
	if gotInstance == nil || gotInstance.Matcher != `^shell\.` || gotInstance.If != `shell.exec(rm\s+-rf)` {
		t.Fatalf("instance matcher/if roundtrip after update: %+v", gotInstance)
	}

	// Builtin upsert: the insert branch, then a version bump exercising the
	// DO UPDATE arm (matcher/if_rule = EXCLUDED.*).
	v1 := validBuiltinInstanceHook("matcher-sync", 1)
	v1.Matcher = "shell"
	if err := s.Hooks().UpsertBuiltinHook(ctx, v1); err != nil {
		t.Fatalf("unexpected builtin v1 upsert error: %v", err)
	}
	stored, _ := s.Hooks().GetInstanceHook(ctx, v1.ID)
	if stored == nil || stored.Matcher != "shell" || stored.If != "" {
		t.Fatalf("builtin v1 matcher/if roundtrip: %+v", stored)
	}
	v2 := validBuiltinInstanceHook("matcher-sync", 2)
	v2.Matcher = `^shell\.(exec|eval)$`
	v2.Event = domain.HookEventPreToolUse
	v2.If = `shell.exec(rm\s)`
	if err := s.Hooks().UpsertBuiltinHook(ctx, v2); err != nil {
		t.Fatalf("unexpected builtin v2 upsert error: %v", err)
	}
	stored, _ = s.Hooks().GetInstanceHook(ctx, v1.ID)
	if stored == nil || stored.ID != v1.ID {
		t.Fatalf("expected identity preserved across version bump, got %+v", stored)
	}
	if stored.Matcher != `^shell\.(exec|eval)$` || stored.If != `shell.exec(rm\s)` {
		t.Fatalf("expected DO UPDATE arm to carry new matcher/if, got (%q, %q)", stored.Matcher, stored.If)
	}
}
