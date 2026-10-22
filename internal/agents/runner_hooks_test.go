package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The hook-dispatch tests (tasks.md 4.4): the turn pipeline fires all five
// lifecycle points through the real dispatcher over the fake store — a
// blocked prompt makes no model call, a blocked tool never executes, a
// failing hook never crashes the turn, the detached observers land, and
// every evaluation is audited.

// wireRealHooks swaps the fixture's no-op dispatcher for the real one over
// the fake store, mirroring the composition root's construction.
func (f *fixture) wireRealHooks() {
	f.mgr.hooks = hooks.NewDispatcher(storeHooks{store: f.store}, hooks.NewRegistry())
}

// seedHook inserts one hook definition for the turn pipeline to resolve.
func seedHook(t *testing.T, fs *fake.Store, hook store.Hook) store.Hook {
	t.Helper()
	require.NoError(t, fs.Hooks().Create(t.Context(), &hook))
	return hook
}

// auditedHooks returns the audit trail; it must never be nil.
func auditedHooks(t *testing.T, fs *fake.Store) []store.HookExecution {
	t.Helper()
	execs, err := fs.HookExecutions().List(t.Context())
	require.NoError(t, err)
	return execs
}

// TestTurn_HookBlocksPrompt_NoModelCall pins the user_prompt_submit gate
// (spec agent-hooks "Blocked prompt does not start a turn"): the block ends
// the turn with a user-visible notice carrying the hook's reason, the model
// is never called, the turn still completes normally, the observers see the
// full lifecycle (the ported blocked-turn shape), and the evaluation is
// audited as blocked with the turn's coordinates.
func TestTurn_HookBlocksPrompt_NoModelCall(t *testing.T) {
	markers := filepath.Join(t.TempDir(), "markers")
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "the model must never be asked"}
	})
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "gate",
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo this prompt is not allowed >&2; exit 2"}`,
		Enabled:     true,
	})
	// The observers must fire on the blocked path too (onclaw's full
	// lifecycle shape), detached.
	seedHook(t, f.store, store.Hook{
		Name:        "saw-start",
		Event:       hooks.EventRunStarted,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo started >> ` + markers + `"}`,
		Enabled:     true,
	})
	seedHook(t, f.store, store.Hook{
		Name:        "saw-finish",
		Event:       hooks.EventRunFinished,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo finished >> ` + markers + `"}`,
		Enabled:     true,
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a hook block is a normal turn end, not a failure")

	require.Empty(t, f.model.calls(), "a blocked prompt makes no model call")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Contains(t, events[1].Text, `Prompt blocked by hook "gate"`)
	require.Contains(t, events[1].Text, "this prompt is not allowed")

	execs := auditedHooks(t, f.store)
	require.Len(t, execs, 1)
	require.Equal(t, hooks.OutcomeBlocked, execs[0].Outcome)
	require.Equal(t, hooks.EventUserPromptSubmit, execs[0].Event)
	require.Equal(t, hookIDOf(t, f.store, "gate"), execs[0].HookID)
	require.Equal(t, f.agent.ID, execs[0].AgentID)
	require.Equal(t, sess.ID, execs[0].SessionID)
	require.Equal(t, events[0].TurnID, execs[0].TurnID)

	// The detached observers land on their own goroutines, after the turn
	// has already completed — and in either order, since each delivery is
	// its own detached process.
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(markers)
		if err != nil {
			return false
		}
		s := string(b)
		return strings.Contains(s, "started\n") && strings.Contains(s, "finished\n")
	}, 5*time.Second, 20*time.Millisecond, "the detached observers must both land")
}

// TestTurn_HookBlocksTool_ToolNeverExecutes pins the pre_tool_use gate (spec
// agent-hooks "Blocked tool never executes"): the tool does not run, the
// model reads the canonical block result as the call's result, the
// transcript records the blocked call with the hook's reason, and the
// evaluation is audited as blocked.
func TestTurn_HookBlocksTool_ToolNeverExecutes(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", ToolCurrentTime, `{}`)
		}
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				if strings.Contains(msg.Content, "blocked_by_hook") {
					return fakeReply{text: "understood, no clock reading", promptTokens: 10, completionTokens: 3}
				}
				return fakeReply{err: errors.New("the model did not receive the block result")}
			}
		}
		return fakeReply{err: errors.New("no tool result reached the model")}
	})
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "no-clock",
		Event:       hooks.EventPreToolUse,
		Matcher:     ToolCurrentTime,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo the clock is off limits >&2; exit 2"}`,
		Enabled:     true,
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what time is it?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a blocked tool is turn data, not a run failure")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 1)
	tool := tools[0].Tool
	require.NotNil(t, tool)
	require.Equal(t, "call-1", tool.CallID)
	// The block payload is the result — the real tool output ("utc") is
	// absent because the tool never executed.
	require.Contains(t, tool.Result, `"blocked_by_hook":true`)
	require.Contains(t, tool.Result, "the clock is off limits")
	require.NotContains(t, tool.Result, `"utc"`)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status, "the block rides as the result, not a tool failure")
	require.False(t, tool.IsError)

	execs := auditedHooks(t, f.store)
	require.Len(t, execs, 1)
	require.Equal(t, hooks.OutcomeBlocked, execs[0].Outcome)
	require.Equal(t, hooks.EventPreToolUse, execs[0].Event)
}

// TestTurn_FailingHookDoesNotBreakTurn pins the failure policy (spec
// agent-hooks "Failing hook does not break the turn"): a handler that fails
// under the default fail-open policy never disturbs the turn — the tool
// runs, the answer lands — and the failure is audited.
func TestTurn_FailingHookDoesNotBreakTurn(t *testing.T) {
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", ToolCurrentTime, `{}`)
		}
		return fakeReply{text: "the time is known", promptTokens: 10, completionTokens: 3}
	})
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "broken-gate",
		Event:       hooks.EventPreToolUse,
		Matcher:     ToolCurrentTime,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"exit 3"}`,
		Enabled:     true,
		// OnFailure "" is the fail-open default.
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what time is it?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a failing hook must not crash the turn")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 1)
	require.Equal(t, sessions.ToolStatusCompleted, tools[0].Tool.Status)
	require.Contains(t, tools[0].Tool.Result, `"utc"`, "the tool really executed")
	require.NotContains(t, kindsOf(events), string(sessions.KindTurnError))

	execs := auditedHooks(t, f.store)
	require.Len(t, execs, 1)
	require.Equal(t, hooks.OutcomeError, execs[0].Outcome)
	require.Contains(t, execs[0].Error, "exited with code 3")
}

// TestTurn_HookObserversFireDetached pins the observational points: a plain
// turn with a tool call fires run_started before the run, post_tool_use
// after the execution, and run_finished once the terminal transcript event
// settled — all detached, so the turn's own completion never waits on them.
func TestTurn_HookObserversFireDetached(t *testing.T) {
	markers := filepath.Join(t.TempDir(), "markers")
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", ToolCurrentTime, `{}`)
		}
		return fakeReply{text: "done", promptTokens: 10, completionTokens: 3}
	})
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "saw-start",
		Event:       hooks.EventRunStarted,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo started >> ` + markers + `"}`,
		Enabled:     true,
	})
	seedHook(t, f.store, store.Hook{
		Name:        "saw-post",
		Event:       hooks.EventPostToolUse,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo posted >> ` + markers + `"}`,
		Enabled:     true,
	})
	seedHook(t, f.store, store.Hook{
		Name:        "saw-finish",
		Event:       hooks.EventRunFinished,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo finished >> ` + markers + `"}`,
		Enabled:     true,
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "tell the time")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	require.Eventually(t, func() bool {
		b, err := os.ReadFile(markers)
		if err != nil {
			return false
		}
		s := string(b)
		return strings.Contains(s, "started\n") && strings.Contains(s, "posted\n") && strings.Contains(s, "finished\n")
	}, 5*time.Second, 20*time.Millisecond, "all three observers must land")

	// Every evaluation is audited: start, post, finish.
	require.Len(t, auditedHooks(t, f.store), 3)
}

// hookIDOf resolves a hook's store id by name (ids are store-assigned).
func hookIDOf(t *testing.T, fs *fake.Store, name string) string {
	t.Helper()
	hooks, err := fs.Hooks().List(t.Context())
	require.NoError(t, err)
	for _, h := range hooks {
		if h.Name == name {
			return h.ID
		}
	}
	t.Fatalf("hook %q not found", name)
	return ""
}
