package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// WorkSessionStore manages workspace-scoped channel work sessions
// (channel-teams D1): bounded engagements with an open → paused → closed
// lifecycle, a hop budget, and a facilitator-owned termination.
//
// Tenant isolation: every method carries a workspaceID predicate — a session
// belonging to another workspace is indistinguishable from an unknown id
// (domain.ErrNotFound). At most one non-closed session exists per channel:
// CreateWorkSession enforces it (domain.ErrWorkSessionActive), and pause /
// resume / hop consumption are optimistic — the status guard rides in the
// UPDATE itself, so a raced transition loses cleanly with
// domain.ErrWorkSessionNotOpen / domain.ErrWorkSessionHopUnavailable instead
// of corrupting state.
type WorkSessionStore interface {
	// CreateWorkSession inserts a new session row, filling ID and timestamps.
	// Status defaults to open and Budget to domain.DefaultWorkSessionBudget
	// when zero. A channel that already has a non-closed session returns
	// domain.ErrWorkSessionActive; an unknown channel or root message in the
	// workspace returns domain.ErrNotFound.
	CreateWorkSession(ctx context.Context, workspaceID string, s *domain.WorkSession) error
	// GetWorkSession resolves one session by id within the workspace.
	GetWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error)
	// ActiveWorkSession returns the channel's non-closed session, or
	// (nil, nil) when the channel is session-less — absence is a normal
	// state, not an error.
	ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error)
	// ListWorkSessions returns the channel's sessions, newest first.
	ListWorkSessions(ctx context.Context, workspaceID, channelID string) ([]domain.WorkSession, error)
	// PauseWorkSession moves an open session to paused with the given reason
	// (optimistic: the UPDATE carries a status='open' guard). Unknown ids or
	// a session in another workspace return domain.ErrNotFound; a closed
	// session returns domain.ErrWorkSessionClosed; a session already paused
	// returns domain.ErrWorkSessionNotOpen — pausing is not idempotent, the
	// first transition wins and the loser re-reads ActiveWorkSession to
	// observe the stored reason.
	PauseWorkSession(ctx context.Context, workspaceID, id string, reason domain.WorkSessionPauseReason) (*domain.WorkSession, error)
	// ResumeWorkSession moves a paused session back to open (optimistic:
	// status='paused' guard), clearing the pause reason. Unknown ids return
	// domain.ErrNotFound; a closed session returns
	// domain.ErrWorkSessionClosed. A session that is already open is an
	// accepted no-op: the resumed goal state holds, so the current row is
	// returned with a nil error.
	ResumeWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error)
	// ConsumeWorkSessionHop increments hops_used atomically — the guard
	// (status='open' AND hops_used < budget) rides in the UPDATE — and
	// returns the updated row. Unknown ids return domain.ErrNotFound; a
	// closed session returns domain.ErrWorkSessionClosed; a guard failure
	// (budget exhausted, or the session raced to paused) returns
	// domain.ErrWorkSessionHopUnavailable.
	ConsumeWorkSessionHop(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error)
	// CloseWorkSession moves any non-closed session to closed (terminal),
	// storing the facilitator's summary and closed_at and clearing any pause
	// reason. Unknown ids return domain.ErrNotFound; an already-closed
	// session returns domain.ErrWorkSessionClosed.
	CloseWorkSession(ctx context.Context, workspaceID, id, summary string) (*domain.WorkSession, error)
	// ListOpenWorkSessions returns every non-closed session across all
	// workspaces. It is the port's ONE unscoped read — the system-level boot
	// scan feeding the stall watchdog's StartWatchdog (the
	// WorkspaceStore.ListAll precedent: reserved for the runtime scan, never
	// tenant request paths).
	ListOpenWorkSessions(ctx context.Context) ([]domain.WorkSession, error)
}
