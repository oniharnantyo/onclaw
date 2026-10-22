package cli

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// scheduleRuns implements scheduler.Runs over the agent runtime and the
// sessions service (add-scheduler design D7). Its fields are bound after the
// runtime exists because the runtime's scheduler tool needs the scheduler
// service that needs this adapter; nothing calls it before Assemble returns
// (ticks start on the gateway's first Running transition).
type scheduleRuns struct {
	runtime  agents.Manager
	sessions sessions.Service
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
// returns once the turn is accepted; the transcript persists as usual.
func (r *scheduleRuns) Submit(ctx context.Context, sessionID, prompt string) error {
	if _, err := r.runtime.Submit(ctx, agents.SendRequest{
		SessionID: sessionID,
		Message:   prompt,
		Origin:    hooks.OriginScheduler,
	}); err != nil {
		return fmt.Errorf("submit scheduled run for session %s: %w", sessionID, err)
	}
	return nil
}
