package services

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// commandHook builds a valid command hook bound to a real lifecycle event.
func commandHook(name, event, command string) store.Hook {
	return store.Hook{
		Name:        name,
		Event:       event,
		HandlerType: "command",
		Config:      `{"command": "` + command + `"}`,
	}
}

func TestSettingsService_TestHook_CommandAllowDecision(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	hook, err := svc.CreateHook(ctx, commandHook("echo", "run_started", "echo hello"))
	require.NoError(t, err)

	result, err := svc.TestHook(ctx, hook.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "allow", result.Decision)
	require.Empty(t, result.Err)
}

func TestSettingsService_TestHook_CommandBlockDecision(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	hook, err := svc.CreateHook(ctx, commandHook("guard", "pre_tool_use", "echo no deploys on friday >&2; exit 2"))
	require.NoError(t, err)

	result, err := svc.TestHook(ctx, hook.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "block", result.Decision)
	require.Contains(t, result.Reason, "no deploys on friday")
}

func TestSettingsService_TestHook_CommandFailureReportsError(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	hook, err := svc.CreateHook(ctx, commandHook("fail", "run_finished", "echo boom >&2; exit 3"))
	require.NoError(t, err)

	result, err := svc.TestHook(ctx, hook.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "failure", result.Decision)
	require.Contains(t, result.Err, "exited with code 3")
}

func TestSettingsService_TestHook_ScriptDecisions(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	blocking, err := svc.CreateHook(ctx, store.Hook{
		Name:        "script-guard",
		Event:       "pre_tool_use",
		HandlerType: "script",
		Config:      `{"script": "(function(input){ console.log(input.event); return {decision: \"block\", reason: \"not today\"}; })"}`,
	})
	require.NoError(t, err)

	result, err := svc.TestHook(ctx, blocking.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "block", result.Decision)
	require.Equal(t, "not today", result.Reason)
	require.Contains(t, result.ConsoleLines, "pre_tool_use")

	allowing, err := svc.CreateHook(ctx, store.Hook{
		Name:        "script-ok",
		Event:       "run_started",
		HandlerType: "script",
		Config:      `{"script": "(function(input){ return {decision: \"allow\"}; })"}`,
	})
	require.NoError(t, err)

	result, err = svc.TestHook(ctx, allowing.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "allow", result.Decision)
}

func TestSettingsService_TestHook_NotFound(t *testing.T) {
	svc := NewSettingsService(fake.New())

	_, err := svc.TestHook(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestSettingsService_CreateHook_Validation(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	cases := []struct {
		name string
		hook store.Hook
		want string
	}{
		{
			name: "unknown event",
			hook: store.Hook{Name: "a", Event: "turn_completed", HandlerType: "command", Config: `{"command":"true"}`},
			want: "unknown event",
		},
		{
			name: "empty event",
			hook: store.Hook{Name: "a", Event: "", HandlerType: "command", Config: `{"command":"true"}`},
			want: "unknown event",
		},
		{
			name: "unsupported handler type",
			hook: store.Hook{Name: "a", Event: "run_started", HandlerType: "http", Config: `{}`},
			want: "unsupported handler type",
		},
		{
			name: "command config not JSON",
			hook: store.Hook{Name: "a", Event: "run_started", HandlerType: "command", Config: `not json`},
			want: "not valid JSON",
		},
		{
			name: "command config missing command",
			hook: store.Hook{Name: "a", Event: "run_started", HandlerType: "command", Config: `{}`},
			want: "config.command is required",
		},
		{
			name: "script missing wrapper",
			hook: store.Hook{Name: "a", Event: "run_started", HandlerType: "script", Config: `{"script": "return 1;"}`},
			want: "wrapper",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateHook(ctx, tc.hook)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)

			// The invalid definition never reached the store.
			rows, err := svc.Hooks(ctx)
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}

	// A valid definition still passes.
	_, err := svc.CreateHook(ctx, commandHook("ok", "user_prompt_submit", "true"))
	require.NoError(t, err)
}

func TestSettingsService_UpdateHook_Validation(t *testing.T) {
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	hook, err := svc.CreateHook(ctx, commandHook("ok", "run_started", "true"))
	require.NoError(t, err)

	bad := hook
	bad.Event = "turn_completed"
	_, err = svc.UpdateHook(ctx, bad)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown event")
}

func TestSettingsService_AgentHookAssignment_RoundTrip(t *testing.T) {
	fs := fake.New()
	svc := NewSettingsService(fs)
	ctx := context.Background()

	agent := store.Agent{Name: "assistant"}
	require.NoError(t, fs.Agents().Create(ctx, &agent))

	first, err := svc.CreateHook(ctx, commandHook("first", "run_started", "true"))
	require.NoError(t, err)
	second, err := svc.CreateHook(ctx, commandHook("second", "pre_tool_use", "true"))
	require.NoError(t, err)

	// Nothing assigned yet.
	assignments, err := svc.AgentHooks(ctx, agent.ID)
	require.NoError(t, err)
	require.Empty(t, assignments)

	// Assign out of order; listing honors position order and carries names.
	require.NoError(t, svc.AssignHook(ctx, agent.ID, second.ID, 2))
	require.NoError(t, svc.AssignHook(ctx, agent.ID, first.ID, 1))

	assignments, err = svc.AgentHooks(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	require.Equal(t, first.ID, assignments[0].HookID)
	require.Equal(t, "first", assignments[0].HookName)
	require.Equal(t, 1, assignments[0].Position)
	require.Equal(t, second.ID, assignments[1].HookID)
	require.Equal(t, 2, assignments[1].Position)

	// Re-assigning replaces the position, not the link.
	require.NoError(t, svc.AssignHook(ctx, agent.ID, first.ID, 5))
	assignments, err = svc.AgentHooks(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	require.Equal(t, second.ID, assignments[0].HookID)
	require.Equal(t, 5, assignments[1].Position)

	// Unassign removes the link; re-listing shrinks.
	require.NoError(t, svc.UnassignHook(ctx, agent.ID, second.ID))
	assignments, err = svc.AgentHooks(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, first.ID, assignments[0].HookID)

	// Unknown pairs are ErrNotFound, both ways.
	require.ErrorIs(t, svc.AssignHook(ctx, "ghost", first.ID, 1), store.ErrNotFound)
	require.ErrorIs(t, svc.UnassignHook(ctx, agent.ID, "ghost"), store.ErrNotFound)
}

func TestSettingsService_HookExecutions_NewestFirst(t *testing.T) {
	fs := fake.New()
	svc := NewSettingsService(fs)
	ctx := context.Background()

	for _, outcome := range []string{store.HookOutcomeAllowed, store.HookOutcomeBlocked} {
		row := store.HookExecution{HookID: "h1", Event: "pre_tool_use", Outcome: outcome}
		require.NoError(t, fs.HookExecutions().Record(ctx, &row))
	}

	rows, err := svc.HookExecutions(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, store.HookOutcomeBlocked, rows[0].Outcome)
	require.True(t, strings.EqualFold(rows[0].Event, "pre_tool_use"))
}
