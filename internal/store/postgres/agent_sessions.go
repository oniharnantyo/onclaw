package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// agentSessionStore implements storeport.AgentSessionStore for PostgreSQL
// (agent-session-index D1/D2). The upsert is a single statement — the runner
// executes it on every persistent run's latency path, so no read-modify-write
// and no pre-checks: the FKs map unknown workspace/agent/user references to
// domain.ErrNotFound via convertError, and the unique
// (workspace_id, agent_id, session_id) triple drives the conflict path.
type agentSessionStore struct {
	db Executor
}

// NewAgentSessionStore creates a new AgentSessionStore with the given database executor.
func NewAgentSessionStore(db Executor) storeport.AgentSessionStore {
	return &agentSessionStore{db: db}
}

// AgentSessions returns the AgentSessionStore sub-port. The accessor rides
// alongside the sub-store it wires (the other aggregate accessors live in
// postgres.go; methods on store may be defined in any file of the package).
func (s *store) AgentSessions() storeport.AgentSessionStore {
	return NewAgentSessionStore(s.db)
}

const agentSessionColumns = `
	id, workspace_id, agent_id, user_id, session_id, title, deleted_at,
	created_at, last_active_at
`

func scanAgentSession(row pgx.Row) (*domain.AgentSession, error) {
	var s domain.AgentSession
	// Nullable columns scan through pointers: NULL cannot scan into a plain
	// string / bare time.
	err := row.Scan(
		&s.ID,
		&s.WorkspaceID,
		&s.AgentID,
		&s.UserID,
		&s.SessionID,
		&s.Title,
		&s.DeletedAt,
		&s.CreatedAt,
		&s.LastActiveAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &s, nil
}

// UpsertAgentSession executes design D2 exactly: birth inserts the row with
// the derived title; ON CONFLICT bumps last_active_at, revives soft-deleted
// rows (deleted_at = NULL), and applies the birth-only title rule —
// EXCLUDED.title wins only while the stored title is still empty. The owning
// user_id is deliberately absent from DO UPDATE: ownership is fixed at birth.
func (a *agentSessionStore) UpsertAgentSession(ctx context.Context, workspaceID, agentID, userID string, up domain.AgentSessionUpsert) error {
	if up.SessionID == "" {
		return domain.ErrInvalid
	}
	// Binding-prefix validation (integrate-telegram-gateway design D3,
	// channel-session-leak fix): only registered prefixes — including the
	// gateway's tg_dm_/tg_group_ — may index rows; unknown "<word>_"-shaped
	// ids are refused. The runner treats the failure as best-effort
	// bookkeeping, so a bad prefix never fails a run.
	if err := domain.ValidateAgentSessionID(up.SessionID); err != nil {
		return err
	}

	query := `
		INSERT INTO agent_sessions (workspace_id, agent_id, user_id, session_id, title, last_active_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (workspace_id, agent_id, session_id) DO UPDATE
		SET last_active_at = now(),
		    deleted_at = NULL,
		    title = CASE WHEN agent_sessions.title = '' THEN EXCLUDED.title
		                 ELSE agent_sessions.title END
	`
	_, err := a.db.Exec(ctx, query, workspaceID, agentID, userID, up.SessionID, up.Title)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (a *agentSessionStore) ListAgentSessions(ctx context.Context, workspaceID, agentID, userID string) ([]domain.AgentSession, error) {
	if workspaceID == "" || agentID == "" || userID == "" {
		return []domain.AgentSession{}, nil
	}

	// Non-deleted only, most recently active first (the listing index
	// ordering), id as the determinism tiebreak — and private-index only
	// (design D3): channel, scheduler, and gateway group sessions are
	// shared/automation artifacts and never surface in a per-user listing;
	// gateway DM sessions do, under the paired member. The underscores in
	// the LIKE patterns are escaped (they are wildcards), matching the
	// literal prefixes.
	query := `
		SELECT ` + agentSessionColumns + `
		FROM agent_sessions
		WHERE workspace_id = $1 AND agent_id = $2 AND user_id = $3
		  AND deleted_at IS NULL
		  AND session_id NOT LIKE 'chan\_%'
		  AND session_id NOT LIKE 'sched\_%'
		  AND session_id NOT LIKE 'tg\_group\_%'
		ORDER BY last_active_at DESC, id DESC
	`
	rows, err := a.db.Query(ctx, query, workspaceID, agentID, userID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	sessions := make([]domain.AgentSession, 0)
	for rows.Next() {
		session, err := scanAgentSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return sessions, nil
}

func (a *agentSessionStore) SoftDeleteAgentSession(ctx context.Context, workspaceID, agentID, userID, sessionID string) error {
	if workspaceID == "" || agentID == "" || userID == "" || sessionID == "" {
		return domain.ErrNotFound
	}

	// The user predicate is the privacy boundary: a foreign user's row is
	// indistinguishable from an absent one. Re-deleting an already-deleted
	// row is an accepted no-op — the listing outcome is identical.
	query := `
		UPDATE agent_sessions
		SET deleted_at = now()
		WHERE workspace_id = $1 AND agent_id = $2 AND user_id = $3 AND session_id = $4
	`
	tag, err := a.db.Exec(ctx, query, workspaceID, agentID, userID, sessionID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
