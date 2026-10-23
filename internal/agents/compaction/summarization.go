package compaction

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// InRunSummarizationThreshold is the fraction of context limit at which the in-run
// summarization safety net triggers (~0.9, strictly above the 0.8 pre-turn compaction threshold).
const InRunSummarizationThreshold = 0.9

// DefaultInRunSummarizationFallbackTokens is the fallback trigger if context limit is unknown.
const DefaultInRunSummarizationFallbackTokens = 180000

// SummarizationCapability configures the in-run summarization safety net middleware.
type SummarizationCapability struct {
	Model         model.BaseModel[*schema.AgenticMessage]
	ContextLimit  int64
	TriggerTokens int
	TokenCounter  summarization.TypedTokenCounterFunc[*schema.AgenticMessage]
}

// DefaultSummarizationTokenCounter estimates tokens using the shared charsPerToken ratio
// over agentic content blocks (design D1, Task 3.4).
func DefaultSummarizationTokenCounter(_ context.Context, input *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
	if input == nil {
		return 0, nil
	}
	chars := 0
	for _, m := range input.Messages {
		if m != nil {
			for _, b := range m.ContentBlocks {
				if b == nil {
					continue
				}
				if b.UserInputText != nil {
					chars += len(b.UserInputText.Text)
				}
				if b.AssistantGenText != nil {
					chars += len(b.AssistantGenText.Text)
				}
				if b.Reasoning != nil {
					chars += len(b.Reasoning.Text)
				}
				if b.FunctionToolCall != nil {
					chars += len(b.FunctionToolCall.Name) + len(b.FunctionToolCall.Arguments)
				}
				if b.FunctionToolResult != nil {
					for _, c := range b.FunctionToolResult.Content {
						if c != nil && c.Text != nil {
							chars += len(c.Text.Text)
						}
					}
				}
			}
		}
	}
	for _, t := range input.Tools {
		if t != nil {
			chars += len(t.Name) + len(t.Desc)
		}
	}
	tokens := chars / CharsPerToken
	if tokens < 1 && chars > 0 {
		tokens = 1
	}
	return tokens, nil
}

// NewSummarizationMiddleware constructs eino's summarization middleware for the in-run safety net.
func NewSummarizationMiddleware(ctx context.Context, cap *SummarizationCapability) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	if cap == nil || cap.Model == nil {
		return nil, fmt.Errorf("summarization capability requires a Model")
	}

	triggerTokens := cap.TriggerTokens
	if triggerTokens <= 0 && cap.ContextLimit > 0 {
		triggerTokens = int(float64(cap.ContextLimit) * InRunSummarizationThreshold)
	}
	if triggerTokens <= 0 {
		triggerTokens = DefaultInRunSummarizationFallbackTokens
	}

	tokenCounter := cap.TokenCounter
	if tokenCounter == nil {
		tokenCounter = DefaultSummarizationTokenCounter
	}

	cfg := &summarization.TypedConfig[*schema.AgenticMessage]{
		Model: cap.Model,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: triggerTokens,
		},
		TokenCounter: tokenCounter,
		CustomFormatContextManagementInstruction: func(_ context.Context) string {
			return ""
		},
	}

	return summarization.NewTyped[*schema.AgenticMessage](ctx, cfg)
}
