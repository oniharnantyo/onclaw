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
	Model         model.BaseChatModel
	ContextLimit  int64
	TriggerTokens int
	TokenCounter  summarization.TokenCounterFunc
}

// DefaultSummarizationTokenCounter estimates tokens using the shared charsPerToken ratio.
func DefaultSummarizationTokenCounter(_ context.Context, input *summarization.TypedTokenCounterInput[*schema.Message]) (int, error) {
	if input == nil {
		return 0, nil
	}
	chars := 0
	for _, m := range input.Messages {
		if m != nil {
			chars += len(m.Content)
			for _, call := range m.ToolCalls {
				chars += len(call.Function.Name) + len(call.Function.Arguments)
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
func NewSummarizationMiddleware(ctx context.Context, cap *SummarizationCapability) (adk.ChatModelAgentMiddleware, error) {
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

	cfg := &summarization.Config{
		Model: cap.Model,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: triggerTokens,
		},
		TokenCounter: tokenCounter,
		CustomFormatContextManagementInstruction: func(_ context.Context) string {
			return ""
		},
	}

	return summarization.New(ctx, cfg)
}
