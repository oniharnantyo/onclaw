package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestOpen_MigratesV6DatabaseForward covers the 000007 rollout on an
// existing database: a schema stopped exactly at v6 — with an agent row
// seeded the way the old build left it — migrates forward-only on open, the
// new tables exist, and the pre-existing agent reads back with subagents
// enabled (the column's DEFAULT 1). A second open is a no-op.
func TestOpen_MigratesV6DatabaseForward(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v6.db")

	// Stop the schema exactly at v6 and seed one pre-v7 agent row.
	st, err := openWith(ctx, path, mustEmbeddedMigrations(t)[:6])
	require.NoError(t, err, "seeding the v6-schema fixture must succeed")
	require.NoError(t, st.Close())

	db := openRaw(t, path)
	stamped := formatTime(time.Date(2025, 9, 1, 8, 0, 0, 0, time.UTC))
	_, err = db.ExecContext(ctx, `
INSERT INTO agents (id, name, created_at, updated_at)
VALUES ('legacy-1', 'Legacy', ?, ?)`, stamped, stamped)
	require.NoError(t, err)

	// The full embedded list migrates the database forward.
	st, err = Open(ctx, path)
	require.NoError(t, err, "a v6 database must migrate forward on open")
	t.Cleanup(func() { _ = st.Close() })

	for _, table := range []string{"agent_hooks", "hook_executions", "tool_settings"} {
		require.True(t, tableExists(t, db, table), "table %s must exist after 000007", table)
	}
	has, err := columnExists(ctx, db, "agents", "subagents_enabled")
	require.NoError(t, err)
	require.True(t, has, "000007 must add agents.subagents_enabled")

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 17, version)

	// The pre-v7 agent emerges with subagents enabled: the column default.
	legacy, err := st.Agents().ByID(ctx, "legacy-1")
	require.NoError(t, err)
	require.True(t, legacy.SubagentsEnabled,
		"the column DEFAULT 1 must stamp pre-existing agents as subagents-enabled")

	// Second open: idempotent, no re-application.
	st2, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, st2.Close())
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 17, version, "re-opening must not re-apply migrations")
}

// TestAgents_SubagentsEnabledRoundTrip covers the dedicated subagents
// toggle (design D3): the flag round-trips create/get/update exactly, in
// both directions — it is a plain column, not a normalized default.
func TestAgents_SubagentsEnabledRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas", SubagentsEnabled: true}
	require.NoError(t, s.Agents().Create(ctx, agent))
	got, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.True(t, got.SubagentsEnabled)

	agent.SubagentsEnabled = false
	require.NoError(t, s.Agents().Update(ctx, agent))
	got, err = s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.False(t, got.SubagentsEnabled, "the toggle must turn off through update")

	off := &store.Agent{Name: "nobody-home", Slug: "nobody-home", SubagentsEnabled: false}
	require.NoError(t, s.Agents().Create(ctx, off))
	got, err = s.Agents().ByID(ctx, off.ID)
	require.NoError(t, err)
	require.False(t, got.SubagentsEnabled, "an explicit off is stored verbatim")
}

// TestAgentHooks_AssignUnassignOrdered covers assignment CRUD (task 1.3):
// assign/unassign, re-assign replacing the position, position-ordered
// listings, and the not-found paths.
func TestAgentHooks_AssignUnassignOrdered(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	hookA := &store.Hook{Name: "a-guard", Event: "before_tool_use"}
	hookB := &store.Hook{Name: "b-guard", Event: "before_tool_use"}
	require.NoError(t, s.Hooks().Create(ctx, hookA))
	require.NoError(t, s.Hooks().Create(ctx, hookB))

	// Empty before any assignment, never nil.
	assignments, err := s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, assignments)
	hooks, err := s.AgentHooks().HooksForAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, hooks)

	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hookB.ID, 1))
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hookA.ID, 2))

	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	require.Equal(t, hookB.ID, assignments[0].HookID, "list must follow position order")
	require.Equal(t, 1, assignments[0].Position)
	require.Equal(t, hookA.ID, assignments[1].HookID)

	hooks, err = s.AgentHooks().HooksForAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, hooks, 2)
	require.Equal(t, hookB.ID, hooks[0].ID, "definitions follow the same assignment order")
	require.Equal(t, hookA.ID, hooks[1].ID)

	// Re-assigning a pair moves it instead of duplicating it.
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hookB.ID, 9))
	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2, "re-assign must not duplicate the pair")
	require.Equal(t, hookA.ID, assignments[0].HookID, "b moved behind a")
	require.Equal(t, 9, assignments[1].Position)

	// Unassign removes exactly the pair.
	require.NoError(t, s.AgentHooks().Unassign(ctx, agent.ID, hookB.ID))
	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, hookA.ID, assignments[0].HookID)
	require.ErrorIs(t, s.AgentHooks().Unassign(ctx, agent.ID, hookB.ID), store.ErrNotFound)

	// Assigning an absent agent or hook is the store sentinel.
	require.ErrorIs(t, s.AgentHooks().Assign(ctx, "missing-agent", hookA.ID, 0), store.ErrNotFound)
	require.ErrorIs(t, s.AgentHooks().Assign(ctx, agent.ID, "missing-hook", 0), store.ErrNotFound)

	// Other agents' assignments are untouched.
	other := &store.Agent{Name: "scout", Slug: "scout"}
	require.NoError(t, s.Agents().Create(ctx, other))
	require.NoError(t, s.AgentHooks().Assign(ctx, other.ID, hookA.ID, 1))
	assignments, err = s.AgentHooks().Assignments(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
}

// TestAgentHooks_UnassignedHooks covers the global-tier query (design D4):
// hooks assigned to no agent, position-ordered, and an assignment to any
// agent removing the hook from the global tier for everyone.
func TestAgentHooks_UnassignedHooks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	hookA := &store.Hook{Name: "a-guard", Position: 2}
	hookB := &store.Hook{Name: "b-guard", Position: 1}
	hookC := &store.Hook{Name: "c-guard", Position: 0}
	require.NoError(t, s.Hooks().Create(ctx, hookA))
	require.NoError(t, s.Hooks().Create(ctx, hookB))
	require.NoError(t, s.Hooks().Create(ctx, hookC))

	unassigned, err := s.AgentHooks().UnassignedHooks(ctx)
	require.NoError(t, err)
	require.Len(t, unassigned, 3)
	require.Equal(t, []string{hookC.ID, hookB.ID, hookA.ID},
		[]string{unassigned[0].ID, unassigned[1].ID, unassigned[2].ID},
		"the global tier lists by position")

	// Assigning one hook to one agent removes it from the global tier.
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hookB.ID, 0))
	unassigned, err = s.AgentHooks().UnassignedHooks(ctx)
	require.NoError(t, err)
	require.Len(t, unassigned, 2)
	for _, hook := range unassigned {
		require.NotEqual(t, hookB.ID, hook.ID)
	}

	// Unassigning returns it to the global tier.
	require.NoError(t, s.AgentHooks().Unassign(ctx, agent.ID, hookB.ID))
	unassigned, err = s.AgentHooks().UnassignedHooks(ctx)
	require.NoError(t, err)
	require.Len(t, unassigned, 3)
}

// TestAgentHooks_CascadeOnAgentAndHookDelete covers the cascades of the
// assignment table: deleting the hook or the agent removes the assignment
// rows (the data-store spec's deletion scenarios).
func TestAgentHooks_CascadeOnAgentAndHookDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas"}
	other := &store.Agent{Name: "scout", Slug: "scout"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	require.NoError(t, s.Agents().Create(ctx, other))
	shared := &store.Hook{Name: "redact", Event: "before_tool_use"}
	solo := &store.Hook{Name: "audit", Event: "run_finished"}
	require.NoError(t, s.Hooks().Create(ctx, shared))
	require.NoError(t, s.Hooks().Create(ctx, solo))

	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, shared.ID, 1))
	require.NoError(t, s.AgentHooks().Assign(ctx, other.ID, shared.ID, 1))
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, solo.ID, 2))
	require.NoError(t, s.AgentHooks().Assign(ctx, other.ID, solo.ID, 3))

	// Deleting the hook removes its assignment from every agent.
	require.NoError(t, s.Hooks().Delete(ctx, shared.ID))
	for _, id := range []string{agent.ID, other.ID} {
		assignments, err := s.AgentHooks().Assignments(ctx, id)
		require.NoError(t, err)
		for _, a := range assignments {
			require.NotEqual(t, shared.ID, a.HookID, "the deleted hook's assignments must cascade")
		}
	}

	// Deleting the agent removes its remaining assignments.
	require.NoError(t, s.Agents().Delete(ctx, agent.ID))
	assignments, err := s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, assignments, "the deleted agent's assignments must cascade")

	// The surviving agent's other assignment was untouched throughout.
	assignments, err = s.AgentHooks().Assignments(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, solo.ID, assignments[0].HookID)
	hooks, err := s.AgentHooks().HooksForAgent(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	require.Equal(t, solo.ID, hooks[0].ID)
}

// TestHookExecutions_RecordAndListNewestFirst covers the audit trail:
// store-assigned identity and timestamp, verbatim round-trip of every
// field, and newest-first listing.
func TestHookExecutions_RecordAndListNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	executions, err := s.HookExecutions().List(ctx)
	require.NoError(t, err)
	require.Empty(t, executions, "the trail starts empty but never nil")

	first := &store.HookExecution{
		HookID:     "hook-1",
		Event:      "before_tool_use",
		Outcome:    store.HookOutcomeAllowed,
		DurationMS: 3,
		OccurredAt: time.Date(2025, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, s.HookExecutions().Record(ctx, first))
	require.NotEmpty(t, first.ID, "record must assign the id")

	second := &store.HookExecution{
		HookID:     "hook-1",
		AgentID:    "agent-1",
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		Event:      "before_tool_use",
		Outcome:    store.HookOutcomeBlocked,
		Error:      "",
		DurationMS: 1,
		OccurredAt: time.Date(2025, 10, 1, 12, 0, 1, 0, time.UTC),
	}
	require.NoError(t, s.HookExecutions().Record(ctx, second))

	third := &store.HookExecution{
		HookID:  "hook-2",
		AgentID: "agent-1",
		Event:   "user_prompt_submit",
		Outcome: store.HookOutcomeError,
		Error:   "handler timed out",
	}
	require.NoError(t, s.HookExecutions().Record(ctx, third))
	require.False(t, third.OccurredAt.IsZero(), "record must stamp an empty occurred_at")

	executions, err = s.HookExecutions().List(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 3)
	require.Equal(t, third.ID, executions[0].ID, "the trail lists newest first")
	require.Equal(t, second.ID, executions[1].ID)
	require.Equal(t, first.ID, executions[2].ID)

	// Every stored field survives the round-trip.
	require.Equal(t, "hook-2", executions[0].HookID)
	require.Equal(t, "agent-1", executions[0].AgentID)
	require.Equal(t, "user_prompt_submit", executions[0].Event)
	require.Equal(t, store.HookOutcomeError, executions[0].Outcome)
	require.Equal(t, "handler timed out", executions[0].Error)
	require.Equal(t, second.AgentID, executions[1].AgentID)
	require.Equal(t, "sess-1", executions[1].SessionID)
	require.Equal(t, "turn-1", executions[1].TurnID)
	require.Equal(t, store.HookOutcomeBlocked, executions[1].Outcome)
	require.Equal(t, second.OccurredAt, executions[1].OccurredAt, "a set occurred_at is preserved")
}

// TestToolSettings_SetGetPerKey covers the per-tool settings store
// (design D8): absent reads, atomic upsert per key, and keys not
// interfering with each other.
func TestToolSettings_SetGetPerKey(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	settings, ok, err := s.ToolSettings().Get(ctx, "web_search")
	require.NoError(t, err)
	require.False(t, ok, "a tool with no stored row has no settings")
	require.Empty(t, settings)

	require.NoError(t, s.ToolSettings().Set(ctx, "web_search", `{"providers":["brave","exa"]}`))
	settings, ok, err = s.ToolSettings().Get(ctx, "web_search")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"providers":["brave","exa"]}`, settings)

	// A second key is its own row.
	require.NoError(t, s.ToolSettings().Set(ctx, "web_fetch", `{"max_bytes":4096}`))
	settings, ok, err = s.ToolSettings().Get(ctx, "web_fetch")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"max_bytes":4096}`, settings)

	// Upserting one key leaves the other untouched.
	require.NoError(t, s.ToolSettings().Set(ctx, "web_search", `{"providers":["searxng"]}`))
	settings, ok, err = s.ToolSettings().Get(ctx, "web_search")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"providers":["searxng"]}`, settings)
	settings, ok, err = s.ToolSettings().Get(ctx, "web_fetch")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"max_bytes":4096}`, settings)

	_, ok, err = s.ToolSettings().Get(ctx, "unknown_tool")
	require.NoError(t, err)
	require.False(t, ok)
}
