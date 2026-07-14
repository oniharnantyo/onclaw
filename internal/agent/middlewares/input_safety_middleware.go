package middlewares

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// InputSafetyMiddleware is a preflight guard that fails fast when the fixed
// input floor (system instruction + tool schemas) reaches the safety limit,
// leaving too little room for conversation history. It runs at handler index 0,
// before summarization, so the turn is rejected before any model call.
//
// The floor is re-estimated each turn from the live state.ToolInfos (the tool
// set actually handed to the model) plus the precomputed system-prompt token
// count, in case the effective tool list differs from assembly time.
type InputSafetyMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	systemPromptTokens int
	contextWindow      int
}

// NewInputSafetyMiddleware constructs the input-safety preflight middleware.
func NewInputSafetyMiddleware(systemPromptTokens, contextWindow int) *InputSafetyMiddleware {
	return &InputSafetyMiddleware{
		systemPromptTokens: systemPromptTokens,
		contextWindow:      contextWindow,
	}
}

// estimateTokens mirrors agent.estimateTokenCount (chars/4). It is duplicated
// here rather than imported to avoid an agent -> middlewares import cycle.
func estimateTokens(charLen int) int {
	return charLen / 4
}

// BeforeModelRewriteState recomputes the input floor and fails fast when it
// reaches the safety limit.
func (m *InputSafetyMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], modelCtx *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	floor := m.systemPromptTokens
	for _, tl := range state.ToolInfos {
		if tl == nil {
			continue
		}
		tl_ := *tl
		tl_.Extra = nil
		text, err := json.Marshal(tl_)
		if err != nil {
			return ctx, state, fmt.Errorf("input safety: marshal tool info: %w", err)
		}
		floor += estimateTokens(len(text))
	}

	limit := FloorSafetyLimit(m.contextWindow)
	if floor >= limit {
		return ctx, state, fmt.Errorf("input floor %d tokens reaches safety limit %d tokens (context window %d): %w",
			floor, limit, m.contextWindow, ErrInputFloorExceedsSafetyLimit)
	}
	return ctx, state, nil
}
