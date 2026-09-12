package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// sessionEventStore implements storeport.SessionEventStore for PostgreSQL.
type sessionEventStore struct {
	db Executor
}

// NewSessionEventStore creates a new SessionEventStore with the given database executor.
func NewSessionEventStore(db Executor) storeport.SessionEventStore {
	return &sessionEventStore{db: db}
}

// AppendEvents idempotently inserts events into the session_events table.
// On conflict (same session_id + event_id), the existing row is preserved.
func (s *sessionEventStore) AppendEvents(ctx context.Context, workspaceID string, events []domain.SessionEvent) error {
	if len(events) == 0 {
		return nil
	}

	for i := range events {
		e := &events[i]
		if e.OccurredAt.IsZero() {
			e.OccurredAt = time.Now().UTC()
		}
		if e.WorkspaceID == "" {
			e.WorkspaceID = workspaceID
		}

		query := `
			INSERT INTO session_events (session_id, event_id, turn_id, seq, kind, payload, occurred_at, workspace_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (session_id, event_id) DO NOTHING
		`
		_, err := s.db.Exec(ctx, query,
			e.SessionID,
			e.EventID,
			e.TurnID,
			e.Seq,
			e.Kind,
			string(e.Payload),
			e.OccurredAt,
			e.WorkspaceID,
		)
		if err != nil {
			return convertError(err)
		}
	}
	return nil
}

// LoadEvents retrieves events from the session_events table according to the query parameters.
func (s *sessionEventStore) LoadEvents(ctx context.Context, params storeport.LoadSessionEventsParams) ([]domain.SessionEvent, error) {
	if params.WorkspaceID == "" || params.SessionID == "" {
		return nil, fmt.Errorf("%w: workspace_id and session_id are required", domain.ErrInvalid)
	}

	args := []any{params.WorkspaceID, params.SessionID}
	conds := []string{"workspace_id = $1", "session_id = $2"}
	argIdx := 3

	if params.AfterEventID != "" {
		// Resolve the seq for the cursor event_id.
		var cursorSeq int64
		err := s.db.QueryRow(ctx,
			`SELECT seq FROM session_events WHERE session_id = $1 AND event_id = $2`,
			params.SessionID, params.AfterEventID,
		).Scan(&cursorSeq)
		if err != nil {
			return nil, convertError(err)
		}
		if params.Reverse {
			conds = append(conds, fmt.Sprintf("seq < $%d", argIdx))
		} else {
			conds = append(conds, fmt.Sprintf("seq > $%d", argIdx))
		}
		args = append(args, cursorSeq)
		argIdx++
	}

	if len(params.Kinds) > 0 {
		conds = append(conds, fmt.Sprintf("kind = ANY($%d)", argIdx))
		args = append(args, params.Kinds)
		argIdx++
	}

	// Deterministic total order (fix-session-event-ordering D2): seq is the
	// ordering authority; occurred_at and event_id only break ties so two
	// loads can never disagree even if legacy tied-seq rows remain.
	order := "ASC"
	if params.Reverse {
		order = "DESC"
	}
	orderBy := fmt.Sprintf("ORDER BY seq %s, occurred_at %s, event_id %s", order, order, order)

	// Limit <= 0 means "no limit": every matching event is returned. Only a
	// positive Limit adds a SQL LIMIT clause (matches the ADK contract where
	// opts.Limit > 0 is the bounded case).
	query := fmt.Sprintf(`
		SELECT session_id, event_id, turn_id, seq, kind, payload, occurred_at, workspace_id
		FROM session_events
		WHERE %s
		%s
	`, strings.Join(conds, " AND "), orderBy)
	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, params.Limit)
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	var events []domain.SessionEvent
	for rows.Next() {
		var e domain.SessionEvent
		if err := rows.Scan(
			&e.SessionID,
			&e.EventID,
			&e.TurnID,
			&e.Seq,
			&e.Kind,
			&e.Payload,
			&e.OccurredAt,
			&e.WorkspaceID,
		); err != nil {
			return nil, convertError(err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return events, nil
}

// NextEventSeq returns the next append position for the session's event log:
// MAX(seq)+1 over its rows, or 0 when the log is empty.
func (s *sessionEventStore) NextEventSeq(ctx context.Context, workspaceID, sessionID string) (int64, error) {
	var next int64
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq)+1, 0) FROM session_events WHERE workspace_id = $1 AND session_id = $2`,
		workspaceID, sessionID,
	).Scan(&next)
	if err != nil {
		return 0, convertError(err)
	}
	return next, nil
}

// EventExists reports whether an event with the given ID is already stored for
// the session. The (session_id, event_id) primary key makes this an indexed
// probe; absence maps to (false, nil) — pgx's no-rows error is not surfaced.
func (s *sessionEventStore) EventExists(ctx context.Context, workspaceID, sessionID, eventID string) (bool, error) {
	var one int
	err := s.db.QueryRow(ctx,
		`SELECT 1 FROM session_events WHERE workspace_id = $1 AND session_id = $2 AND event_id = $3`,
		workspaceID, sessionID, eventID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, convertError(err)
	}
	return true, nil
}
