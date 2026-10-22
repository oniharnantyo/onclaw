package agents

import (
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The Runner-swap smoke test (tasks.md 4.1, design D4 risk register): a
// normal turn through the checkpointed adk.Runner produces the exact
// event-kind sequence the direct turnAgent.Run produced before the swap —
// live-only running announcements and durable records alike — and the usage
// accounting is unchanged.

// TestRunnerSwap_SmokeEventSequence pins the post-swap behavior: a streaming
// tool-call round trip lands user message, terminal tool record, assistant
// message, completion — in that order, with the real result and the same
// live stream shape (running first, terminal second) as before the swap.
func TestRunnerSwap_SmokeEventSequence(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return fakeReply{
				reasoning: "thinking about the clock",
				toolCalls: []schema.ToolCall{
					{ID: "call-1", Function: schema.FunctionCall{Name: ToolCurrentTime, Arguments: `{}`}},
				},
				promptTokens: 12, completionTokens: 3,
			}
		}
		sawToolResult := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawToolResult = true
			}
		}
		if !sawToolResult {
			return fakeReply{err: errToolResultMissing}
		}
		return fakeReply{text: "the time is known", promptTokens: 30, completionTokens: 5}
	})
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what time is it?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	// The durable event-kind sequence is exactly the pre-swap shape.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindReasoning),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))

	// The terminal tool record carries the real execution result.
	tool := events[2].Tool
	require.NotNil(t, tool)
	require.Equal(t, "call-1", tool.CallID)
	require.Equal(t, ToolCurrentTime, tool.Name)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
	require.Contains(t, tool.Result, `"utc"`)
	require.Equal(t, "the time is known", events[3].Text)

	// Usage rode the streamed frames through the Runner: both model calls
	// summed (12+3 + 30+5).
	require.NotNil(t, events[4].Usage)
	require.Equal(t, 50, events[4].Usage.TotalTokens)

	// The live stream showed the running announcement before the terminal
	// record, and the text deltas streamed live.
	live := collector.Wait()
	var liveTools []sessions.Event
	deltas := 0
	for _, ev := range live {
		switch ev.Kind {
		case sessions.KindToolActivity:
			liveTools = append(liveTools, ev)
		case sessions.KindTextDelta:
			deltas++
		}
	}
	require.Len(t, liveTools, 2, "a running and a terminal record ride the live stream")
	require.Equal(t, sessions.ToolStatusRunning, liveTools[0].Tool.Status)
	require.Empty(t, liveTools[0].ID, "the running form is live-only")
	require.Equal(t, sessions.ToolStatusCompleted, liveTools[1].Tool.Status)
	require.Equal(t, liveTools[0].Tool.CallID, liveTools[1].Tool.CallID)
	require.NotEmpty(t, liveTools[1].ID)
	require.Positive(t, deltas, "text deltas streamed live through the Runner")
}

// errToolResultMissing is the scripted failure when the tool result never
// reached the model.
var errToolResultMissing = errors.New("the tool result never reached the model")
