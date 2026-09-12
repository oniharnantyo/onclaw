package channels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// RunSubmitter mints agent runs: *agents.Runner satisfies it. Channel runs
// drain their own EventStream taps; the submitter's async contract means a
// submit error is immediate (validation, 409 one-run-per-session) while the
// run's outcome arrives through the stream.
type RunSubmitter interface {
	Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
}

// ChannelSessionID is the deterministic per-(channel, agent) session id
// (design D7): globally unique, stable across summons, so an agent's channel
// tool history accumulates in one session and the RunManager's
// one-run-per-session guard keeps concurrent agents on separate sessions.
func ChannelSessionID(channelID, agentID string) string {
	return "chan_" + channelID + "_" + agentID
}

// submitRequest describes one minted run: which agent, triggered by which
// message, under whose user identity.
type submitRequest struct {
	channel      *domain.Channel
	agentID      string
	trigger      domain.ChannelMessage
	authorHandle string
	// userID is the workspace member the run executes under (the runner's
	// load path requires one): the acting human, propagated down the chain
	// for agent re-triggers.
	userID string
	// workSessionID is the work session the run was minted for
	// (channel-teams D1); "" for session-less v1 runs. The hop is billed by
	// the caller BEFORE submitting; the id rides the ExecRequest so the
	// drain's auto-post can detect a session that closed mid-run.
	workSessionID string
	// Synthetic-summon fields (the watchdog and the budget-exhaustion status
	// notify — no trigger message): when input is non-empty it replaces the
	// default attributed trigger line, and rootMessageID/chainDepth carry the
	// chain coordinates explicitly (the session's kickoff root, depth 0).
	input         string
	rootMessageID string
	chainDepth    int
}

// submitRun mints one run for a summoned agent: deterministic session id,
// attributed turn input, run_started broadcast, async submit, and a drain
// goroutine that auto-posts the final reply and writes the run summary.
func (c *Chokepoint) submitRun(ctx context.Context, req submitRequest) {
	sessionID := ChannelSessionID(req.channel.ID, req.agentID)
	input := req.input
	if input == "" {
		input = fmt.Sprintf("[#%s] @%s: %s", req.channel.Slug, req.authorHandle, req.trigger.Body)
	}
	rootMessageID, chainDepth := req.rootMessageID, req.chainDepth
	if req.trigger.ID != "" {
		rootMessageID = chainRootOf(req.trigger)
		chainDepth = req.trigger.ChainDepth
	}

	exec := agents.ExecRequest{
		WorkspaceID:   req.channel.WorkspaceID,
		AgentID:       req.agentID,
		SessionID:     sessionID,
		UserID:        req.userID,
		Origin:        agents.OriginChannel,
		ChannelID:     req.channel.ID,
		RootMessageID: rootMessageID,
		ChainDepth:    chainDepth,
		WorkSessionID: req.workSessionID,
		Input:         input,
	}

	c.hub.Broadcast(req.channel.ID, newRunStatusEvent(EventRunStarted, req.channel.ID, req.agentID, sessionID, RunStatusRunning))

	// Register the link before submitting: a channel.post firing the instant
	// the run starts must find it (unregistered again on submit failure).
	link := &runLink{
		sessionID:     sessionID,
		userID:        req.userID,
		rootMessageID: exec.RootMessageID,
		chainDepth:    exec.ChainDepth,
		workSessionID: exec.WorkSessionID,
	}
	c.registerLink(req.channel.ID, req.agentID, link)

	stream, err := c.runner.Run(ctx, exec)
	if err != nil {
		c.unregisterLink(req.channel.ID, req.agentID, link)
		slog.WarnContext(ctx, "channels: run submit failed",
			"channel_id", req.channel.ID, "agent_id", req.agentID,
			"session_id", sessionID, "error", err)
		c.hub.Broadcast(req.channel.ID, newRunStatusEvent(EventRunFinished, req.channel.ID, req.agentID, sessionID, RunStatusFailed))
		return
	}

	// The drain outlives the caller: an HTTP post's request context must not
	// cancel a run's tail work (auto-post, summary writeback). The runner
	// itself binds runs to its base context; only the tap drain lives here.
	drainCtx := context.WithoutCancel(ctx)
	go c.drainRun(drainCtx, req, stream, link)
}

// drainRun consumes one run's event tap to EOF: it counts tool calls,
// discovers the turn id, captures the final assistant text, auto-posts the
// reply on success (D9.1 — failed/cancelled runs post nothing), writes the
// run summary onto the messages the run posted (D9.3), and broadcasts
// run_finished.
func (c *Chokepoint) drainRun(ctx context.Context, req submitRequest, stream *agents.EventStream, link *runLink) {
	start := time.Now()

	toolCounts := make(map[string]int)
	turnID := ""
	finalText := ""
	status := RunStatusCompleted

	for {
		ev, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.WarnContext(ctx, "channels: run tap receive failed",
					"channel_id", req.channel.ID, "agent_id", req.agentID, "error", err)
			}
			break
		}
		if ev == nil {
			continue
		}
		if ev.TurnID != "" && turnID == "" {
			turnID = ev.TurnID
			c.setLinkTurn(link, turnID)
		}
		switch ev.Kind {
		case agents.TranscriptEventToolCallFinished:
			if ev.ToolResult != nil && ev.ToolResult.Name != "" {
				toolCounts[ev.ToolResult.Name]++
			}
		case agents.TranscriptEventMessageCompleted:
			if ev.Message != nil && ev.Message.Role == "assistant" &&
				len(ev.Message.ToolCalls) == 0 && strings.TrimSpace(ev.Message.Content) != "" {
				finalText = ev.Message.Content
			}
		case agents.TranscriptEventError:
			status = RunStatusFailed
		case agents.TranscriptEventCancelled:
			status = RunStatusCancelled
		}
	}

	// Auto-post (D9.1): the final assistant text re-enters the pipeline as
	// the agent — mention parsing, caps, possible follow-on summons. The
	// active link is still registered, so the post propagates the chain
	// (root, depth+1) and stamps session/turn for the summary writeback.
	if status == RunStatusCompleted && strings.TrimSpace(finalText) != "" {
		if _, err := c.Post(ctx, req.channel.WorkspaceID, req.channel.ID, Author{
			Type:    string(domain.ChannelMemberTypeAgent),
			AgentID: req.agentID,
		}, finalText); err != nil {
			slog.WarnContext(ctx, "channels: auto-post failed",
				"channel_id", req.channel.ID, "agent_id", req.agentID, "error", err)
		}
	}

	// Run summary (D9.3): written once at finish, onto the messages this run
	// posted — the auto-posted final and any channel.post messages, found by
	// their (session_id, turn_id) stamp. A run that posted nothing matches
	// no rows.
	summary := domain.RunSummary{
		Tools:      toolCounts,
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err := c.channels.UpdateChannelMessageRunSummary(ctx, req.channel.WorkspaceID, req.channel.ID, link.sessionID, turnID, summary); err != nil {
		slog.WarnContext(ctx, "channels: run summary writeback failed",
			"channel_id", req.channel.ID, "agent_id", req.agentID,
			"session_id", link.sessionID, "error", err)
	}

	c.unregisterLink(req.channel.ID, req.agentID, link)
	c.hub.Broadcast(req.channel.ID, newRunStatusEvent(EventRunFinished, req.channel.ID, req.agentID, link.sessionID, status))
}
