package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type sessionEventStore struct{ e executor }

// appendEventSQL inserts one event. The sequence is allocated inside the
// statement (MAX(seq)+1 for the session): under SQLite's single-writer rule
// two concurrent appends serialize, so sequences are gapless per session and
// strictly monotonic. A retried (session_id, event_id) pair is skipped by
// the ON CONFLICT clause. ?NNN placeholders let session_id appear once in
// the column list and again in the seq subquery.
const appendEventSQL = `
INSERT INTO session_events (session_id, event_id, turn_id, seq, kind, payload, occurred_at)
VALUES (?1, ?2, ?3,
        (SELECT COALESCE(MAX(seq), 0) + 1 FROM session_events WHERE session_id = ?1),
        ?4, ?5, ?6)
ON CONFLICT (session_id, event_id) DO NOTHING`

func (s *sessionEventStore) AppendEvents(ctx context.Context, sessionID string, events []store.SessionEvent) error {
	if sessionID == "" {
		return fmt.Errorf("append session events: empty session id")
	}
	for i := range events {
		if events[i].OccurredAt.IsZero() {
			events[i].OccurredAt = nowUTC()
		}
		_, err := s.e.ExecContext(ctx, appendEventSQL,
			sessionID, events[i].EventID, events[i].TurnID,
			events[i].Kind, events[i].Payload, formatTime(events[i].OccurredAt),
		)
		if err != nil {
			return fmt.Errorf("append session event %s: %w", events[i].EventID, err)
		}
	}
	return nil
}

func (s *sessionEventStore) LoadEvents(ctx context.Context, sessionID string) ([]store.SessionEvent, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT session_id, event_id, turn_id, seq, kind, payload, occurred_at
FROM session_events
WHERE session_id = ?
ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	defer rows.Close()

	events := make([]store.SessionEvent, 0)
	for rows.Next() {
		var ev store.SessionEvent
		var occurredAt string
		if err := rows.Scan(&ev.SessionID, &ev.EventID, &ev.TurnID, &ev.Seq, &ev.Kind,
			&ev.Payload, &occurredAt); err != nil {
			return nil, fmt.Errorf("load session events: %w", err)
		}
		if ev.OccurredAt, err = parseTime(occurredAt); err != nil {
			return nil, fmt.Errorf("load session events: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	return events, nil
}

func (s *sessionEventStore) LoadEventsPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]store.SessionEvent, error) {
	query := `
SELECT session_id, event_id, turn_id, seq, kind, payload, occurred_at
FROM session_events
WHERE session_id = ?`
	args := []any{sessionID}
	if beforeSeq > 0 {
		query += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	// Newest window first, replayed in ascending order.
	query += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.e.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load session events page: %w", err)
	}
	defer rows.Close()

	events := make([]store.SessionEvent, 0)
	for rows.Next() {
		var ev store.SessionEvent
		var occurredAt string
		if err := rows.Scan(&ev.SessionID, &ev.EventID, &ev.TurnID, &ev.Seq, &ev.Kind,
			&ev.Payload, &occurredAt); err != nil {
			return nil, fmt.Errorf("load session events page: %w", err)
		}
		if ev.OccurredAt, err = parseTime(occurredAt); err != nil {
			return nil, fmt.Errorf("load session events page: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load session events page: %w", err)
	}
	// Reverse into replay (ascending) order.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func (s *sessionEventStore) DeleteAll(ctx context.Context, sessionID string) error {
	_, err := s.e.ExecContext(ctx, `DELETE FROM session_events WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session events: %w", err)
	}
	return nil
}
