package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestAgentHooks_AssignUnassignOrdered covers the fake's assignment store:
// assign/unassign with explicit IDs, position ordering (hook ID as the
// tiebreak), re-assign replacing the position, and the not-found paths
// mirroring the sqlite adapter.
func TestAgentHooks_AssignUnassignOrdered(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	hookA := &store.Hook{ID: "hook-a", Name: "a-guard", Event: "before_tool_use"}
	hookB := &store.Hook{ID: "hook-b", Name: "b-guard", Event: "before_tool_use"}
	require.NoError(t, s.Hooks().Create(ctx, hookA))
	require.NoError(t, s.Hooks().Create(ctx, hookB))

	// Assigning an absent agent or hook is the store sentinel.
	require.ErrorIs(t, s.AgentHooks().Assign(ctx, "missing-agent", hookA.ID, 0), store.ErrNotFound)
	require.ErrorIs(t, s.AgentHooks().Assign(ctx, agent.ID, "missing-hook", 0), store.ErrNotFound)

	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, "hook-b", 2))
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, "hook-a", 1))

	assignments, err := s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	require.Equal(t, "hook-a", assignments[0].HookID, "list must follow position order")
	require.Equal(t, "hook-b", assignments[1].HookID)

	// Equal positions fall back to hook ID order.
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, "hook-b", 1))
	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2, "re-assign must not duplicate the pair")
	require.Equal(t, "hook-a", assignments[0].HookID)
	require.Equal(t, "hook-b", assignments[1].HookID, "the hook ID breaks the position tie")

	hooks, err := s.AgentHooks().HooksForAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, hooks, 2)
	require.Equal(t, "a-guard", hooks[0].Name)
	require.Equal(t, "hook-b", hooks[1].ID)

	require.NoError(t, s.AgentHooks().Unassign(ctx, agent.ID, "hook-a"))
	require.ErrorIs(t, s.AgentHooks().Unassign(ctx, agent.ID, "hook-a"), store.ErrNotFound)

	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, "hook-b", assignments[0].HookID)
}

// TestAgentHooks_CascadeOnAgentAndHookDelete covers the fake's cascades:
// deleting the hook or the agent removes the assignment rows.
func TestAgentHooks_CascadeOnAgentAndHookDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	hook := &store.Hook{ID: "hook-1", Name: "redact", Event: "before_tool_use"}
	require.NoError(t, s.Hooks().Create(ctx, hook))
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hook.ID, 1))

	// Deleting the hook takes the assignment with it.
	require.NoError(t, s.Hooks().Delete(ctx, hook.ID))
	assignments, err := s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, assignments)

	// Reassign, then delete the agent: the assignment goes too.
	require.NoError(t, s.Hooks().Create(ctx, hook))
	require.NoError(t, s.AgentHooks().Assign(ctx, agent.ID, hook.ID, 1))
	require.NoError(t, s.Agents().Delete(ctx, agent.ID))
	assignments, err = s.AgentHooks().Assignments(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, assignments)
}

// TestHookExecutions_RecordAndListNewestFirst covers the fake's audit
// trail: store-assigned id and timestamp, verbatim round-trip, and
// newest-first listing.
func TestHookExecutions_RecordAndListNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	first := &store.HookExecution{
		HookID:     "hook-1",
		Event:      "run_started",
		Outcome:    store.HookOutcomeAllowed,
		DurationMS: 2,
		OccurredAt: time.Date(2025, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, s.HookExecutions().Record(ctx, first))
	require.NotEmpty(t, first.ID)

	second := &store.HookExecution{
		HookID:     "hook-1",
		AgentID:    "agent-1",
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		Event:      "before_tool_use",
		Outcome:    store.HookOutcomeBlocked,
		Error:      "",
		DurationMS: 1,
	}
	require.NoError(t, s.HookExecutions().Record(ctx, second))
	require.False(t, second.OccurredAt.IsZero(), "record must stamp an empty occurred_at")

	// Equal timestamps keep the newest-inserted first (the sqlite rowid
	// tiebreak).
	third := &store.HookExecution{
		HookID:     "hook-2",
		Event:      "after_tool_use",
		Outcome:    store.HookOutcomeError,
		Error:      "boom",
		OccurredAt: second.OccurredAt,
	}
	require.NoError(t, s.HookExecutions().Record(ctx, third))

	executions, err := s.HookExecutions().List(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 3)
	require.Equal(t, third.ID, executions[0].ID)
	require.Equal(t, second.ID, executions[1].ID)
	require.Equal(t, first.ID, executions[2].ID)
	require.Equal(t, store.HookOutcomeError, executions[0].Outcome)
	require.Equal(t, "boom", executions[0].Error)
	require.Equal(t, "sess-1", executions[1].SessionID)
	require.Equal(t, "turn-1", executions[1].TurnID)
	require.Equal(t, first.OccurredAt, executions[2].OccurredAt)
}

// TestAgentHooks_UnassignedHooks covers the global tier (design D4): hooks
// assigned to no agent, position-ordered, shrinking as any agent assigns —
// assignment removes a hook from the global tier for everyone.
func TestAgentHooks_UnassignedHooks(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	hookA := &store.Hook{ID: "hook-a", Name: "a-guard", Position: 2}
	hookB := &store.Hook{ID: "hook-b", Name: "b-guard", Position: 1}
	hookC := &store.Hook{ID: "hook-c", Name: "c-guard", Position: 0}
	require.NoError(t, s.Hooks().Create(ctx, hookA))
	require.NoError(t, s.Hooks().Create(ctx, hookB))
	require.NoError(t, s.Hooks().Create(ctx, hookC))

	unassigned, err := s.AgentHooks().UnassignedHooks(ctx)
	require.NoError(t, err)
	require.Len(t, unassigned, 3)
	require.Equal(t, []string{"hook-c", "hook-b", "hook-a"},
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

// TestToolSettings_SetGetPerKey covers the fake's per-tool settings store:
// absent reads, upsert per key, and keys not interfering with each other.
func TestToolSettings_SetGetPerKey(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	settings, ok, err := s.ToolSettings().Get(ctx, "web_search")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, settings)

	require.NoError(t, s.ToolSettings().Set(ctx, "web_search", `{"providers":["brave"]}`))
	require.NoError(t, s.ToolSettings().Set(ctx, "web_fetch", `{"max_bytes":1024}`))
	require.NoError(t, s.ToolSettings().Set(ctx, "web_search", `{"providers":["exa"]}`))

	settings, ok, err = s.ToolSettings().Get(ctx, "web_search")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"providers":["exa"]}`, settings)

	settings, ok, err = s.ToolSettings().Get(ctx, "web_fetch")
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, `{"max_bytes":1024}`, settings)

	_, ok, err = s.ToolSettings().Get(ctx, "unknown_tool")
	require.NoError(t, err)
	require.False(t, ok)
}
