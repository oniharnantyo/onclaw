package middlewares

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// FSErrorMiddleware wraps each filesystem-middleware tool call and converts
// expected, recoverable filesystem errors (classified sentinels in the tools
// package) into tool-result observations with a nil error, so the agent turn
// continues instead of terminating. Genuine infrastructure failures and
// context cancellation are left unchanged (fatal).
//
// It is a sibling of FSToggleMiddleware and sees the full (string, error)
// tool return uniformly across all six filesystem tools — necessary because
// the Backend.Write/Edit interface returns only error and cannot express a
// non-fatal decline inline.
type FSErrorMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
}

// NewFSErrorMiddleware constructs the filesystem error-conversion middleware.
func NewFSErrorMiddleware() *FSErrorMiddleware {
	return &FSErrorMiddleware{}
}

// fsObservation renders an expected filesystem error as a recoverable
// observation. Only the requested path/value and the policy reason are
// surfaced — never the absolute workspace root.
func fsObservation(err error) string {
	return fmt.Sprintf("Tool could not complete: %s", err.Error())
}

// WrapInvokableToolCall converts expected filesystem errors for standard tool calls.
func (m *FSErrorMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if tc != nil && fsToolNames[tc.Name] {
		return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
			out, err := endpoint(ctx, args, opts...)
			if err == nil {
				return out, nil
			}
			// Never convert context cancellation/deadline — the turn must stop.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return out, err
			}
			if tools.IsExpectedFSError(err) {
				return fsObservation(err), nil
			}
			return out, err
		}, nil
	}
	return endpoint, nil
}

// WrapEnhancedInvokableToolCall converts expected filesystem errors for
// enhanced (multimodal) tool calls (e.g. read_file under multimodal mode).
func (m *FSErrorMiddleware) WrapEnhancedInvokableToolCall(_ context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, tc *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	if tc != nil && fsToolNames[tc.Name] {
		return func(ctx context.Context, args *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
			out, err := endpoint(ctx, args, opts...)
			if err == nil {
				return out, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return out, err
			}
			if tools.IsExpectedFSError(err) {
				return &schema.ToolResult{
					Parts: []schema.ToolOutputPart{{
						Type: schema.ToolPartTypeText,
						Text: fsObservation(err),
					}},
				}, nil
			}
			return out, err
		}, nil
	}
	return endpoint, nil
}
