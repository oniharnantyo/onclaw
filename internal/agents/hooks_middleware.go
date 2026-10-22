package agents

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
)

// hooksMiddleware gates tool calls through the turn's resolved hook chain
// (spec agent-hooks "Decision contract"; the ported onclaw hooks_middleware
// shape): pre_tool_use evaluates BEFORE the wrapped tool endpoint runs and a
// block is delivered as the call's result payload — the model reads the
// canonical block result and adapts, the run continues, and a blocked call
// NEVER reaches the tool. The per-call decision cache lives in the per-turn
// hooks.Resolved (design D4): hooks evaluate a call exactly once per turn.
// post_tool_use fires detached after a successful execution — handler
// latency, errors, and panics never delay or fail the turn.
//
// BOTH endpoint flavors are wrapped — the known eino gotcha: the ADK builds
// separate invokable and streamable chains from every handler, and the
// ToolsNode executes through the STREAMABLE one, so wrapping only the
// invokable chain never sees the call.
//
// Ordering: buildHandlers appends this middleware after filesystem and
// BEFORE tool-error-result, whose wrapper therefore sits outside it. The
// block result is returned as a successful tool output and passes through
// the outer wrapper untouched; endpoint errors are forwarded for it to
// convert into error results.
type hooksMiddleware struct {
	adk.BaseChatModelAgentMiddleware
	resolved *hooks.Resolved
	base     hooks.Event
}

// newHooksMiddleware returns the middleware for one turn's resolved hook
// chain. base carries the per-run event identity each delivery clones; it
// rides whenever the resolved chain does.
func newHooksMiddleware(resolved *hooks.Resolved, base hooks.Event) *hooksMiddleware {
	return &hooksMiddleware{resolved: resolved, base: base}
}

// WrapInvokableToolCall gates the synchronous chain: block first (the
// wrapped endpoint is never called), then execute, then the detached
// post_tool_use observer on success.
func (m *hooksMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
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

// WrapStreamableToolCall gates the streamable chain — the one the ToolsNode
// executes through. The result stream is forwarded untouched: frames are
// never buffered or drained, and the detached post_tool_use observer fires
// when the endpoint returned its stream successfully — the tool has run at
// that point, and the event payload carries the tool name, call id, and
// arguments, never the result text.
func (m *hooksMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if blocked, resultJSON := m.preToolUse(ctx, tCtx, argumentsInJSON); blocked {
			return errorResultStream(resultJSON), nil
		}
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			// Interrupts, cancellations, and failures pass through untouched:
			// interrupts are the runtime's control channel and errors belong
			// to the outer tool-error-result wrapper.
			return nil, err
		}
		m.postToolUse(ctx, tCtx, argumentsInJSON)
		return sr, nil
	}, nil
}

// preToolUse runs the blocking gate on the caller's context — run
// cancellation kills in-flight hooks. Args pass through as-is: what the
// model produced is what handlers receive.
func (m *hooksMiddleware) preToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) (blocked bool, resultJSON string) {
	toolEvent := hooks.EventTool{Name: tCtx.Name, CallID: tCtx.CallID, Args: argumentsInJSON}
	return m.resolved.PreToolUse(ctx, m.base, toolEvent)
}

// postToolUse fires the observational post_tool_use delivery detached
// (design D10): the run never waits on it.
func (m *hooksMiddleware) postToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) {
	m.resolved.PostToolUse(ctx, m.base, hooks.EventTool{Name: tCtx.Name, CallID: tCtx.CallID, Args: argumentsInJSON})
}
