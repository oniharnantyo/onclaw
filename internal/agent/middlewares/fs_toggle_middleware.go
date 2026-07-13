package middlewares

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// fsToolNames are the seven tools injected by the Eino filesystem middleware.
var fsToolNames = map[string]bool{
	"ls":         true,
	"read_file":  true,
	"write_file": true,
	"edit_file":  true,
	"glob":       true,
	"grep":       true,
	"execute":    true,
}

// FSToggleMiddleware wraps each filesystem-middleware tool call and withholds
// any tool whose global enable flag (from tools.EnabledChecker) is false. It
// restores the tool_registry enable/disable semantics for tools that bypass
// the tool factory.
type FSToggleMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	checker tools.EnabledChecker
}

// NewFSToggleMiddleware constructs the toggle middleware.
func NewFSToggleMiddleware(checker tools.EnabledChecker) *FSToggleMiddleware {
	return &FSToggleMiddleware{checker: checker}
}

// WrapInvokableToolCall blocks disabled filesystem tools.
func (m *FSToggleMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if tc != nil && fsToolNames[tc.Name] && m.checker != nil && !m.checker.Enabled(tc.Name) {
		name := tc.Name
		return func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
			return fmt.Sprintf("tool %s is disabled", name), nil
		}, nil
	}
	return endpoint, nil
}

// WrapEnhancedInvokableToolCall blocks disabled filesystem tools invoked as
// enhanced tools (e.g. read_file under multimodal mode).
func (m *FSToggleMiddleware) WrapEnhancedInvokableToolCall(_ context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, tc *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	if tc != nil && fsToolNames[tc.Name] && m.checker != nil && !m.checker.Enabled(tc.Name) {
		name := tc.Name
		return func(ctx context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
			return &schema.ToolResult{
				Parts: []schema.ToolOutputPart{{
					Type: schema.ToolPartTypeText,
					Text: fmt.Sprintf("tool %s is disabled", name),
				}},
			}, nil
		}, nil
	}
	return endpoint, nil
}
