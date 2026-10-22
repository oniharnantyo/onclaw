package store

import "context"

// SessionEventStore manages the append-only session event log. Replaying a
// session's transcript means appending its events in Seq order.
type SessionEventStore interface {
	// AppendEvents appends events to the session's log, assigning Seq
	// monotonically per session (previously assigned Seq values on the
	// events are ignored). Re-appending an EventID already stored for the
	// session is a no-op for that event — retries never duplicate. An empty
	// batch is a no-op.
	AppendEvents(ctx context.Context, sessionID string, events []SessionEvent) error
	// LoadEvents returns the session's events ordered by Seq ascending —
	// the transcript replay. The result is empty (not nil) for an unknown
	// session.
	LoadEvents(ctx context.Context, sessionID string) ([]SessionEvent, error)
	// LoadEventsPage returns one window of the session's log (design D6):
	// the newest limit events with Seq strictly below beforeSeq — or the
	// newest limit events overall when beforeSeq is 0 — ordered by Seq
	// ascending, so the window replays in transcript order. The result is
	// empty (not nil) when the window is empty.
	LoadEventsPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]SessionEvent, error)
	// DeleteAll removes every event of the session. An empty log (unknown
	// session) is a no-op.
	DeleteAll(ctx context.Context, sessionID string) error
}
