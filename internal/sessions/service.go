package sessions

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Service is the event-sourced session model: session lifecycle, the
// mapping between typed session events and the store's append-only event
// log, and compaction checkpoint persistence. It is the persistence half of
// the agent runtime and speaks no model types.
type Service interface {
	// CreateSession registers a new session for the agent. An empty title is
	// allowed; the runtime titles the session from its first message.
	CreateSession(ctx context.Context, agentID, title string) (Session, error)
	// Session returns the session with the given ID; store.ErrNotFound when
	// absent.
	Session(ctx context.Context, id string) (Session, error)
	// SessionsByAgent returns the agent's sessions, most recent activity
	// first. The result is never nil.
	SessionsByAgent(ctx context.Context, agentID string) ([]Session, error)
	// AllSessions returns every agent's sessions, most recent activity first
	// (add-grouped-session-rail design D1). The result is never nil.
	AllSessions(ctx context.Context) ([]Session, error)
	// SetTitle rewrites the session's title; store.ErrNotFound when absent.
	SetTitle(ctx context.Context, id, title string) error
	// SetOverrides rewrites the session's model and reasoning-effort
	// overrides (design D1); an empty value clears the override back to
	// inherit. store.ErrNotFound when absent.
	SetOverrides(ctx context.Context, id, model, effort string) error

	// Append persists events to the session's log in order, assigning each
	// event its store id and occurrence time in place, and advances the
	// session's last-activity. Append and the activity advance commit
	// together.
	Append(ctx context.Context, sessionID string, events []Event) error
	// Events replays the session's persisted events in log order, decoded.
	// Unknown event kinds (written by a newer version) are skipped. The
	// result is empty (not nil) for a session without events.
	Events(ctx context.Context, sessionID string) ([]Event, error)
	// EventsPage replays one window of the session's log (design D6): the
	// newest limit decoded events with Seq strictly below beforeSeq — or the
	// newest page when beforeSeq is 0 — in log order. Unknown kinds are
	// skipped like Events. A non-positive limit takes DefaultPageLimit.
	EventsPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]Event, error)

	// Delete removes the session, its event log, and its compaction
	// checkpoint (spec chat-view "Sessions organized per agent": the delete
	// removes the session and its transcript). An unknown session is a safe
	// no-op. The agent's other sessions are untouched.
	Delete(ctx context.Context, id string) error

	// SaveCheckpoint create-or-replaces the session's compaction checkpoint.
	SaveCheckpoint(ctx context.Context, sessionID string, cp Checkpoint) error
	// Checkpoint returns the session's checkpoint; (zero, false, nil) when
	// the session has none.
	Checkpoint(ctx context.Context, sessionID string) (Checkpoint, bool, error)
}

// NewService returns the store-backed session Service.
func NewService(s store.Store) Service {
	return &service{store: s}
}
