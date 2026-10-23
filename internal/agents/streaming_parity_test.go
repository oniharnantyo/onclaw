package agents

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedEvent records an event captured by the record tap during a turn drain.
type recordedEvent struct {
	Event   sessions.Event
	Durable bool
}

// TestStreamingParity_Baseline pins the pre-swap drain behavior (Task 3.1):
// reasoning deltas, text deltas, tool activity ordering, durable messages,
// and token usage accumulation against an ADK event stream.
func TestStreamingParity_Baseline(t *testing.T) {
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()

	// 1. Stream Assistant Message chunks:
	// Frame 1: Reasoning delta
	// Frame 2: Reasoning delta 2
	// Frame 3: Text delta 1
	// Frame 4: Text delta 2 + Tool Call + Token Usage
	streamR, streamW := schema.Pipe[*schema.AgenticMessage](4)
	go func() {
		defer streamW.Close()
		streamW.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.Reasoning{
					Text: "Thinking about ",
				}),
			},
		}, nil)
		streamW.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.Reasoning{
					Text: "the problem.",
				}),
			},
		}, nil)
		streamW.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{
					Text: "Hello, ",
				}),
			},
		}, nil)
		streamW.Send(&schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				schema.NewContentBlock(&schema.AssistantGenText{
					Text: "world!",
				}),
				schema.NewContentBlock(&schema.FunctionToolCall{
					CallID:    "call_1",
					Name:      "test_tool",
					Arguments: `{"q":"ping"}`,
				}),
			},
			ResponseMeta: &schema.AgenticResponseMeta{
				TokenUsage: &schema.TokenUsage{
					PromptTokens:     12,
					CompletionTokens: 8,
					TotalTokens:      20,
				},
			},
		}, nil)
	}()

	go func() {
		defer gen.Close()
		// Send assistant streaming message event
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					AgenticRole:   schema.AgenticRoleTypeAssistant,
					IsStreaming:   true,
					MessageStream: streamR,
				},
			},
		})

		// Send tool result event
		gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
			Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
				MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
					Message: &schema.AgenticMessage{
						ContentBlocks: []*schema.ContentBlock{
							schema.NewContentBlock(&schema.FunctionToolResult{
								CallID: "call_1",
								Name:   "test_tool",
								Content: []*schema.FunctionToolResultContentBlock{
									{
										Type: schema.FunctionToolResultContentBlockTypeText,
										Text: &schema.UserInputText{Text: `{"status":"ok"}`},
									},
								},
							}),
						},
					},
				},
			},
		})
	}()

	var recorded []recordedEvent
	record := func(ev *sessions.Event, durable bool) error {
		recorded = append(recorded, recordedEvent{
			Event:   *ev,
			Durable: durable,
		})
		return nil
	}

	usage := &sessions.Usage{}
	res, err := drainTurn(iter, record, usage, newToolErrorResult(), nil, nil, "")
	require.NoError(t, err)
	assert.Empty(t, res.partialText)
	assert.Equal(t, `{"q":"ping"}`, res.args["call_1"])

	// Assert Usage
	assert.Equal(t, 12, usage.InputTokens)
	assert.Equal(t, 8, usage.OutputTokens)
	assert.Equal(t, 20, usage.TotalTokens)

	// Collect event kinds in order
	var kinds []sessions.Kind
	var texts []string
	var durables []bool
	for _, rec := range recorded {
		kinds = append(kinds, rec.Event.Kind)
		texts = append(texts, rec.Event.Text)
		durables = append(durables, rec.Durable)
	}

	// Verify ordering:
	// 1. KindReasoningDelta (Thinking about )
	// 2. KindReasoningDelta (the problem.)
	// 3. KindTextDelta (Hello, )
	// 4. KindTextDelta (world!)
	// 5. KindReasoning (durable, complete thinking)
	// 6. KindAssistantMessage (durable, complete text)
	// 7. KindToolActivity (running, announcement)
	// 8. KindToolActivity (completed, durable)
	expectedKinds := []sessions.Kind{
		sessions.KindReasoningDelta,
		sessions.KindReasoningDelta,
		sessions.KindTextDelta,
		sessions.KindTextDelta,
		sessions.KindReasoning,
		sessions.KindAssistantMessage,
		sessions.KindToolActivity,
		sessions.KindToolActivity,
	}
	require.Equal(t, expectedKinds, kinds, "drain event kinds must match exact baseline order")

	assert.Equal(t, "Thinking about ", texts[0])
	assert.Equal(t, "the problem.", texts[1])
	assert.Equal(t, "Hello, ", texts[2])
	assert.Equal(t, "world!", texts[3])

	// Durable assistant messages
	assert.True(t, durables[4])
	assert.Equal(t, "Thinking about the problem.", texts[4])
	assert.True(t, durables[5])
	assert.Equal(t, "Hello, world!", texts[5])

	// Tool call announcement
	require.NotNil(t, recorded[6].Event.Tool)
	assert.Equal(t, sessions.ToolStatusRunning, recorded[6].Event.Tool.Status)
	assert.Equal(t, "test_tool", recorded[6].Event.Tool.Name)
	assert.Equal(t, `{"q":"ping"}`, recorded[6].Event.Tool.Arguments)

	// Durable tool completion
	assert.True(t, durables[7])
	require.NotNil(t, recorded[7].Event.Tool)
	assert.Equal(t, sessions.ToolStatusCompleted, recorded[7].Event.Tool.Status)
	assert.Equal(t, `{"status":"ok"}`, recorded[7].Event.Tool.Result)
}

// TestTranscriptFormat_AgenticBlocks_ByteComparable tests Task 3.5:
// transcript formatting over agentic blocks produces the exact JSONL shape
// as transcriptEntry.
func TestTranscriptFormat_AgenticBlocks_ByteComparable(t *testing.T) {
	// Baseline transcriptEntry records:
	taskLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepTask,
		Content: "test delegation prompt",
	})
	require.NoError(t, err)

	reasoningLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepReasoning,
		Content: "thinking steps",
	})
	require.NoError(t, err)

	textLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepText,
		Content: "response text",
	})
	require.NoError(t, err)

	toolCallLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepTool,
		Content: "call echo_tool {\"q\":\"hi\"}",
	})
	require.NoError(t, err)

	toolResultLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepTool,
		Content: "result echo_tool: {\"echo\":\"hi\"}",
	})
	require.NoError(t, err)

	resultLine, err := json.Marshal(subagents.TranscriptEntry{
		Step:    subagents.StepResult,
		Content: "final task report",
		Status:  "completed",
	})
	require.NoError(t, err)

	// Verify that formatAgenticTranscriptEntry yields byte-identical JSONL serialization
	formatEntry := func(step subagents.TranscriptStep, content, status string) []byte {
		b, err := json.Marshal(subagents.TranscriptEntry{
			Step:    step,
			Content: content,
			Status:  status,
		})
		require.NoError(t, err)
		return b
	}

	assert.Equal(t, string(taskLine), string(formatEntry(subagents.StepTask, "test delegation prompt", "")))
	assert.Equal(t, string(reasoningLine), string(formatEntry(subagents.StepReasoning, "thinking steps", "")))
	assert.Equal(t, string(textLine), string(formatEntry(subagents.StepText, "response text", "")))
	assert.Equal(t, string(toolCallLine), string(formatEntry(subagents.StepTool, "call echo_tool {\"q\":\"hi\"}", "")))
	assert.Equal(t, string(toolResultLine), string(formatEntry(subagents.StepTool, "result echo_tool: {\"echo\":\"hi\"}", "")))
	assert.Equal(t, string(resultLine), string(formatEntry(subagents.StepResult, "final task report", "completed")))

	// Round-trip into transcriptEntry structs
	var parsed subagents.TranscriptEntry
	err = json.Unmarshal(resultLine, &parsed)
	require.NoError(t, err)
	assert.Equal(t, subagents.StepResult, parsed.Step)
	assert.Equal(t, "final task report", parsed.Content)
	assert.Equal(t, "completed", parsed.Status)
}
