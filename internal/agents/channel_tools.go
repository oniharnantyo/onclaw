package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Channel tool names (integrate-agent-channels task 5). Both are registry
// tools registered under dotted names, so pre_tool_use hooks target them by
// exact name in matchers. They are exposed ONLY in channel runs: resolve
// un-scopes them out of the agent's denylist (and appends them to a per-turn
// override) for channel runs and strips them from non-channel runs.
const (
	ChannelToolPost    = "channel.post"
	ChannelToolHistory = "channel.history"
)

// ChannelToolNames is the channel-run toolset, in catalog order.
var ChannelToolNames = []string{ChannelToolPost, ChannelToolHistory}

// channelHistoryLimits bound one channel.history page (design D16): the page
// defaults to 50 and never exceeds it.
const (
	channelHistoryDefaultLimit = 50
	channelHistoryMaxLimit     = 50
)

// channelHistoryEmptyMarker is returned when the cursor page is empty — an
// explicit state, not an error (the memory tool's empty-marker precedent).
const channelHistoryEmptyMarker = "(no earlier messages)"

// scopeChannelToolsIn returns the allowlist with the channel toolset appended
// for channel runs. This is the per-turn override path — denylist resolution
// un-scopes them instead (effectiveToolsFromDenylist) — and the append lands
// before the workspace gate, so the settings toggle still governs them. The
// input slice is not mutated.
func scopeChannelToolsIn(allowlist []string, channelRun bool) []string {
	if !channelRun {
		return allowlist
	}
	out := make([]string, 0, len(allowlist)+len(ChannelToolNames))
	out = append(out, allowlist...)
	out = append(out, ChannelToolNames...)
	return out
}

// withoutChannelTools strips the channel toolset — non-channel runs never
// expose it (exposure is channel-run-only; the tools bind to per-run channel
// state a direct chat does not have). Denylist resolution carries the names
// in the catalog, so this strip is what removes them.
func withoutChannelTools(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == ChannelToolPost || name == ChannelToolHistory {
			continue
		}
		out = append(out, name)
	}
	return out
}

// channelPostTool posts into the running agent's channel through the
// ChannelFeed chokepoint (integrate-agent-channels D9.2). It is constructed
// per channel run from the ToolContext bindings.
type channelPostTool struct {
	feed        ChannelFeed
	workspaceID string
	channelID   string
	agentID     string
}

// newChannelPostTool builds the tool for one execution context. A nil feed
// means the context is not a channel run — construction fails explicitly
// rather than wiring a tool that cannot post.
func newChannelPostTool(tctx ToolContext) (tool.BaseTool, error) {
	if tctx.ChannelFeed == nil {
		return nil, errors.New("channel.post is only available in channel runs")
	}
	return &channelPostTool{
		feed:        tctx.ChannelFeed,
		workspaceID: tctx.WorkspaceID,
		channelID:   tctx.ChannelID,
		agentID:     tctx.AgentID,
	}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *channelPostTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ChannelToolPost,
		Desc: "Post a message into the channel you are currently participating in, attributed to you. " +
			"Use it to interject mid-run — share an interim finding, answer a teammate, or hand work to another agent by @mentioning it. " +
			"The message enters the shared room feed; other members (humans and agents) see it and may respond. " +
			"Do not post your final answer here — the room receives your final reply automatically when the run completes.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"body": {
				Type:     schema.String,
				Desc:     "The message text. Use @handle to mention a member.",
				Required: true,
			},
		}),
	}, nil
}

// channelPostArgs is the deserialized tool-call argument shape.
type channelPostArgs struct {
	Body string `json:"body"`
}

// channelPostResult is the compact posted-message confirmation.
type channelPostResult struct {
	ID        string    `json:"id"`
	Seq       int64     `json:"seq"`
	CreatedAt time.Time `json:"created_at"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *channelPostTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args channelPostArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("channel.post: %w", err)
	}
	if strings.TrimSpace(args.Body) == "" {
		return "", errors.New("channel.post: body is required")
	}
	msg, err := t.feed.PostFromAgent(ctx, t.workspaceID, t.channelID, t.agentID, args.Body)
	if err != nil {
		return "", fmt.Errorf("channel.post: %w", err)
	}
	out, err := json.Marshal(channelPostResult{ID: msg.ID, Seq: msg.Seq, CreatedAt: msg.CreatedAt})
	if err != nil {
		return "", fmt.Errorf("channel.post: encode result: %w", err)
	}
	return string(out), nil
}

// channelHistoryTool pages back through the channel feed with attributed
// lines (integrate-agent-channels D8 L4).
type channelHistoryTool struct {
	context     ChannelContext
	handles     ChannelHandles
	workspaceID string
	channelID   string
}

// newChannelHistoryTool builds the tool for one execution context. A nil
// context means the execution is not a channel run — construction fails
// explicitly rather than wiring a tool that cannot read the room.
func newChannelHistoryTool(tctx ToolContext) (tool.BaseTool, error) {
	if tctx.ChannelContext == nil || tctx.ChannelHandles == nil {
		return nil, errors.New("channel.history is only available in channel runs")
	}
	return &channelHistoryTool{
		context:     tctx.ChannelContext,
		handles:     tctx.ChannelHandles,
		workspaceID: tctx.WorkspaceID,
		channelID:   tctx.ChannelID,
	}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *channelHistoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ChannelToolHistory,
		Desc: "Read earlier messages of the channel you are participating in. " +
			"Returns attributed lines `[#seq] @handle: body`, oldest→newest. " +
			"Your context already includes the most recent messages; page further back by passing before with the seq of the oldest message you already have. " +
			"Without before, the newest page is returned.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"before": {
				Type: schema.Integer,
				Desc: "Exclusive cursor: return messages strictly older than this seq (the seq is the [#N] prefix on every line). Omit or 0 for the newest page.",
			},
			"limit": {
				Type: schema.Integer,
				Desc: fmt.Sprintf("Maximum messages to return, 1–%d. Defaults to %d.", channelHistoryMaxLimit, channelHistoryDefaultLimit),
			},
		}),
	}, nil
}

// channelHistoryArgs is the deserialized tool-call argument shape. before and
// limit arrive as JSON numbers; before accepts any integer seq.
type channelHistoryArgs struct {
	Before int64 `json:"before"`
	Limit  int   `json:"limit"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *channelHistoryTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args channelHistoryArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("channel.history: %w", err)
	}
	limit := args.Limit
	if limit <= 0 {
		limit = channelHistoryDefaultLimit
	}
	if limit > channelHistoryMaxLimit {
		limit = channelHistoryMaxLimit
	}
	msgs, err := t.context.ChannelMessagesAfter(ctx, t.workspaceID, t.channelID, args.Before, limit)
	if err != nil {
		return "", fmt.Errorf("channel.history: %w", err)
	}
	if len(msgs) == 0 {
		return channelHistoryEmptyMarker, nil
	}
	lines := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		line, err := channelMessageLine(ctx, t.handles, msg)
		if err != nil {
			return "", fmt.Errorf("channel.history: %w", err)
		}
		lines = append(lines, fmt.Sprintf("[#%d] %s", msg.Seq, line))
	}
	return strings.Join(lines, "\n"), nil
}
