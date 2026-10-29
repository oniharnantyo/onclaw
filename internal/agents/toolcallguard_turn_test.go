package agents

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The unknown-tool-name turn test (spec agent-tools "Tool execution loop",
// fix-agent-unknown-tool-call design D1): a model that hallucinates a tool
// name (`list` for `ls`) is answered with the shared error payload, corrects
// to a real tool, and the turn completes — one failed record on the
// transcript for the unknown call, no duplicates.

// TestTurn_UnknownToolNameFeedsBack drives the live incident shape through
// the Manager: the model first calls `list` (not in the surface), reads the
// synthetic error result, retries with `ls`, and answers.
func TestTurn_UnknownToolNameFeedsBack(t *testing.T) {
	calls := 0
	f := newFixture(t, func(input []*schema.Message) fakeReply {
		calls++
		switch calls {
		case 1:
			return toolCallsReply("call-1", "list", "{}")
		case 2:
			// The correction is only reachable if the unknown call's error
			// result rode the context.
			for _, msg := range input {
				if msg.Role == schema.Tool && msg.ToolCallID == "call-1" &&
					strings.Contains(msg.Content, "unknown tool") {
					return toolCallsReply("call-2", ToolLs, `{"path":"/workspace"}`)
				}
			}
			return fakeReply{err: errString("the unknown-call error result never reached the model")}
		default:
			for _, msg := range input {
				if msg.Role == schema.Tool && msg.ToolCallID == "call-2" {
					return fakeReply{text: "the workspace is empty", promptTokens: 30, completionTokens: 5}
				}
			}
			return fakeReply{err: errString("the ls result never reached the model")}
		}
	}, WithWorkspaces(workspaces.NewManager(t.TempDir())))

	// The fixture agent has no slug; give it one so `ls` is offered.
	ctx := t.Context()
	agent := f.agent
	agent.Slug = "tester"
	require.NoError(t, f.store.Agents().Update(ctx, &agent))
	f.agent = agent
	sess, err := f.mgr.CreateSession(ctx, agent.ID)
	require.NoError(t, err)

	turn, err := f.mgr.Send(ctx, sess.ID, "list the files")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "an unknown tool name must not fail the turn")
	require.Equal(t, 3, calls, "the model corrected to ls and then answered")

	// The model's second input carries the synthetic error result for the
	// unknown call, with the shared {"error","tool"} payload naming it.
	second := f.model.calls()[1]
	var sawUnknownResult bool
	for _, msg := range second {
		if msg.Role == schema.Tool && msg.ToolCallID == "call-1" &&
			strings.Contains(msg.Content, "unknown tool") &&
			strings.Contains(msg.Content, `"tool":"list"`) {
			sawUnknownResult = true
		}
	}
	require.True(t, sawUnknownResult, "the error result for the unknown call rides the second input")

	// The transcript records the unknown call exactly once as failed, and the
	// corrected ls call exactly once as completed — no duplicate records.
	events, err := f.sessions.Events(ctx, sess.ID)
	require.NoError(t, err)
	require.NotContains(t, kindsOf(events), string(sessions.KindTurnError))
	tools := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, tools, 2, "one record per tool call, nothing duplicated")
	for _, ev := range tools {
		switch ev.Tool.CallID {
		case "call-1":
			require.Equal(t, "list", ev.Tool.Name, "the record carries the hallucinated name")
			require.Equal(t, sessions.ToolStatusFailed, ev.Tool.Status)
			require.True(t, ev.Tool.IsError)
			require.Contains(t, ev.Tool.Result, "unknown tool")
			require.Equal(t, "{}", ev.Tool.Arguments)
		case "call-2":
			require.Equal(t, ToolLs, ev.Tool.Name)
			require.Equal(t, sessions.ToolStatusCompleted, ev.Tool.Status)
			require.False(t, ev.Tool.IsError)
		default:
			t.Fatalf("unexpected tool record for call id %q", ev.Tool.CallID)
		}
	}
	require.Equal(t, sessions.KindAssistantMessage, events[len(events)-2].Kind)
	require.Equal(t, "the workspace is empty", events[len(events)-2].Text)
	require.Equal(t, sessions.KindTurnCompleted, events[len(events)-1].Kind)
}
