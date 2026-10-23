package agents

import (
	"context"

	"github.com/cloudwego/eino/adk"
	einolens "github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/compaction"
)

const inRunSummarizationThreshold = compaction.InRunSummarizationThreshold
const defaultInRunSummarizationFallbackTokens = compaction.DefaultInRunSummarizationFallbackTokens

type summarizationCapability = compaction.SummarizationCapability

func defaultSummarizationTokenCounter(ctx context.Context, input *einolens.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
	return compaction.DefaultSummarizationTokenCounter(ctx, input)
}

func newSummarizationMiddleware(ctx context.Context, cap *summarizationCapability) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	return compaction.NewSummarizationMiddleware(ctx, cap)
}
