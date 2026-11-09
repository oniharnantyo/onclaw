package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// The unknown-tool-call tests (spec agent-tools "Tool execution loop",
// design D1): a model response calling a tool outside the turn's surface is
// answered with the same JSON error payload any tool failure produces, so
// the model reads the correction as turn data and the turn loops to a final
// answer instead of dying in the ToolsNode dispatch.

// scriptedModel replies with one scripted response per call and records every
// call's input messages.
type scriptedModel struct {
	model.BaseModel[*schema.AgenticMessage]
	replies []*schema.AgenticMessage
	calls   int
	inputs  [][]*schema.AgenticMessage
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	cp := make([]*schema.AgenticMessage, len(input))
	copy(cp, input)
	m.inputs = append(m.inputs, cp)
	if m.calls >= len(m.replies) {
		return nil, errors.New("scripted model exhausted")
	}
	r := m.replies[m.calls]
	m.calls++
	return r, nil
}

// lsProbe is an invokable "ls" tool that counts its executions.
type lsProbe struct {
	tool.BaseTool
	runs int
}

func (t *lsProbe) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "ls", Desc: "list files"}, nil
}

func (t *lsProbe) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.runs++
	return "a.txt", nil
}

// callReply returns a response carrying the given tool calls.
func callReply(calls ...*schema.FunctionToolCall) *schema.AgenticMessage {
	blocks := make([]*schema.ContentBlock, 0, len(calls))
	for _, c := range calls {
		blocks = append(blocks, schema.NewContentBlock(c))
	}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: blocks}
}

// textReply returns a plain text response.
func textReply(text string) *schema.AgenticMessage {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: text}),
		},
	}
}

// runGuardedTurn composes the real turn agent over the scripted model and the
// ls probe, runs one turn, and returns the model, the probe, the failure
// registry, and the run's final output message.
func runGuardedTurn(t *testing.T, replies ...*schema.AgenticMessage) (*scriptedModel, *lsProbe, *ToolErrorResult, *schema.AgenticMessage) {
	t.Helper()
	mdl := &scriptedModel{replies: replies}
	ls := &lsProbe{}
	agent, toolErrors, err := ComposeTurnAgent(t.Context(), &ComposeConfig{
		Name:          "guarded",
		ChatModel:     mdl,
		Tools:         []tool.BaseTool{ls},
		MaxIterations: 5,
	})
	require.NoError(t, err)

	user := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "go"}),
		},
	}
	iter := agent.Run(t.Context(), &adk.TypedAgentInput[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{user}})
	var last *schema.AgenticMessage
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		require.NoError(t, ev.Err, "an unknown tool name must not fail the run")
		if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
			last = ev.Output.MessageOutput.Message
		}
	}
	return mdl, ls, toolErrors, last
}

// toolIDCounts counts the tool call ids a response carried and the tool
// result ids answering them across an input sequence.
func toolIDCounts(msgs []*schema.AgenticMessage) (calls, results map[string]int) {
	calls, results = map[string]int{}, map[string]int{}
	for _, msg := range msgs {
		for _, b := range msg.ContentBlocks {
			if b == nil {
				continue
			}
			if b.Type == schema.ContentBlockTypeFunctionToolCall && b.FunctionToolCall != nil {
				calls[b.FunctionToolCall.CallID]++
			}
			if b.Type == schema.ContentBlockTypeFunctionToolResult && b.FunctionToolResult != nil {
				results[b.FunctionToolResult.CallID]++
			}
		}
	}
	return calls, results
}

// resultTextOf returns the text content of the tool result answering callID,
// failing the test when the sequence carries none.
func resultTextOf(t *testing.T, msgs []*schema.AgenticMessage, callID string) string {
	t.Helper()
	for _, msg := range msgs {
		for _, b := range msg.ContentBlocks {
			if b == nil || b.Type != schema.ContentBlockTypeFunctionToolResult || b.FunctionToolResult == nil {
				continue
			}
			if b.FunctionToolResult.CallID != callID {
				continue
			}
			var text string
			for _, c := range b.FunctionToolResult.Content {
				if c != nil && c.Type == schema.FunctionToolResultContentBlockTypeText && c.Text != nil {
					text += c.Text.Text
				}
			}
			return text
		}
	}
	t.Fatalf("no tool result for call id %q in the input sequence", callID)
	return ""
}

// TestUnknownToolCall_MixedResponseLoops pins the mixed response (design D1,
// probe C2): one known and one unknown call — the known tool executes, the
// unknown call is answered with the error payload naming it, every call id is
// answered exactly once in the next model call's input, and the loop reaches
// the final answer.
func TestUnknownToolCall_MixedResponseLoops(t *testing.T) {
	mdl, ls, toolErrors, last := runGuardedTurn(t,
		callReply(
			&schema.FunctionToolCall{CallID: "call-a", Name: "ls", Arguments: "{}"},
			&schema.FunctionToolCall{CallID: "call-b", Name: "list", Arguments: "{}"},
		),
		textReply("done"),
	)

	require.Equal(t, 2, mdl.calls, "the turn loops to a second model call, not more")
	require.Equal(t, 1, ls.runs, "the known tool executes exactly once")
	require.Equal(t, "done", blockText(t, last), "the run ends with the scripted answer")

	// The failure rides the shared registry under the unknown call's id; the
	// known call stays unrecorded.
	text, failed := toolErrors.FailureOf("call-b")
	require.True(t, failed, "the unknown call is recorded as failed")
	require.Contains(t, text, `unknown tool "list"`)
	_, failed = toolErrors.FailureOf("call-a")
	require.False(t, failed, "the known call is not marked failed")

	// The next model call sees the response untouched — both call blocks —
	// and both ids answered exactly once (design D3).
	require.Len(t, mdl.inputs, 2)
	calls, results := toolIDCounts(mdl.inputs[1])
	require.Equal(t, 1, calls["call-a"], "the known call block stays in the response")
	require.Equal(t, 1, calls["call-b"], "the unknown call block stays in the response")
	require.Equal(t, map[string]int{"call-a": 1, "call-b": 1}, results,
		"every call id is answered exactly once")

	// The known call's result is the real execution; the unknown call's is
	// the error payload in the shared shape, naming the tool.
	require.Equal(t, "a.txt", resultTextOf(t, mdl.inputs[1], "call-a"))
	payload := resultTextOf(t, mdl.inputs[1], "call-b")
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(payload), &parsed), "the error result is the JSON payload")
	require.Equal(t, map[string]string{"error": `unknown tool "list"; use one of the tools offered in this request`, "tool": "list"}, parsed)
}

// TestUnknownToolCall_AllUnknownResponseLoops pins the live incident shape
// (design D1, probe C3): a response whose every call is unknown still loops —
// the error result answers the call and the next model call recovers to a
// final answer.
func TestUnknownToolCall_AllUnknownResponseLoops(t *testing.T) {
	mdl, ls, toolErrors, last := runGuardedTurn(t,
		callReply(&schema.FunctionToolCall{CallID: "call-b", Name: "list", Arguments: "{}"}),
		textReply("corrected"),
	)

	require.Equal(t, 2, mdl.calls, "the turn loops to a second model call")
	require.Equal(t, 0, ls.runs, "no known tool was called")
	require.Equal(t, "corrected", blockText(t, last))

	text, failed := toolErrors.FailureOf("call-b")
	require.True(t, failed)
	require.Contains(t, text, `unknown tool "list"`)

	require.Len(t, mdl.inputs, 2)
	calls, results := toolIDCounts(mdl.inputs[1])
	require.Equal(t, 1, calls["call-b"])
	require.Equal(t, map[string]int{"call-b": 1}, results, "the unknown call is answered exactly once")
	payload := resultTextOf(t, mdl.inputs[1], "call-b")
	require.Contains(t, payload, "unknown tool")
	require.Contains(t, payload, `"tool":"list"`)
}

// TestUnknownToolCall_AllKnownResponseUntouched is the regression guard: a
// response whose calls all exist behaves exactly as before the wiring — real
// execution, real result, no failure recorded.
func TestUnknownToolCall_AllKnownResponseUntouched(t *testing.T) {
	mdl, ls, toolErrors, last := runGuardedTurn(t,
		callReply(&schema.FunctionToolCall{CallID: "call-a", Name: "ls", Arguments: "{}"}),
		textReply("listed"),
	)

	require.Equal(t, 2, mdl.calls)
	require.Equal(t, 1, ls.runs)
	require.Equal(t, "listed", blockText(t, last))
	require.Equal(t, "a.txt", resultTextOf(t, mdl.inputs[1], "call-a"), "the real execution result is fed back")
	_, failed := toolErrors.FailureOf("call-a")
	require.False(t, failed, "no failure is recorded for a known call")
}

// TestUnknownToolCall_NoToolCallPassthrough pins the plain path: a text-only
// response completes in one model call with nothing answered and nothing
// recorded.
func TestUnknownToolCall_NoToolCallPassthrough(t *testing.T) {
	mdl, ls, toolErrors, last := runGuardedTurn(t, textReply("plain answer"))

	require.Equal(t, 1, mdl.calls)
	require.Equal(t, 0, ls.runs)
	require.Equal(t, "plain answer", blockText(t, last))
	require.Len(t, mdl.inputs[0], 1, "the model received exactly the user message")
	require.Len(t, toolErrors.failures, 0, "no failure is recorded")
}

// blockText renders a message's text blocks for assertions.
func blockText(t *testing.T, msg *schema.AgenticMessage) string {
	t.Helper()
	require.NotNil(t, msg)
	var text string
	for _, b := range msg.ContentBlocks {
		if b != nil && b.AssistantGenText != nil {
			text += b.AssistantGenText.Text
		}
	}
	return text
}

// failingProbe is an invokable tool whose Fn fails with a Go error — the
// fs middleware's shape (a jail refusal): the error must convert to a
// readable result on EVERY endpoint flavor, the enhanced one included
// (the agentic ToolsNode routes through it; the live /tmp/openbot_check.png
// read_file refusal escaped there and killed the turn).
type failingProbe struct {
	tool.BaseTool
}

func (t *failingProbe) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "read_file", Desc: "fails like the jailed fs tools"}, nil
}

func (t *failingProbe) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", fmt.Errorf("absolute path is outside jail roots: %q resolves to %q", "/tmp/x.png", "/private/tmp/x.png")
}

func TestToolErrorResult_EnhancedFlavorFailureFeedsBackAndTurnContinues(t *testing.T) {
	mdl := &scriptedModel{replies: []*schema.AgenticMessage{
		callReply(&schema.FunctionToolCall{CallID: "call_jail", Name: "read_file", Arguments: `{"path":"/tmp/x.png"}`}),
		textReply("the refusal came back, carrying on"),
	}}
	probe := &failingProbe{}
	agent, toolErrors, err := ComposeTurnAgent(t.Context(), &ComposeConfig{
		Name:          "enhanced-failure",
		ChatModel:     mdl,
		Tools:         []tool.BaseTool{probe},
		MaxIterations: 5,
	})
	require.NoError(t, err)

	user := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "go"}),
		},
	}
	iter := agent.Run(t.Context(), &adk.TypedAgentInput[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{user}})
	var last *schema.AgenticMessage
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		require.NoError(t, ev.Err, "a failing tool must never fail the run")
		if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
			last = ev.Output.MessageOutput.Message
		}
	}

	// The turn survived to the final text reply, the model read the refusal
	// as its tool result, and the failure is registered for the drain.
	require.Equal(t, 2, mdl.calls)
	require.Equal(t, "the refusal came back, carrying on", blockText(t, last))
	text := resultTextOf(t, mdl.inputs[1], "call_jail")
	require.Contains(t, text, "outside jail roots")
	require.Contains(t, text, "read_file")
	_, recorded := toolErrors.FailureOf("call_jail")
	require.True(t, recorded, "the failure is recorded for the drain's terminal record")
}
