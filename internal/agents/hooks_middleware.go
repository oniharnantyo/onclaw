package agents

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
)

// hooksMiddleware is the machine policy gate on tool calls (design.md D2/D3):
// pre_tool_use evaluates before the wrapped tool endpoint runs and a block is
// delivered as the call's result payload — the model reads it and adapts, the
// run continues, and the block NEVER surfaces as a run failure or as a human
// approval interrupt (a blocked call never reaches the tool that would
// interrupt). The decision cache lives in the per-run [agenthooks.Resolved]
// (D4): an approved call re-executing after human approval returns the cached
// outcome verbatim.
//
// BOTH endpoint flavors are wrapped — the known eino gotcha: the ADK builds
// separate invokable and streamable chains from every handler, and the
// ToolsNode executes through the STREAMABLE one, so wrapping only the
// invokable chain never sees the call.
//
// Ordering: buildMiddlewares appends this middleware BEFORE the
// tool-error-result middleware, whose wrapper therefore sits outside it. The
// block result is returned as a successful tool output, interrupt and cancel
// errors pass through untouched (the approval flow depends on them reaching
// the ADK), and every other endpoint error is forwarded for the outer
// tool-error-result wrapper to convert into an error result.
type hooksMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	resolved *agenthooks.Resolved
	base     agenthooks.Event
}

// newHooksMiddleware returns the ADK middleware for one run's resolved hook
// chain. cfg.HooksBase carries the per-run event identity (workspace, agent,
// session, user, origin) each delivery clones; it is set whenever Hooks is.
func newHooksMiddleware(resolved *agenthooks.Resolved, base agenthooks.Event) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &hooksMiddleware{resolved: resolved, base: base}
}

// WrapInvokableToolCall gates the synchronous chain: block first (the wrapped
// endpoint is never called), then execute, then the detached post_tool_use
// observer on success.
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
// never buffered or drained (the keep-up-consumer constraint), and the
// detached post_tool_use observer fires when the endpoint returned its stream
// successfully — the tool has run at that point, and the event payload
// carries the tool name, call id, and arguments, never the result text.
func (m *hooksMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if blocked, resultJSON := m.preToolUse(ctx, tCtx, argumentsInJSON); blocked {
			return errorResultStream(resultJSON), nil
		}
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			// Interrupts, cancellations, and failures pass through untouched:
			// interrupts are the approval flow's control channel and errors
			// belong to the outer tool-error-result wrapper.
			return nil, err
		}
		m.postToolUse(ctx, tCtx, argumentsInJSON)
		return sr, nil
	}, nil
}

// preToolUse runs the blocking gate on the caller's context. Args is passed
// through as-is: partial streaming JSON is what the model produced and what
// handlers receive.
func (m *hooksMiddleware) preToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) (blocked bool, resultJSON string) {
	toolEvent := agenthooks.EventTool{Name: tCtx.Name, CallID: tCtx.CallID, Args: argumentsInJSON}
	return m.resolved.PreToolUse(ctx, m.base, toolEvent)
}

// postToolUse fires the observational post_tool_use delivery detached (D5):
// handler latency, errors, and panics never delay or fail the run.
func (m *hooksMiddleware) postToolUse(ctx context.Context, tCtx *adk.ToolContext, argumentsInJSON string) {
	m.resolved.PostToolUse(ctx, m.base, agenthooks.EventTool{Name: tCtx.Name, CallID: tCtx.CallID, Args: argumentsInJSON})
}
