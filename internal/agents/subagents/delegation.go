package subagents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// DelegationArgs normalizes every call of the agent tool before eino's
// subagent middleware parses it (spec agent-subagents "Delegation runs in the
// background"): run_in_background is forced on, so the launch returns the task
// id at once and the child runs detached; a missing subagent_type falls back
// to the one agent type on offer; and a model that sends the task text under
// `task` (or omits description) still delegates instead of failing validation.
//
// Both endpoint flavors are wrapped: the ADK executes tool calls through the
// streamable chain (invokable-only tools are auto-wrapped), so an
// invokable-only wrapper would never see the call.
type DelegationArgs struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]

	toolName    string
	defaultType string
}

// NewDelegationArgs returns the middleware for the named agent tool; empty
// subagent_type arguments resolve to defaultType.
func NewDelegationArgs(toolName, defaultType string) *DelegationArgs {
	return &DelegationArgs{toolName: toolName, defaultType: defaultType}
}

func (m *DelegationArgs) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if tCtx == nil || tCtx.Name != m.toolName {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return endpoint(ctx, normalizeDelegationArgs(args, m.defaultType), opts...)
	}, nil
}

func (m *DelegationArgs) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	if tCtx == nil || tCtx.Name != m.toolName {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		return endpoint(ctx, normalizeDelegationArgs(args, m.defaultType), opts...)
	}, nil
}

// normalizeDelegationArgs rewrites one agent-tool argument payload. Arguments
// that are not a JSON object are returned untouched so the tool's own
// validation reports them.
func normalizeDelegationArgs(args, defaultType string) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(args), &fields); err != nil || fields == nil {
		return args
	}
	if stringField(fields, "prompt") == "" {
		if task := stringField(fields, "task"); task != "" {
			fields["prompt"] = task
		}
	}
	delete(fields, "task")
	if stringField(fields, "description") == "" {
		if prompt := stringField(fields, "prompt"); prompt != "" {
			fields["description"] = TaskSummary(prompt)
		}
	}
	if stringField(fields, "subagent_type") == "" && defaultType != "" {
		fields["subagent_type"] = defaultType
	}
	fields["run_in_background"] = true
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return string(out)
}

func stringField(fields map[string]any, key string) string {
	s, _ := fields[key].(string)
	return strings.TrimSpace(s)
}

// DelegationToolDescription is the agent tool's description under the
// always-background contract. It replaces eino's default, whose background
// paragraph documents a foreground default this runtime does not have. The
// available agent types are still listed by the middleware itself.
func DelegationToolDescription(context.Context, []adk.TypedAgent[*schema.AgenticMessage]) (string, error) {
	return delegationToolDescription, nil
}

const delegationToolDescription = `Launch a new agent to handle complex, multi-step tasks. Each agent type has specific capabilities and tools available to it.

When using the agent tool, specify a subagent_type parameter to select which agent type to use, a complete prompt (each agent starts fresh with no shared context), and a short description.

Every subagent runs as a background task: this call returns immediately with the task id (the run_in_background argument is ignored), the task keeps running after your turn ends, and the user watches its progress in the task panel. Use task_output with the task id to read its progress or wait for its result, and task_stop to cancel it. The agent's final message is returned through task_output; it is not shown to the user — relay what matters. Launch independent subagents in one response so they run concurrently.`
