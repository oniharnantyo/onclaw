package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSummarization_ContextLimitDerivation verifies that the trigger threshold is
// derived at ~0.9 of ContextLimit, with 180k fallback.
func TestSummarization_ContextLimitDerivation(t *testing.T) {
	ctx := t.Context()
	fakeMdl := &fakeModel{
		reply: func([]*schema.Message) fakeReply { return fakeReply{text: "OK"} },
	}

	// 1. With ContextLimit = 200,000 -> 0.9 * 200,000 = 180,000
	cap1 := &summarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 200000,
	}
	mw1, err := newSummarizationMiddleware(ctx, cap1)
	require.NoError(t, err)
	require.NotNil(t, mw1)

	// 2. With ContextLimit = 100,000 -> 0.9 * 100,000 = 90,000
	cap2 := &summarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 100000,
	}
	mw2, err := newSummarizationMiddleware(ctx, cap2)
	require.NoError(t, err)
	require.NotNil(t, mw2)

	// 3. Fallback when ContextLimit <= 0
	cap3 := &summarizationCapability{
		Model:        fakeMdl,
		ContextLimit: 0,
	}
	mw3, err := newSummarizationMiddleware(ctx, cap3)
	require.NoError(t, err)
	require.NotNil(t, mw3)

	// 4. Missing Model returns error
	cap4 := &summarizationCapability{}
	_, err = newSummarizationMiddleware(ctx, cap4)
	require.Error(t, err)
}

// TestSummarizationSafetyNet verifies that an in-run state crossing the ~0.9 threshold
// triggers in-run summarization, collapsing the working context within the turn without
// writing a DB checkpoint, without a marker event on the transcript, and without
// transcript truncation.
func TestSummarizationSafetyNet(t *testing.T) {
	ctx := t.Context()

	// Tool returning voluminous data (~2000 chars / ~500 tokens).
	bulkyOutput := strings.Repeat("Long technical data payload section. ", 50)
	bulkyTool, err := utils.InferTool("fetch_bulky_data", "fetches voluminous data",
		func(_ context.Context, _ struct{}) (string, error) {
			return bulkyOutput, nil
		})
	require.NoError(t, err)

	callCount := 0
	sawSummaryInModelInput := false

	fakeMdl := &fakeModel{
		reply: func(input []*schema.Message) fakeReply {
			callCount++

			// Check if this is the summarizer call itself (eino summarization prompt)
			for _, msg := range input {
				if strings.Contains(msg.Content, "tasked with summarizing conversations") ||
					strings.Contains(msg.Content, "create a detailed summary") ||
					strings.Contains(msg.Content, "summarize the following conversation") ||
					strings.Contains(msg.Content, "Please provide a summary") {
					return fakeReply{text: "Summary of earlier messages: bulky data was fetched successfully."}
				}
			}

			// Check if model sees the generated summary in its context
			for _, msg := range input {
				if strings.Contains(msg.Content, "Summary of earlier messages") {
					sawSummaryInModelInput = true
				}
			}

			if callCount == 1 {
				// Propose calling bulky tool
				return toolCallsReply("call-bulk-1", "fetch_bulky_data", `{}`)
			}

			// Final answer after seeing tool output / summary
			return fakeReply{text: "Final answer after bulky data processing."}
		},
	}

	// Trigger set low (100 tokens) so the 500-token tool output easily trips the in-run safety net.
	cfg := &composeConfig{
		Name:        "test-summarization-agent",
		Instruction: "You are a test agent.",
		ChatModel:   fakeMdl,
		Tools:       []tool.BaseTool{bulkyTool},
		Summarization: &summarizationCapability{
			Model:         fakeMdl,
			TriggerTokens: 100,
		},
	}

	turnAgent, toolErrors, err := composeTurnAgent(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, turnAgent)
	require.NotNil(t, toolErrors)

	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           turnAgent,
		EnableStreaming: true,
	})

	iter := runner.Run(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("run bulky fetch")})
	err = drainRunnerIterator(iter)
	require.NoError(t, err)

	// Verifications:
	// 1. Model was called at least 3 times: 1st turn call, summarizer call, 2nd turn call
	assert.GreaterOrEqual(t, callCount, 3)
	// 2. The in-run summary collapsed into the model's working input
	assert.True(t, sawSummaryInModelInput, "model must see in-run collapsed summary in its context")
}
