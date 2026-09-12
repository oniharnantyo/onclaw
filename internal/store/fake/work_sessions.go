package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// WorkSessionStore implementation (channel-teams D1). Optimistic transition
// guards mirror the postgres adapter: the status predicate rides in the
// mutation itself under the store mutex, so raced transitions lose cleanly.
// -------------------------------------------------------------------------

// WorkSessions returns the WorkSessionStore sub-port.
func (s *fakeStore) WorkSessions() store.WorkSessionStore {
	return &workSessionStore{s: s}
}

type workSessionStore struct {
	s *fakeStore
}

func cloneWorkSessionPtr(s *domain.WorkSession) *domain.WorkSession {
	return cloneWorkSession(s)
}

func (ws *workSessionStore) CreateWorkSession(ctx context.Context, workspaceID string, s *domain.WorkSession) error {
	if s == nil {
		return domain.ErrInvalid
	}
	if s.Status == "" {
		s.Status = domain.WorkSessionOpen
	}
	if s.Budget == 0 {
		s.Budget = domain.DefaultWorkSessionBudget
	}
	if err := s.Validate(); err != nil {
		return err
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if _, exists := ws.s.workspaces[workspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	channel, exists := ws.s.channels.channels[s.ChannelID]
	if !exists || channel.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: channel not found in workspace", domain.ErrNotFound)
	}
	root, exists := ws.s.channels.messages[s.RootMessageID]
	if !exists || root.ChannelID != s.ChannelID || root.WorkspaceID != workspaceID {
		return fmt.Errorf("%w: root message not found in channel", domain.ErrNotFound)
	}

	// At most one non-closed session per channel (D1).
	if active := ws.activeSessionLocked(s.ChannelID); active != nil {
		return fmt.Errorf("%w: channel %q already has session %q in status %q",
			domain.ErrWorkSessionActive, s.ChannelID, active.ID, active.Status)
	}

	if s.ID != "" {
		if _, exists := ws.s.channels.workSessions[s.ID]; exists {
			return fmt.Errorf("%w: work session with id %q already exists", domain.ErrConflict, s.ID)
		}
	} else {
		s.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	ws.s.channels.workSessions[s.ID] = cloneWorkSession(s)
	return nil
}

// activeSessionLocked returns the channel's non-closed session, if any.
// Caller must hold ws.s.mu for writing.
func (ws *workSessionStore) activeSessionLocked(channelID string) *domain.WorkSession {
	for _, session := range ws.s.channels.workSessions {
		if session != nil && session.ChannelID == channelID && session.Status != domain.WorkSessionClosed {
			return session
		}
	}
	return nil
}

func (ws *workSessionStore) GetWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	session, exists := ws.s.channels.workSessions[id]
	if !exists || session.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneWorkSession(session), nil
}

func (ws *workSessionStore) ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error) {
	if workspaceID == "" || channelID == "" {
		return nil, nil
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	var active *domain.WorkSession
	for _, session := range ws.s.channels.workSessions {
		if session == nil || session.WorkspaceID != workspaceID || session.ChannelID != channelID {
			continue
		}
		if session.Status == domain.WorkSessionClosed {
			continue
		}
		if active == nil || session.CreatedAt.After(active.CreatedAt) {
			active = session
		}
	}
	return cloneWorkSessionPtr(active), nil
}

func (ws *workSessionStore) ListWorkSessions(ctx context.Context, workspaceID, channelID string) ([]domain.WorkSession, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.WorkSession{}, nil
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	sessions := make([]domain.WorkSession, 0)
	for _, session := range ws.s.channels.workSessions {
		if session != nil && session.WorkspaceID == workspaceID && session.ChannelID == channelID {
			sessions = append(sessions, *cloneWorkSession(session))
		}
	}
	// Newest first (created_at DESC, id DESC tiebreak).
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].ID > sessions[j].ID
		}
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})
	return sessions, nil
}

func (ws *workSessionStore) PauseWorkSession(ctx context.Context, workspaceID, id string, reason domain.WorkSessionPauseReason) (*domain.WorkSession, error) {
	switch reason {
	case domain.WorkSessionPauseAwaitingHuman, domain.WorkSessionPauseBudgetExhausted:
	default:
		return nil, fmt.Errorf("%w: pause reason %q must be %q or %q", domain.ErrInvalid, reason, domain.WorkSessionPauseAwaitingHuman, domain.WorkSessionPauseBudgetExhausted)
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	session, err := ws.sessionLocked(workspaceID, id)
	if err != nil {
		return nil, err
	}
	// Optimistic open→paused: the first transition wins, the loser reports
	// the conflict (postgres mirrors this with a status='open' UPDATE guard).
	if session.Status != domain.WorkSessionOpen {
		if session.Status == domain.WorkSessionClosed {
			return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, session.Status)
		}
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionNotOpen, id, session.Status)
	}

	session.Status = domain.WorkSessionPaused
	session.PauseReason = reason
	session.UpdatedAt = time.Now().UTC()
	return cloneWorkSession(session), nil
}

func (ws *workSessionStore) ResumeWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	session, err := ws.sessionLocked(workspaceID, id)
	if err != nil {
		return nil, err
	}
	// Optimistic paused→open. Already-open is an accepted no-op: the resumed
	// goal state holds, return the current row (documented port semantics).
	if session.Status == domain.WorkSessionOpen {
		return cloneWorkSession(session), nil
	}
	if session.Status != domain.WorkSessionPaused {
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, session.Status)
	}

	session.Status = domain.WorkSessionOpen
	session.PauseReason = ""
	session.UpdatedAt = time.Now().UTC()
	return cloneWorkSession(session), nil
}

func (ws *workSessionStore) ConsumeWorkSessionHop(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	session, err := ws.sessionLocked(workspaceID, id)
	if err != nil {
		return nil, err
	}
	// Atomic hop consumption: only while open AND hops_used < budget (the
	// guard postgres expresses inside the UPDATE).
	if session.Status != domain.WorkSessionOpen {
		if session.Status == domain.WorkSessionClosed {
			return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, session.Status)
		}
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionHopUnavailable, id, session.Status)
	}
	if session.HopsUsed >= session.Budget {
		return nil, fmt.Errorf("%w: session %q has used %d of %d hops", domain.ErrWorkSessionHopUnavailable, id, session.HopsUsed, session.Budget)
	}

	session.HopsUsed++
	session.UpdatedAt = time.Now().UTC()
	return cloneWorkSession(session), nil
}

func (ws *workSessionStore) CloseWorkSession(ctx context.Context, workspaceID, id, summary string) (*domain.WorkSession, error) {
	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	session, err := ws.sessionLocked(workspaceID, id)
	if err != nil {
		return nil, err
	}
	// Any non-closed → closed; closed is terminal.
	if session.Status == domain.WorkSessionClosed {
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, session.Status)
	}

	closedAt := time.Now().UTC()
	session.Status = domain.WorkSessionClosed
	session.PauseReason = ""
	session.Summary = summary
	session.ClosedAt = &closedAt
	session.UpdatedAt = closedAt
	return cloneWorkSession(session), nil
}

func (ws *workSessionStore) ListOpenWorkSessions(ctx context.Context) ([]domain.WorkSession, error) {
	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	sessions := make([]domain.WorkSession, 0)
	for _, session := range ws.s.channels.workSessions {
		if session != nil && session.Status != domain.WorkSessionClosed {
			sessions = append(sessions, *cloneWorkSession(session))
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.Before(sessions[j].CreatedAt) })
	return sessions, nil
}

// sessionLocked resolves one workspace-scoped session. Caller must hold
// ws.s.mu for writing.
func (ws *workSessionStore) sessionLocked(workspaceID, id string) (*domain.WorkSession, error) {
	session, exists := ws.s.channels.workSessions[id]
	if !exists || session.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return session, nil
}
