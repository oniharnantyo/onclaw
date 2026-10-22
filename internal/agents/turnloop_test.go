package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The tool-loop tests (tasks.md 1.1): the composed ADK agent executes the
// tools the model calls, feeds results back, and the transcript records each
// call's terminal state — driven end to end through the Manager with the
// fake model and the real catalog tools.

// toolCallsReply returns a fakeReply carrying one tool call.
func toolCallsReply(id, name, arguments string) fakeReply {
	return fakeReply{toolCalls: []schema.ToolCall{
		{ID: id, Function: schema.FunctionCall{Name: name, Arguments: arguments}},
	}}
}

// TestTurn_ToolCallRoundTrip is the core acceptance path: the model calls
// current_time, the tool really executes, the JSON result is fed back as a
// tool message, and the model's follow-up answer closes the turn.
func TestTurn_ToolCallRoundTrip(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return toolCallsReply("call-1", ToolCurrentTime, `{}`)
		}
		// The follow-up is only reachable if the tool result is in context.
		sawToolResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, `"utc"`) {
				sawToolResult = true
			}
		}
		if !sawToolResult {
			return fakeReply{err: errors.New("the tool result never reached the model")}
		}
		return fakeReply{text: "the time is known", promptTokens: 30, completionTokens: 5}
	})
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what time is it?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the round trip must complete")

	// The model ran the loop: call 1 proposed the tool, call 2 saw the result.
	require.GreaterOrEqual(t, calls, 2)
	secondCall := f.model.calls()[1]
	var sawToolCall, sawToolMessage bool
	for _, msg := range secondCall {
		if msg.Role == schema.Assistant && len(msg.ToolCalls) == 1 && msg.ToolCalls[0].Function.Name == ToolCurrentTime {
			sawToolCall = true
		}
		if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
			sawToolMessage = true
		}
	}
	require.True(t, sawToolCall, "the tool call rides the follow-up input")
	require.True(t, sawToolMessage, "the tool result rides the follow-up input as a tool message")

	// The transcript carries the terminal tool record with the real result.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	tool := events[1].Tool
	require.NotNil(t, tool)
	require.Equal(t, "call-1", tool.CallID)
	require.Equal(t, ToolCurrentTime, tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
	require.NotEmpty(t, tool.Result, "the real execution result is persisted")
	require.Contains(t, tool.Result, `"utc"`)
	require.False(t, tool.IsError)

	// The live stream showed running first, then the terminal finish.
	live := collector.Wait()
	var liveTools []sessions.Event
	for _, ev := range live {
		if ev.Kind == sessions.KindToolActivity {
			liveTools = append(liveTools, ev)
		}
	}
	require.Len(t, liveTools, 2, "a running and a terminal record ride the live stream")
	require.Equal(t, sessions.ToolStatusRunning, liveTools[0].Tool.Status)
	require.Empty(t, liveTools[0].ID, "the running form is live-only")
	require.Equal(t, sessions.ToolStatusCompleted, liveTools[1].Tool.Status)
	require.Equal(t, liveTools[0].Tool.CallID, liveTools[1].Tool.CallID)
	require.NotEmpty(t, liveTools[1].ID, "the terminal record carries its store id")
	require.Equal(t, tool.Result, liveTools[1].Tool.Result)
}

// TestTurn_ToolFailureFeedsBack pins the failure contract: a failing tool
// returns its error to the model as the tool result and the turn continues —
// the transcript records the failed terminal state with the error text.
func TestTurn_ToolFailureFeedsBack(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			// http_get rejects non-http schemes: a deterministic tool failure.
			return toolCallsReply("call-1", ToolHTTPGet, `{"url":"file:///etc/passwd"}`)
		}
		sawErrorResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && strings.Contains(msg.Content, "unsupported URL scheme") {
				sawErrorResult = true
			}
		}
		if !sawErrorResult {
			return fakeReply{err: errors.New("the failure never reached the model")}
		}
		return fakeReply{text: "the fetch failed as reported", promptTokens: 20, completionTokens: 5}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "fetch that file")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a tool failure must not fail the turn")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	tool := events[1].Tool
	require.NotNil(t, tool)
	require.Equal(t, sessions.ToolStatusFailed, tool.Status)
	require.True(t, tool.IsError)
	require.Contains(t, tool.Result, "unsupported URL scheme")
	require.Equal(t, `{"url":"file:///etc/passwd"}`, tool.Arguments)
}

// TestTurn_ToolFailureLoopContinues proves the model can keep using tools
// after a failure: a failing first call, a successful second one, then the
// answer — three model calls, one failed and one completed tool record.
func TestTurn_ToolFailureLoopContinues(t *testing.T) {
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		switch calls {
		case 1:
			return toolCallsReply("call-1", ToolHTTPGet, `{"url":"not-a-url"}`)
		case 2:
			return toolCallsReply("call-2", ToolCurrentTime, `{}`)
		default:
			return fakeReply{text: "recovered", promptTokens: 30, completionTokens: 5}
		}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "try, then tell the time")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 2)
	require.Equal(t, sessions.ToolStatusFailed, tools[0].Tool.Status)
	require.Equal(t, "call-1", tools[0].Tool.CallID)
	require.Equal(t, sessions.ToolStatusCompleted, tools[1].Tool.Status)
	require.Equal(t, "call-2", tools[1].Tool.CallID)
	require.Equal(t, "recovered", events[len(events)-2].Text)
}

// TestTurn_IterationCapNotice pins the bounded runaway loop (spec
// agent-tools): a model that never stops calling tools ends the turn with a
// visible cap notice — the turn succeeds, the notice is the final assistant
// message, and the model was stopped at the runtime cap.
func TestTurn_IterationCapNotice(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		return toolCallsReply(fmt.Sprintf("call-%d", n), ToolCurrentTime, `{}`)
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "never stop")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the cap ends the turn with a notice, not a failure")

	// The bound held: exactly the runtime constant's worth of model calls.
	mu.Lock()
	got := calls
	mu.Unlock()
	require.Equal(t, DefaultMaxIterations, got)

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotContains(t, kindsOf(events), string(sessions.KindTurnError))
	require.Equal(t, sessions.KindAssistantMessage, events[len(events)-2].Kind,
		"the notice is the turn's final durable word")
	require.Contains(t, events[len(events)-2].Text, iterationCapNotice())
	completed := events[len(events)-1]
	require.Equal(t, sessions.KindTurnCompleted, completed.Kind)

	// The tools the runaway loop did run are on the transcript as completed.
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, DefaultMaxIterations)
	for _, ev := range tools {
		require.Equal(t, sessions.ToolStatusCompleted, ev.Tool.Status)
	}
}

// TestTurn_WorkspaceToolWritesFiles proves the jailed filesystem is real
// inside a turn: the model writes and then reads a file through the fs
// tools, and the bytes land in the agent's workspace directory on disk.
func TestTurn_WorkspaceToolWritesFiles(t *testing.T) {
	root := t.TempDir()
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		switch calls {
		case 1:
			return toolCallsReply("call-1", ToolWriteFile,
				`{"file_path":"/workspace/notes.md","content":"from the tool"}`)
		case 2:
			return toolCallsReply("call-2", ToolReadFile, `{"file_path":"/workspace/notes.md"}`)
		default:
			for _, msg := range input {
				if msg.Role == schema.Tool && strings.Contains(msg.Content, "from the tool") {
					return fakeReply{text: "the file says: from the tool", promptTokens: 30, completionTokens: 5}
				}
			}
			return fakeReply{err: errors.New("the written file content never reached the model")}
		}
	}, WithWorkspaces(workspaces.NewManager(root)))

	// The fixture agent has no slug; give it one so it owns a workspace.
	ctx := t.Context()
	agent := f.agent
	agent.Slug = "tester"
	require.NoError(t, f.store.Agents().Update(ctx, &agent))
	f.agent = agent

	sess, err := f.mgr.CreateSession(ctx, agent.ID)
	require.NoError(t, err)

	turn, err := f.mgr.Send(ctx, sess.ID, "write and read back a note")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 2)
	require.Equal(t, sessions.ToolStatusCompleted, tools[0].Tool.Status)
	require.Equal(t, ToolWriteFile, tools[0].Tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tools[1].Tool.Status)
	require.Equal(t, ToolReadFile, tools[1].Tool.Name)

	// The bytes are on disk inside the workspace jail.
	content, err := os.ReadFile(filepath.Join(root, "tester", "notes.md"))
	require.NoError(t, err)
	require.Equal(t, "from the tool", string(content))
}

// TestTurn_NoToolCallsBehavesAsToday is the regression guard: with a model
// that never calls tools, the turn transcript is byte-identical to the
// pre-port shape — user message, assistant message, completion.
func TestTurn_NoToolCallsBehavesAsToday(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "plain answer", promptTokens: 10, completionTokens: 2}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, "plain answer", events[1].Text)
	require.NotNil(t, events[2].Usage)
	require.Equal(t, 12, events[2].Usage.TotalTokens)
}
