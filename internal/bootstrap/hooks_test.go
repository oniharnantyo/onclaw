package bootstrap

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/hooks/builtin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// httpHookBase is a valid http-handler hook body used across the sync tests.
func httpHookBase(name string, timeoutMS int) domain.HookBase {
	return domain.HookBase{
		Name:        name,
		Event:       domain.HookEventRunFinished,
		Matcher:     "failed",
		HandlerType: domain.HookHandlerHTTP,
		Config:      json.RawMessage(`{"url":"https://ops.example.com/hook"}`),
		TimeoutMS:   timeoutMS,
		OnFailure:   domain.HookFailureAllow,
		Enabled:     true,
	}
}

// seedBuiltin inserts a source=builtin row through the store's sync-pipeline
// method, exactly the state a previous binary's sync would have left behind.
func seedBuiltin(t *testing.T, ctx context.Context, st store.Store, key string, version int, base domain.HookBase) domain.InstanceHook {
	t.Helper()
	hook := domain.InstanceHook{HookBase: base, Key: key, Source: domain.HookSourceBuiltin, Version: version}
	if err := st.Hooks().UpsertBuiltinHook(ctx, &hook); err != nil {
		t.Fatalf("seed builtin %q v%d: %v", key, version, err)
	}
	return hook
}

func TestSyncBuiltinHooks_EmptyRegistryIsNoop(t *testing.T) {
	ctx := context.Background()

	// A fresh store: the sync succeeds and materializes nothing.
	st := fake.New()
	if err := SyncBuiltinHooks(ctx, st); err != nil {
		t.Fatalf("sync with empty registry failed: %v", err)
	}
	hooks, err := st.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("list instance hooks: %v", err)
	}
	if len(hooks) != 0 {
		t.Fatalf("expected no instance hooks after empty sync, got %d", len(hooks))
	}

	// Managed rows are never the sync's business.
	managed := &domain.InstanceHook{
		HookBase: httpHookBase("Ops Webhook", 5000),
		Key:      "ops-webhook", Source: domain.HookSourceManaged, Version: 1,
	}
	if err := st.Hooks().CreateInstanceHook(ctx, managed); err != nil {
		t.Fatalf("create managed instance hook: %v", err)
	}
	if err := SyncBuiltinHooks(ctx, st); err != nil {
		t.Fatalf("sync with managed rows present failed: %v", err)
	}
	hooks, err = st.Hooks().ListInstanceHooks(ctx)
	if err != nil {
		t.Fatalf("list instance hooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].ID != managed.ID || hooks[0].Source != domain.HookSourceManaged {
		t.Fatalf("managed row did not survive the empty sync unchanged: %+v", hooks)
	}
}

func TestSyncBuiltinHooks_RemovesStaleBuiltins_ExecutionsSurvive(t *testing.T) {
	ctx := context.Background()
	st := fake.New()

	// A builtin row shipped by an older binary and one execution against it.
	stale := seedBuiltin(t, ctx, st, "legacy-gate", 3, httpHookBase("Legacy Gate", 5000))
	hookID := stale.ID
	exec := &domain.HookExecution{
		HookID:      &hookID,
		HookName:    stale.Name,
		HookLevel:   domain.HookLevelInstance,
		WorkspaceID: "ws-exec",
		Event:       domain.HookEventRunFinished,
		Decision:    "allow",
	}
	if err := st.Hooks().RecordHookExecution(ctx, exec); err != nil {
		t.Fatalf("record hook execution: %v", err)
	}

	// A managed row that must be untouched by the removal pass.
	managed := &domain.InstanceHook{
		HookBase: httpHookBase("Ops Webhook", 5000),
		Key:      "ops-webhook", Source: domain.HookSourceManaged, Version: 1,
	}
	if err := st.Hooks().CreateInstanceHook(ctx, managed); err != nil {
		t.Fatalf("create managed instance hook: %v", err)
	}

	// v1 ships zero builtins, so the sync deletes the stale row.
	if err := SyncBuiltinHooks(ctx, st); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	gone, err := st.Hooks().GetInstanceHook(ctx, stale.ID)
	if err != nil {
		t.Fatalf("get stale builtin: %v", err)
	}
	if gone != nil {
		t.Fatalf("stale builtin row survived the sync: %+v", gone)
	}
	kept, err := st.Hooks().GetInstanceHook(ctx, managed.ID)
	if err != nil {
		t.Fatalf("get managed row: %v", err)
	}
	if kept == nil {
		t.Fatalf("managed row was deleted by the sync")
	}

	// The execution record survives with hook_id detached and the
	// denormalized name intact (ON DELETE SET NULL + hook_name, D16).
	execs, err := st.Hooks().ListHookExecutions(ctx, "ws-exec", nil, 0)
	if err != nil {
		t.Fatalf("list hook executions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected the execution record to survive, got %d", len(execs))
	}
	if execs[0].HookID != nil {
		t.Fatalf("expected hook_id to be detached (nil), got %q", *execs[0].HookID)
	}
	if execs[0].HookName != "Legacy Gate" || execs[0].HookLevel != domain.HookLevelInstance {
		t.Fatalf("denormalized identity lost: name=%q level=%q", execs[0].HookName, execs[0].HookLevel)
	}
}

func TestSyncBuiltinHooks_UpgradesOlderVersion(t *testing.T) {
	ctx := context.Background()
	st := fake.New()

	old := seedBuiltin(t, ctx, st, "export-gate", 1, httpHookBase("Export Gate", 1000))

	defs := []builtin.Definition{{
		Key:     "export-gate",
		Version: 2,
		Hook:    domain.InstanceHook{HookBase: httpHookBase("Export Gate", 5000)},
	}}
	if err := syncBuiltinHookDefs(ctx, st, defs); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	got, err := st.Hooks().GetInstanceHook(ctx, old.ID)
	if err != nil {
		t.Fatalf("get upgraded hook: %v", err)
	}
	if got == nil {
		t.Fatalf("upgraded hook missing")
	}
	if got.Version != 2 || got.Name != "Export Gate" || got.TimeoutMS != 5000 {
		t.Fatalf("expected in-place upgrade to v2 content, got v%d name=%q timeout=%d", got.Version, got.Name, got.TimeoutMS)
	}
	if got.ID != old.ID || !got.CreatedAt.Equal(old.CreatedAt) {
		t.Fatalf("identity fields must persist across an upgrade: id %q -> %q", old.ID, got.ID)
	}
	if got.Source != domain.HookSourceBuiltin || got.Key != "export-gate" {
		t.Fatalf("source/key corrupted by sync: source=%q key=%q", got.Source, got.Key)
	}
}

func TestSyncBuiltinHooks_DeterministicIDs(t *testing.T) {
	ctx := context.Background()

	// Two fresh stores running the same sync must mint the same primary key:
	// concurrent starts race only through the (source,key) conflict, never
	// through divergent IDs.
	ids := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		st := fake.New()
		defs := []builtin.Definition{{
			Key:     "export-gate",
			Version: 1,
			Hook:    domain.InstanceHook{HookBase: httpHookBase("Export Gate", 5000)},
		}}
		if err := syncBuiltinHookDefs(ctx, st, defs); err != nil {
			t.Fatalf("sync %d failed: %v", i, err)
		}
		hooks, err := st.Hooks().ListInstanceHooks(ctx)
		if err != nil {
			t.Fatalf("list instance hooks: %v", err)
		}
		if len(hooks) != 1 {
			t.Fatalf("sync %d materialized %d rows, want 1", i, len(hooks))
		}
		ids = append(ids, hooks[0].ID)
	}
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("builtin IDs are not deterministic across stores: %q vs %q", ids[0], ids[1])
	}
}

func TestSyncBuiltinHooks_NeverDowngrades(t *testing.T) {
	ctx := context.Background()
	st := fake.New()

	// A row already newer than what this binary ships (rolling-deploy skew).
	newer := seedBuiltin(t, ctx, st, "export-gate", 9, httpHookBase("Export Gate v9", 5000))

	defs := []builtin.Definition{{
		Key:     "export-gate",
		Version: 2,
		Hook:    domain.InstanceHook{HookBase: httpHookBase("Export Gate v2", 5000)},
	}}
	if err := syncBuiltinHookDefs(ctx, st, defs); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	got, err := st.Hooks().GetInstanceHook(ctx, newer.ID)
	if err != nil {
		t.Fatalf("get hook: %v", err)
	}
	if got == nil {
		t.Fatalf("newer row vanished")
	}
	if got.Version != 9 || got.Name != "Export Gate v9" {
		t.Fatalf("sync downgraded a newer stored row: v%d name=%q", got.Version, got.Name)
	}
}

func TestSyncBuiltinHooks_Idempotent(t *testing.T) {
	ctx := context.Background()
	st := fake.New()

	defs := []builtin.Definition{
		{Key: "export-gate", Version: 2, Hook: domain.InstanceHook{HookBase: httpHookBase("Export Gate", 5000)}},
		{Key: "prompt-gate", Version: 4, Hook: domain.InstanceHook{HookBase: httpHookBase("Prompt Gate", 5000)}},
	}

	snapshot := func() []domain.InstanceHook {
		t.Helper()
		hooks, err := st.Hooks().ListInstanceHooks(ctx)
		if err != nil {
			t.Fatalf("list instance hooks: %v", err)
		}
		return hooks
	}

	if err := syncBuiltinHookDefs(ctx, st, defs); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	first := snapshot()
	if len(first) != 2 {
		t.Fatalf("expected 2 builtin rows after sync, got %d", len(first))
	}

	if err := syncBuiltinHookDefs(ctx, st, defs); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	second := snapshot()

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("sync is not idempotent:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}
