package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/webhooks"
)

// webhookRuns implements webhooks.Runs over the agent runtime and the
// sessions service — the webhook twin of scheduleRuns (add-webhook-triggers
// design D2: schedule-shaped agent-target fires). Its fields are bound after
// the runtime exists because the same construction cycle that needs this
// adapter builds the runtime; nothing calls it before Assemble returns
// (deliveries start on the gateway's first Running transition).
type webhookRuns struct {
	runtime  agents.Manager
	sessions sessions.Service
	// notifEngine, when bound in Assemble alongside the fields above, gates
	// webhook-fire completions into the notifications engine's Webhooks
	// category (add-webhook-triggers spec "Agent-target completions
	// notify"). Nil — the tests' default — means no notifications:
	// announceCompletion behaves exactly as before.
	notifEngine *notifications.Engine
}

var _ webhooks.Runs = (*webhookRuns)(nil)

// Create opens a chat session for the agent, titles it before the first
// message (the receiver passes "<endpoint name> — <delivery time>"; the
// runner only auto-titles untitled sessions), and returns the session id.
func (r *webhookRuns) Create(ctx context.Context, agentID, title string) (string, error) {
	sess, err := r.runtime.CreateSession(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("create webhook fire session for agent %s: %w", agentID, err)
	}
	if err := r.sessions.SetTitle(ctx, sess.ID, title); err != nil {
		return "", fmt.Errorf("title webhook fire session %s: %w", sess.ID, err)
	}
	return sess.ID, nil
}

// Submit sends the prompt as the session's next turn with the origin the
// receiver stamps (hooks.OriginWebhook), so hooks can tell an unattended run
// from a user turn. It returns once the turn is accepted, not when it
// finishes. When the notification engine is bound, the turn's terminal state
// is announced in the background — best-effort, never part of the delivery's
// recorded outcome.
func (r *webhookRuns) Submit(ctx context.Context, sessionID, prompt, origin string) (webhooks.TurnView, error) {
	turn, err := r.runtime.Submit(ctx, agents.SendRequest{
		SessionID: sessionID,
		Message:   prompt,
		Origin:    origin,
	})
	if err != nil {
		return nil, fmt.Errorf("submit webhook fire for session %s: %w", sessionID, err)
	}
	if r.notifEngine != nil && turn != nil {
		// The turn outlives the delivery's HTTP answer, and so does the
		// announcement: the submit context's cancellation is dropped (the
		// scheduler adapter's rule), not inherited.
		go r.announceCompletion(context.WithoutCancel(ctx), sessionID, turn)
	}
	return turn, nil
}

// announceCompletion waits for the fire's terminal state and enqueues a
// Webhooks-category candidate (add-webhook-triggers spec "Agent-target
// completions notify", mirroring the scheduled-run completion path): the
// title resolves through the engine's agent resolver — a webhook fire is an
// agent-target run, so the name key is the session's agent, not the session —
// and the excerpt is the turn's final assistant message. There is no
// home-chat announcement: the spec scopes group deliveries to the transcript
// and agent deliveries to the desktop notification. Best-effort: every
// failure is logged and dropped — the receiver already answered 202 and the
// recorded delivery outcome is never touched.
func (r *webhookRuns) announceCompletion(ctx context.Context, sessionID string, turn *agents.Turn) {
	waitErr := turn.Wait()
	outcome := outcomeCompleted
	switch {
	case errors.Is(waitErr, agents.ErrTurnCancelled):
		outcome = outcomeCancelled
	case waitErr != nil:
		outcome = outcomeFailed
	}

	excerpt := turnExcerpt(ctx, r.sessions, sessionID, turn.TurnID())
	if excerpt == "" && outcome == outcomeFailed {
		excerpt = truncateRunes(waitErr.Error(), maxExcerptRunes)
	}

	// Best-effort by the notifications contract: the Enqueue gates and
	// delivers on its own goroutine path and never returns an error; a failed
	// delivery is logged inside the engine and cannot reach this site.
	r.notifEngine.Enqueue(notifications.Candidate{
		ID:       notifications.WebhookNotificationID(sessionID, turn.TurnID()),
		Category: notifications.CategoryWebhooks,
		Origin:   notifications.OriginWebhook,
		NameKey:  sessionAgentID(r.sessions, sessionID),
		Outcome:  outcome,
		Detail:   excerpt,
		Route:    notifications.Route{Kind: notifications.RouteSession, ID: sessionID},
		ThreadID: sessionID,
	})
}

// groupSender adapts the groups service onto webhooks.GroupSender
// (add-webhook-triggers design D2): one user-authored send of the rendered
// text — mentions, lead routing, and task behavior are the send path's own.
type groupSender struct {
	groups *groups.Service
	// msgs reads the transcript after the send so the produced message id can
	// ride the delivery log (the send path answers with an error only).
	msgs store.AgentGroupStore
}

var _ webhooks.GroupSender = (*groupSender)(nil)

// webhookGroupReadWindow bounds the post-send transcript read: the row this
// send appended sits at the window's tail unless the send's own routing
// appended more, which a group's roster size bounds far below this.
const webhookGroupReadWindow = 100

// Send appends text to the group's transcript as the user through the
// service's send path and returns the transcript message id the delivery log
// records. The send answers with an error only, so the id is resolved with a
// bounded post-send transcript read: the newest user message row appended
// after the pre-send sequence snapshot. Rows append serialized under the
// group's write lock, so our row precedes any later send's; a send that
// slips in between this Send's return and the read would shadow it — the
// id then points at the sibling row (same rendered shape), which the
// delivery log tolerates.
func (g groupSender) Send(ctx context.Context, groupID, text string) (string, error) {
	before, err := g.msgs.LastSeq(ctx, groupID)
	if err != nil {
		return "", fmt.Errorf("webhook group delivery to %s: read transcript head: %w", groupID, err)
	}
	if err := g.groups.Send(ctx, groups.SendRequest{GroupID: groupID, Text: text}); err != nil {
		return "", fmt.Errorf("webhook group delivery to %s: %w", groupID, err)
	}

	rows, err := g.msgs.ListMessages(ctx, groupID, 0, webhookGroupReadWindow)
	if err != nil {
		// The send succeeded; a failed id lookup only degrades the delivery
		// log's link, never the delivery.
		slog.WarnContext(ctx, "webhook group delivery: transcript read for message id failed",
			"group_id", groupID, "error", err)
		return "", nil
	}
	var messageID string
	for _, row := range rows {
		if row.Seq <= before {
			continue
		}
		if row.Kind != store.GroupMessageKindMessage || row.AuthorKind != store.GroupAuthorKindUser {
			continue // coordinator lines and member results are not the delivery
		}
		messageID = row.MessageID
	}
	return messageID, nil
}
