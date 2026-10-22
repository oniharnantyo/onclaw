package compose

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
)

// HooksMiddleware gates tool calls through the turn's resolved hook chain.
type HooksMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	resolved *hooks.Resolved
	base     hooks.Event
}

// NewHooksMiddleware returns the middleware for one turn's resolved hook chain.
func NewHooksMiddleware(resolved *hooks.Resolved, base hooks.Event) *HooksMiddleware {
	return &HooksMiddleware{resolved: resolved, base: base}
}

// WrapInvokableToolCall gates the synchronous chain.
func (m *HooksMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if blocked, resultJSON := m.preToolUse(ctx, tCtx, argumentsInJSON); blocked {
			return resultJSON, nil
		}
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil {
			m.postToolUse(ctx, tCtx, argumentsInJSON)
		}
		return out, err
	}, nil
}

// WrapStreamableToolCall gates the streaming chain.
func (m *HooksMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if blocked, resultJSON := m.preToolUse(ctx, tCtx, argumentsInJSON); blocked {
			return errorResultStream(resultJSON), nil
		}
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return nil, err
		}
		m.postToolUse(ctx, tCtx, argumentsInJSON)
		return sr, nil
	}, nil
}

func (m *HooksMiddleware) preToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) (blocked bool, resultJSON string) {
	if m.resolved == nil {
		return false, ""
	}
	name := ""
	callID := ""
	if tCtx != nil {
		name = tCtx.Name
		callID = tCtx.CallID
	}
	toolEvent := hooks.EventTool{Name: name, CallID: callID, Args: argumentsInJSON}
	return m.resolved.PreToolUse(ctx, m.base, toolEvent)
}

func (m *HooksMiddleware) postToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) {
	if m.resolved == nil {
		return
	}
	name := ""
	callID := ""
	if tCtx != nil {
		name = tCtx.Name
		callID = tCtx.CallID
	}
	m.resolved.PostToolUse(ctx, m.base, hooks.EventTool{Name: name, CallID: callID, Args: argumentsInJSON})
}
