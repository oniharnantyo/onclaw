package hooks

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Event-aware matched-value mapping (D8, unchanged by D19): one matcher
// shape serves every event because the compared value changes with the event
// — the tool name on tool events, the origin on run-start/prompt events, the
// terminal status on run_finished.
func MatchedValue(ev Event) string {
	switch domain.HookEvent(ev.Event) {
	case domain.HookEventPreToolUse, domain.HookEventPostToolUse:
		if ev.Tool != nil {
			return ev.Tool.Name
		}
		return ""
	case domain.HookEventRunFinished:
		return ev.Status
	case domain.HookEventRunStarted, domain.HookEventUserPromptSubmit:
		return ev.Origin
	default:
		return ""
	}
}

// OriginValues is the fixed origin value set (D1). Fixed enums are also the
// candidate set for run_started / user_prompt_submit match counts. Telegram
// joined with the gateway integration (integrate-telegram-gateway task 6.3):
// origin matchers can target gateway turns like any other origin.
func OriginValues() []string {
	return []string{"user", "scheduler", "channel", "telegram"}
}

// StatusValues is the fixed run_finished status value set (D1).
func StatusValues() []string {
	return []string{"completed", "failed", "cancelled"}
}

// ToolValueSource enumerates the tool names currently visible to a workspace:
// the capability registry, browser alias expansion, and the workspace's MCP
// tools. The REST worker injects the real implementation; CountMatches only
// depends on this narrow interface.
type ToolValueSource interface {
	VisibleToolNames(ctx context.Context, workspaceID string) ([]string, error)
}

// CountMatches computes the save-time match-count report (D19: {"matched": N,
// "of": M}): how many of the event's currently available values the matcher
// selects. Tool events enumerate the workspace-visible toolset through src;
// origin and status events ignore src and use the fixed enums. The event is
// explicit because the value set — and therefore the report — is event-aware.
func CountMatches(ctx context.Context, event domain.HookEvent, src ToolValueSource, workspaceID string, matcher string) (matched, total int, err error) {
	var values []string
	switch event {
	case domain.HookEventPreToolUse, domain.HookEventPostToolUse:
		if src == nil {
			return 0, 0, fmt.Errorf("match count: tool value source is required for %s", event)
		}
		values, err = src.VisibleToolNames(ctx, workspaceID)
		if err != nil {
			return 0, 0, fmt.Errorf("match count: list visible tools: %w", err)
		}
	case domain.HookEventRunStarted, domain.HookEventUserPromptSubmit:
		values = OriginValues()
	case domain.HookEventRunFinished:
		values = StatusValues()
	default:
		return 0, 0, fmt.Errorf("%w: event: unknown event %q", domain.ErrInvalid, event)
	}

	compiled, err := CompileMatcher(matcher)
	if err != nil {
		return 0, 0, err
	}
	for _, value := range values {
		if compiled.Matches(value) {
			matched++
		}
	}
	return matched, len(values), nil
}
