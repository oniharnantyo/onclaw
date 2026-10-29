package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type sessionsStore struct{ s *Store }

func (x sessionsStore) Create(_ context.Context, session *store.Session) error {
	if session == nil {
		return fmt.Errorf("create session: nil session")
	}

	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	if session.CreatedAt.IsZero() {
		session.CreatedAt = nowUTC()
	}
	if session.LastActivityAt.IsZero() {
		session.LastActivityAt = session.CreatedAt
	}
	if session.ID == "" {
		session.ID = nextID()
	}
	x.s.sessions[session.ID] = cloneSession(*session)
	return nil
}

func (x sessionsStore) ByID(_ context.Context, id string) (*store.Session, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	session, ok := x.s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s: %w", id, store.ErrNotFound)
	}
	out := cloneSession(session)
	return &out, nil
}

func (x sessionsStore) ListByAgent(_ context.Context, agentID string) ([]store.Session, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	out := make([]store.Session, 0)
	for _, session := range x.s.sessions {
		if session.AgentID == agentID {
			out = append(out, cloneSession(session))
		}
	}
	// Most recent activity first; created_at DESC then ID break ties
	// deterministically, mirroring the sqlite adapter's ORDER BY.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (x sessionsStore) List(_ context.Context) ([]store.Session, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	out := make([]store.Session, 0, len(x.s.sessions))
	for _, session := range x.s.sessions {
		out = append(out, cloneSession(session))
	}
	// Most recent activity first; created_at DESC then ID break ties
	// deterministically, mirroring the sqlite adapter's ORDER BY.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (x sessionsStore) Touch(_ context.Context, id string, at time.Time) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	session, ok := x.s.sessions[id]
	if !ok {
		return fmt.Errorf("touch session %s: %w", id, store.ErrNotFound)
	}
	session.LastActivityAt = at
	x.s.sessions[id] = session
	return nil
}

func (x sessionsStore) SetTitle(_ context.Context, id, title string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	session, ok := x.s.sessions[id]
	if !ok {
		return fmt.Errorf("set session title %s: %w", id, store.ErrNotFound)
	}
	session.Title = title
	x.s.sessions[id] = session
	return nil
}

func (x sessionsStore) SetOverrides(_ context.Context, id, model, effort string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	session, ok := x.s.sessions[id]
	if !ok {
		return fmt.Errorf("set session overrides %s: %w", id, store.ErrNotFound)
	}
	session.Model = model
	session.Effort = effort
	x.s.sessions[id] = session
	return nil
}

func (x sessionsStore) Delete(_ context.Context, id string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()

	// An unknown session is a safe no-op, mirroring the sqlite adapter.
	delete(x.s.sessions, id)
	return nil
}

func cloneSession(in store.Session) store.Session { return in }
