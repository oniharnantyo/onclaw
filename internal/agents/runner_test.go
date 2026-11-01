package agents

import (
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// Tool-call assembly on the streamed drain path (tasks.md 1.1, design D1/D3):
// the GLM/Z.AI connector streams one tool call as an identity frame (call id
// + name, no argument text yet) followed by argument frames whose CallID is
// empty. Every frame must merge into that one call — the call then reaches
// the terminal record with the full argument text — and no orphan entry may
// appear for the unidentifiable frames.

const chunkedToolCallArgs = `{"command":"echo hi","timeout_secs":10}`

// chunkedToolCallFrames streams one shell call the way the GLM connector
// chunks it: an identity frame first, then argument deltas on frames that
// identify the call incompletely (empty CallID).
func chunkedToolCallFrames(t *testing.T) *schema.StreamReader[*schema.AgenticMessage] {
	t.Helper()
	streamR, streamW := schema.Pipe[*schema.AgenticMessage](3)
	go func() {
		defer streamW.Close()
		send := func(call *schema.FunctionToolCall) {
			streamW.Send(&schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(call),
				},
			}, nil)
		}
		send(&schema.FunctionToolCall{CallID: "call_glm_1", Name: "shell"})
		send(&schema.FunctionToolCall{Arguments: `{"command":"echo hi",`})
		send(&schema.FunctionToolCall{Arguments: `"timeout_secs":10}`})
	}()
	return streamR
}

// TestDrainAssistantOutput_ChunkedToolCallKeepsOneCall drives the streamed
// drain over the chunked shape (design D3) and asserts one assembled call
// carrying the full argument string, with no orphan announcement.
func TestDrainAssistantOutput_ChunkedToolCallKeepsOneCall(t *testing.T) {
	mo := &adk.TypedMessageVariant[*schema.AgenticMessage]{
		AgenticRole:   schema.AgenticRoleTypeAssistant,
		IsStreaming:   true,
		MessageStream: chunkedToolCallFrames(t),
	}
	var announced []*sessions.ToolActivity
	record := func(ev *sessions.Event, _ bool) error {
		if ev.Kind == sessions.KindToolActivity {
			announced = append(announced, ev.Tool)
		}
		return nil
	}
	args := map[string]string{}

	require.NoError(t, drainAssistantOutput(mo, record, &sessions.Usage{}, args, map[string]time.Time{}))

	require.Equal(t, chunkedToolCallArgs, args["call_glm_1"],
		"the call's assembled arguments reach the args map the terminal record copies from")
	require.Len(t, announced, 1, "argument frames merge into the identity call; no orphan announcement")
	require.Equal(t, "call_glm_1", announced[0].CallID)
	require.Equal(t, "shell", announced[0].Name)
	require.Equal(t, chunkedToolCallArgs, announced[0].Arguments)
}

// TestToolCallAssembler_MergesUnidentifiableFramesIntoMostRecentCall pins the
// assembler semantics directly (design D2): an empty-CallID block continues
// the most recently observed in-progress call, so interleaved calls each end
// up with their own argument text.
func TestToolCallAssembler_MergesUnidentifiableFramesIntoMostRecentCall(t *testing.T) {
	a := newToolCallAssembler()
	a.observeBlock(&schema.FunctionToolCall{CallID: "call_a", Name: "shell"})
	a.observeBlock(&schema.FunctionToolCall{Arguments: `{"command":`})
	a.observeBlock(&schema.FunctionToolCall{CallID: "call_b", Name: "shell"})
	a.observeBlock(&schema.FunctionToolCall{Arguments: `{"command":"ls"}`})

	calls := a.finish()
	require.Len(t, calls, 2, "no orphan entry for the unidentifiable frames")
	require.Equal(t, "call_a", calls[0].CallID)
	require.Equal(t, "shell", calls[0].Name)
	require.Equal(t, `{"command":`, calls[0].Arguments)
	require.Equal(t, "call_b", calls[1].CallID)
	require.Equal(t, "shell", calls[1].Name)
	require.Equal(t, `{"command":"ls"}`, calls[1].Arguments)
}

// TestToolCallAssembler_FinishDropsResidualEmptyEntry pins the defensive
// drop (design D2): argument text that arrives before any identifiable call
// — an entry no call id ever claims — never reaches finish's output.
func TestToolCallAssembler_FinishDropsResidualEmptyEntry(t *testing.T) {
	a := newToolCallAssembler()
	a.observeBlock(&schema.FunctionToolCall{Arguments: `{"x":1}`})

	require.Empty(t, a.finish())
}
