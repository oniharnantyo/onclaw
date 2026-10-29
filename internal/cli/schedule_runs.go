package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// RunCompletionAnnouncer is the optional seam told about finished scheduled
// runs (add-telegram-channel design D13): the channel's announcer formats
// "schedule name — outcome — excerpt" to the home chat through its normal
// outbound path.
type RunCompletionAnnouncer interface {
	AnnounceRunCompletion(ctx context.Context, scheduleName, outcome, excerpt string) error
}

// Scheduled-run terminal outcomes, as the announcer receives them (design
// D13). A failed run's terminal error text rides the excerpt when the turn
// produced no assistant message — the failure is otherwise announced bare.
const (
	outcomeCompleted = "completed"
	outcomeCancelled = "cancelled"
	outcomeFailed    = "failed"
)

// maxExcerptRunes caps the assistant-message excerpt handed to the announcer
// (design D13: ≤400 characters, never splitting a rune).
const maxExcerptRunes = 400

// scheduleRuns implements scheduler.Runs over the agent runtime and the
// sessions service (add-scheduler design D7). Its fields are bound after the
// runtime exists because the runtime's scheduler tool needs the scheduler
// service that needs this adapter; nothing calls it before Assemble returns
// (ticks start on the gateway's first Running transition).
type scheduleRuns struct {
	runtime  agents.Manager
	sessions sessions.Service
	// announcer, when bound in Assemble alongside the fields above, is told
	// about finished runs (design D13). Nil — today's default — means no
	// completion announcements: Submit behaves exactly as before.
	announcer RunCompletionAnnouncer
}

var _ scheduler.Runs = (*scheduleRuns)(nil)

// Create opens a chat session for the agent, titles it before the first
// message (the runner only auto-titles untitled sessions), and returns the
// session id.
func (r *scheduleRuns) Create(ctx context.Context, agentID, title string) (string, error) {
	sess, err := r.runtime.CreateSession(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("create scheduled session for agent %s: %w", agentID, err)
	}
	if err := r.sessions.SetTitle(ctx, sess.ID, title); err != nil {
		return "", fmt.Errorf("title scheduled session %s: %w", sess.ID, err)
	}
	return sess.ID, nil
}

// Submit sends the prompt as the session's next turn with origin scheduler
// (design D6), so hooks can tell an unattended run from a user turn. It
// returns once the turn is accepted; the transcript persists as usual. When
// an announcer is bound, the turn's terminal state is announced in the
// background (design D13) — best-effort, never part of the fire's outcome.
func (r *scheduleRuns) Submit(ctx context.Context, sessionID, prompt string) error {
	turn, err := r.runtime.Submit(ctx, agents.SendRequest{
		SessionID: sessionID,
		Message:   prompt,
		Origin:    hooks.OriginScheduler,
	})
	if err != nil {
		return fmt.Errorf("submit scheduled run for session %s: %w", sessionID, err)
	}
	if r.announcer != nil && turn != nil {
		// The turn outlives the fire tick, and so does the announcement: the
		// submit context's cancellation is dropped (the runtime's own
		// record paths do the same), not inherited.
		go r.announceCompletion(context.WithoutCancel(ctx), sessionID, turn)
	}
	return nil
}

// announceCompletion waits for the run's terminal state and reports it to
// the announcer (add-telegram-channel design D13): the schedule name is the
// session title (the fire path titles the session at Create), the excerpt is
// the turn's final assistant message. Best-effort: every failure is logged
// and dropped — RecordFire outcomes are never touched.
func (r *scheduleRuns) announceCompletion(ctx context.Context, sessionID string, turn *agents.Turn) {
	waitErr := turn.Wait()
	outcome := outcomeCompleted
	switch {
	case errors.Is(waitErr, agents.ErrTurnCancelled):
		outcome = outcomeCancelled
	case waitErr != nil:
		outcome = outcomeFailed
	}

	sess, err := r.sessions.Session(ctx, sessionID)
	if err != nil {
		slog.WarnContext(ctx, "schedule runs: completion announce skipped, session unreadable",
			"session_id", sessionID, "error", err)
		return
	}

	excerpt := r.turnExcerpt(ctx, sessionID, turn.TurnID())
	if excerpt == "" && outcome == outcomeFailed {
		excerpt = truncateRunes(waitErr.Error(), maxExcerptRunes)
	}

	if err := r.announcer.AnnounceRunCompletion(ctx, sess.Title, outcome, excerpt); err != nil {
		slog.WarnContext(ctx, "schedule runs: completion announce failed",
			"session_id", sessionID, "error", err)
	}
}

// turnExcerpt replays the session's persisted events and returns the turn's
// final assistant message, truncated to maxExcerptRunes without splitting a
// rune. Empty when the turn produced none. Events persisted before turn ids
// existed count as the session's — a scheduled fire session holds a single
// turn.
func (r *scheduleRuns) turnExcerpt(ctx context.Context, sessionID, turnID string) string {
	evs, err := r.sessions.Events(ctx, sessionID)
	if err != nil {
		slog.WarnContext(ctx, "schedule runs: completion excerpt read failed",
			"session_id", sessionID, "error", err)
		return ""
	}
	var last string
	for _, ev := range evs {
		if ev.Kind != sessions.KindAssistantMessage {
			continue
		}
		if turnID != "" && ev.TurnID != "" && ev.TurnID != turnID {
			continue // another turn's reply
		}
		last = ev.Text
	}
	return truncateRunes(last, maxExcerptRunes)
}

// truncateRunes clips s to its first max runes; a rune is never split.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
