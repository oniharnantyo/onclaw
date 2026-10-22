package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type sessionsStore struct{ e executor }

func (s *sessionsStore) Create(ctx context.Context, session *store.Session) error {
	if session == nil {
		return fmt.Errorf("create session: nil session")
	}

	now := nowUTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.LastActivityAt.IsZero() {
		session.LastActivityAt = session.CreatedAt
	}
	if session.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		session.ID = id
	}

	_, err := s.e.ExecContext(ctx, `
INSERT INTO sessions (id, agent_id, title, model, effort, created_at, last_activity_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.AgentID, session.Title, session.Model, session.Effort,
		formatTime(session.CreatedAt), formatTime(session.LastActivityAt),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *sessionsStore) ByID(ctx context.Context, id string) (*store.Session, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT id, agent_id, title, model, effort, created_at, last_activity_at
FROM sessions WHERE id = ?`, id)

	session, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %s: %w", id, store.ErrNotFound)
		}
		return nil, readErr("get session", err)
	}
	return session, nil
}

func (s *sessionsStore) ListByAgent(ctx context.Context, agentID string) ([]store.Session, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT id, agent_id, title, model, effort, created_at, last_activity_at
FROM sessions
WHERE agent_id = ?
ORDER BY last_activity_at DESC, created_at DESC, id ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]store.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

func (s *sessionsStore) Touch(ctx context.Context, id string, at time.Time) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE sessions SET last_activity_at = ? WHERE id = ?`, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("touch session %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("touch session %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *sessionsStore) SetTitle(ctx context.Context, id, title string) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE sessions SET title = ? WHERE id = ?`, title, id)
	if err != nil {
		return fmt.Errorf("set session title %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set session title %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *sessionsStore) SetOverrides(ctx context.Context, id, model, effort string) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE sessions SET model = ?, effort = ? WHERE id = ?`, model, effort, id)
	if err != nil {
		return fmt.Errorf("set session overrides %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set session overrides %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *sessionsStore) Delete(ctx context.Context, id string) error {
	_, err := s.e.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	return nil
}

func scanSession(sc scanner) (*store.Session, error) {
	var session store.Session
	var createdAt, lastActivityAt string
	if err := sc.Scan(&session.ID, &session.AgentID, &session.Title,
		&session.Model, &session.Effort,
		&createdAt, &lastActivityAt); err != nil {
		return nil, err
	}
	var err error
	if session.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if session.LastActivityAt, err = parseTime(lastActivityAt); err != nil {
		return nil, err
	}
	return &session, nil
}
