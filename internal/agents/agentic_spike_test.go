package agents

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sequentialAgenticModel struct {
	responses []*schema.AgenticMessage
	index     int32
}

func (m *sequentialAgenticModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	idx := atomic.AddInt32(&m.index, 1) - 1
	if int(idx) >= len(m.responses) {
		return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
	}
	return m.responses[idx], nil
}

func (m *sequentialAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	r, w := schema.Pipe[*schema.AgenticMessage](1)
	go func() {
		defer w.Close()
		msg, err := m.Generate(ctx, input, opts...)
		if err != nil {
			return
		}
		w.Send(msg, nil)
	}()
	return r, nil
}

type agenticEchoParam struct {
	Text string `json:"text"`
}

type agenticEchoOutput struct {
	Echo string `json:"echo"`
}

func TestAgenticRuntimeSpike_EndToEnd(t *testing.T) {
	ctx := context.Background()

	// 1. Tool that takes agenticEchoParam and returns text
	echoTool, err := utils.InferTool("echo_tool", "Echoes input text", func(_ context.Context, param *agenticEchoParam) (*agenticEchoOutput, error) {
		return &agenticEchoOutput{Echo: "echo: " + param.Text}, nil
	})
	require.NoError(t, err)

	// 2. Sequential fake model responding:
	// Turn 1: Reasoning + ToolCall
	// Turn 2: AssistantGenText
	callID := "call_spike_123"
	turn1 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{
				Text: "I should call echo_tool with hello agentic",
			}),
			schema.NewContentBlock(&schema.FunctionToolCall{
				CallID:    callID,
				Name:      "echo_tool",
				Arguments: `{"text":"hello agentic"}`,
			}),
		},
	}
	turn2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "The tool returned: echo: hello agentic",
			}),
		},
	}

	fakeModel := &sequentialAgenticModel{
		responses: []*schema.AgenticMessage{turn1, turn2},
	}

	// 3. Compose TypedChatModelAgent over *schema.AgenticMessage
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "spike_agent",
		Description: "spike test agent",
		Instruction: "You are a test agent",
		Model:       fakeModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{echoTool},
			},
		},
	})
	require.NoError(t, err)

	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent: agent,
	})

	messages := []*schema.AgenticMessage{
		schema.UserAgenticMessage("Please echo hello agentic"),
	}

	// 4. Run & Drain events
	iter := runner.Run(ctx, messages)

	var (
		seenReasoning bool
		seenToolCall  bool
		seenToolRes   bool
		seenFinalText bool
	)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		require.NoError(t, event.Err)
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg := event.Output.MessageOutput.Message
		if msg == nil {
			continue
		}

		for _, block := range msg.ContentBlocks {
			switch block.Type {
			case schema.ContentBlockTypeReasoning:
				if block.Reasoning != nil && block.Reasoning.Text != "" {
					seenReasoning = true
					assert.Contains(t, block.Reasoning.Text, "echo_tool")
				}
			case schema.ContentBlockTypeFunctionToolCall:
				if block.FunctionToolCall != nil {
					seenToolCall = true
					assert.Equal(t, "echo_tool", block.FunctionToolCall.Name)
				}
			case schema.ContentBlockTypeFunctionToolResult:
				if block.FunctionToolResult != nil {
					seenToolRes = true
					for _, c := range block.FunctionToolResult.Content {
						if c.Text != nil {
							assert.Contains(t, c.Text.Text, "echo: hello agentic")
						}
					}
				}
			case schema.ContentBlockTypeAssistantGenText:
				if block.AssistantGenText != nil {
					seenFinalText = true
					assert.Contains(t, block.AssistantGenText.Text, "The tool returned")
				}
			}
		}
	}

	assert.True(t, seenReasoning, "must see reasoning content block")
	assert.True(t, seenToolCall, "must see function tool call block")
	assert.True(t, seenToolRes, "must see function tool result block")
	assert.True(t, seenFinalText, "must see final assistant gen text block")
}
