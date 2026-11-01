package agents

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// capturingModel records the sanitized input it receives and delegates.
type capturingModel struct {
	inputs [][]*schema.AgenticMessage
}

func (c *capturingModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	c.inputs = append(c.inputs, input)
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
}

func (c *capturingModel) Stream(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	c.inputs = append(c.inputs, input)
	return schema.StreamReaderFromArray([]*schema.AgenticMessage{{Role: schema.AgenticRoleTypeAssistant}}), nil
}

func toolCallBlock(callID, name, arguments string) *schema.ContentBlock {
	return schema.NewContentBlock(&schema.FunctionToolCall{CallID: callID, Name: name, Arguments: arguments})
}

func textBlock(text string) *schema.ContentBlock {
	return schema.NewContentBlock(&schema.AssistantGenText{Text: text})
}

func TestSanitizeModel_Generate(t *testing.T) {
	runSanitizeTest(t, func(m *sanitizeModel, input []*schema.AgenticMessage) {
		_, err := m.Generate(context.Background(), input)
		require.NoError(t, err)
	})
}

func TestSanitizeModel_Stream(t *testing.T) {
	runSanitizeTest(t, func(m *sanitizeModel, input []*schema.AgenticMessage) {
		r, err := m.Stream(context.Background(), input)
		require.NoError(t, err)
		r.Close()
	})
}

func runSanitizeTest(t *testing.T, call func(*sanitizeModel, []*schema.AgenticMessage)) {
	t.Helper()

	truncated := toolCallBlock("call-1", "create_document", `{ "file_name": "x`)
	valid := toolCallBlock("call-2", "read_file", `{"path": "a.txt"}`)
	empty := toolCallBlock("call-3", "list_dir", "")
	text := textBlock("hello")
	msg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{truncated, valid, empty, text},
	}
	cleanMsg := &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{textBlock("question")},
	}
	input := []*schema.AgenticMessage{msg, cleanMsg}

	capture := &capturingModel{}
	call(newSanitizeModel(capture), input)

	require.Len(t, capture.inputs, 1)
	got := capture.inputs[0]
	require.Len(t, got, 2)

	// Input slice not mutated in place.
	require.Same(t, msg, input[0])
	require.Len(t, input[0].ContentBlocks, 4)
	require.Equal(t, `{ "file_name": "x`, truncated.FunctionToolCall.Arguments)

	// Offending message was copied, offending block rewritten to "{}".
	sent := got[0]
	require.NotSame(t, msg, sent)
	require.Len(t, sent.ContentBlocks, 4)
	require.Equal(t, "{}", sent.ContentBlocks[0].FunctionToolCall.Arguments)
	require.NotSame(t, truncated, sent.ContentBlocks[0])
	require.NotSame(t, truncated.FunctionToolCall, sent.ContentBlocks[0].FunctionToolCall)

	// Fine blocks stay pointer-identical.
	require.Same(t, valid, sent.ContentBlocks[1])
	require.Same(t, empty, sent.ContentBlocks[2])
	require.Same(t, text, sent.ContentBlocks[3])

	// Clean message stays pointer-identical.
	require.Same(t, cleanMsg, got[1])
}

func TestSanitizeToolCallArguments_NoToolCalls(t *testing.T) {
	text := textBlock("just text")
	input := []*schema.AgenticMessage{{Role: schema.AgenticRoleTypeUser, ContentBlocks: []*schema.ContentBlock{text}}}
	got := sanitizeToolCallArguments(input)
	require.Same(t, input[0], got[0])
	require.Same(t, text, got[0].ContentBlocks[0])
}
