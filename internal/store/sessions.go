package store

import (
	"context"
	"time"
)

// SessionsStore manages chat sessions: one row per conversation, owned by an
// agent. The session registry is the queryable index behind the chat menu's
// per-agent session list; the conversation content itself lives in the
// append-only session event log.
type SessionsStore interface {
	// Create stores a new session, assigning ID and stamping CreatedAt when
	// empty. LastActivityAt defaults to CreatedAt when zero.
	Create(ctx context.Context, session *Session) error
	// ByID returns the session with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*Session, error)
	// ListByAgent returns the agent's sessions, most recent activity first.
	// The result is never nil.
	ListByAgent(ctx context.Context, agentID string) ([]Session, error)
	// Touch advances the session's LastActivityAt; ErrNotFound when absent.
	Touch(ctx context.Context, id string, at time.Time) error
	// SetTitle rewrites the session's title; ErrNotFound when absent.
	SetTitle(ctx context.Context, id, title string) error
	// SetOverrides rewrites the session's model and reasoning-effort
	// overrides in one write; an empty value clears the override back to
	// inherit. ErrNotFound when absent.
	SetOverrides(ctx context.Context, id, model, effort string) error
	// Delete removes the session row. An unknown session is a safe no-op
	// (spec chat-view: the delete removes the session and its transcript —
	// an already-absent session has nothing to remove). The event log is
	// deleted separately through SessionEventStore.DeleteAll.
	Delete(ctx context.Context, id string) error
}
