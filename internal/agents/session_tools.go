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
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// SessionToolClose is the facilitator's termination power (channel-teams D2):
// a registry tool registered under its dotted name, so pre_tool_use hooks
// target it by exact name in matchers. Exposure is decided at resolution —
// un-scoped out of the agent's denylist (or appended to a per-turn override)
// only for a work-session channel run whose running agent holds the
// facilitator role, stripped otherwise — and the constructor re-checks the
// same conditions as a backstop.
const SessionToolClose = "session.close"

// SessionToolNames is the work-session toolset, in catalog order. Empty for
// now beyond session.close; kept parallel to ChannelToolNames.
var SessionToolNames = []string{SessionToolClose}

// WorkSessions is the session-lifecycle write port the session.close tool
// calls through (channel-teams D2). The channel chokepoint satisfies it —
// CloseWorkSession closes the session, stores the summary, and posts the
// closing message through the same pipeline as every other utterance; tests
// use fakes.
type WorkSessions interface {
	CloseWorkSession(ctx context.Context, workspaceID, channelID, agentID, summary string) (*domain.WorkSession, error)
}

// ProjectSpace materializes the shared per-channel project directories the
// jail mounts read-write at /project (channel-teams D5). The local driver
// lives in internal/channels; tests use fakes.
type ProjectSpace interface {
	// Ensure creates (idempotently) the channel's project directory and
	// returns its absolute host path.
	Ensure(workspaceID, channelSlug string) (string, error)
}

// scopeSessionToolsIn returns the allowlist with session.close appended for
// runs that may close a session. This is the per-turn override path —
// denylist resolution un-scopes it instead (effectiveToolsFromDenylist) —
// and the append lands before the workspace gate, so the settings toggle
// still governs it (the channel toolset precedent). The input slice is not
// mutated.
func scopeSessionToolsIn(allowlist []string, expose bool) []string {
	if !expose {
		return allowlist
	}
	out := make([]string, 0, len(allowlist)+len(SessionToolNames))
	out = append(out, allowlist...)
	out = append(out, SessionToolNames...)
	return out
}

// withoutSessionTools strips session.close — runs outside its exposure
// conditions never see it (exposure is facilitator-session-run-only; the
// tool binds to per-run session state a direct chat does not have). Denylist
// resolution carries the name in the catalog, so this strip is what removes
// it.
func withoutSessionTools(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == SessionToolClose {
			continue
		}
		out = append(out, name)
	}
	return out
}

// sessionCloseTool closes the running work session with a stored summary
// (channel-teams D2). It is constructed per session run from the ToolContext
// bindings.
type sessionCloseTool struct {
	sessions    WorkSessions
	workspaceID string
	channelID   string
	agentID     string
	sessionID   string
}

// newSessionCloseTool builds the tool for one execution context. It fails
// explicitly outside the exposure conditions — a non-channel run, no active
// session, a non-facilitator agent, or an unwired WorkSessions port — rather
// than wiring a tool that must refuse at call time.
func newSessionCloseTool(tctx ToolContext) (tool.BaseTool, error) {
	if tctx.ChannelID == "" {
		return nil, errors.New("session.close is only available in channel runs")
	}
	if tctx.WorkSessionID == "" {
		return nil, errors.New("session.close is only available during an active work session")
	}
	if tctx.WorkSessions == nil {
		return nil, errors.New("session.close requires the work sessions port")
	}
	if tctx.ChannelRole != domain.ChannelMemberRoleFacilitator {
		return nil, errors.New("session.close is only available to the channel's facilitator")
	}
	return &sessionCloseTool{
		sessions:    tctx.WorkSessions,
		workspaceID: tctx.WorkspaceID,
		channelID:   tctx.ChannelID,
		agentID:     tctx.AgentID,
		sessionID:   tctx.WorkSessionID,
	}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *sessionCloseTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: SessionToolClose,
		Desc: "Close the channel's current work session. Use it ONLY once the session's goal is met — " +
			"the summary you provide is stored as the session's final record and posted to the channel feed. " +
			"After closing, no further work happens under the session. Only the facilitator can close.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"summary": {
				Type:     schema.String,
				Desc:     "The closing summary: what was accomplished, what remains, and where the artifacts live (e.g. under /project).",
				Required: true,
			},
		}),
	}, nil
}

// sessionCloseArgs is the deserialized tool-call argument shape.
type sessionCloseArgs struct {
	Summary string `json:"summary"`
}

// sessionCloseResult is the compact closed-session confirmation.
type sessionCloseResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	ClosedAt  string `json:"closed_at"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *sessionCloseTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args sessionCloseArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("session.close: %w", err)
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		return "", errors.New("session.close: summary is required")
	}
	session, err := t.sessions.CloseWorkSession(ctx, t.workspaceID, t.channelID, t.agentID, summary)
	if err != nil {
		return "", fmt.Errorf("session.close: %w", err)
	}
	out, err := json.Marshal(sessionCloseResult{
		SessionID: session.ID,
		Status:    string(session.Status),
		ClosedAt:  formatSessionTimestamp(session.ClosedAt),
	})
	if err != nil {
		return "", fmt.Errorf("session.close: encode result: %w", err)
	}
	return string(out), nil
}

// formatSessionTimestamp renders the closed_at instant in RFC 3339 UTC, or
// the empty string when the session closed without a recorded instant.
func formatSessionTimestamp(closedAt *time.Time) string {
	if closedAt == nil {
		return ""
	}
	return closedAt.UTC().Format(time.RFC3339)
}
