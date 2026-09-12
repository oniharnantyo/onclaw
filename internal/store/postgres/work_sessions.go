package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// workSessionStore implements storeport.WorkSessionStore for PostgreSQL
// (channel-teams D1). Lifecycle transitions are optimistic: the status guard
// rides inside the UPDATE, and a zero-row update re-reads the row to classify
// the conflict (ErrNotFound / ErrWorkSessionClosed / ErrWorkSessionNotOpen /
// ErrWorkSessionHopUnavailable).
type workSessionStore struct {
	db Executor
}

// NewWorkSessionStore creates a new WorkSessionStore with the given database executor.
func NewWorkSessionStore(db Executor) storeport.WorkSessionStore {
	return &workSessionStore{db: db}
}

const workSessionColumns = `
	id, workspace_id, channel_id, root_message_id, goal, status, pause_reason,
	budget, hops_used, summary, closed_at, created_at, updated_at
`

func scanWorkSession(row pgx.Row) (*domain.WorkSession, error) {
	var s domain.WorkSession
	// Nullable columns scan through pointers: NULL cannot scan into a plain
	// string / bare time.
	var pauseReason, summary *string
	err := row.Scan(
		&s.ID,
		&s.WorkspaceID,
		&s.ChannelID,
		&s.RootMessageID,
		&s.Goal,
		&s.Status,
		&pauseReason,
		&s.Budget,
		&s.HopsUsed,
		&summary,
		&s.ClosedAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if pauseReason != nil {
		s.PauseReason = domain.WorkSessionPauseReason(*pauseReason)
	}
	if summary != nil {
		s.Summary = *summary
	}
	return &s, nil
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

	// The channel and the root message must exist within the workspace (the
	// FKs alone would admit cross-workspace references).
	found, err := channelExistsInWorkspace(ctx, ws.db, workspaceID, s.ChannelID)
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	found, err = rootExistsInChannel(ctx, ws.db, workspaceID, s.ChannelID, s.RootMessageID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: root message not found in channel", domain.ErrNotFound)
	}

	if s.ID == "" {
		s.ID = uuid.NewString()
	}

	// At most one non-closed session per channel (D1). The single statement
	// locks the channel row and evaluates the existence check in the same
	// implicit transaction, so two concurrent kickoffs serialize on the
	// channel row and the loser inserts zero rows.
	query := `
		WITH locked AS (
			SELECT 1 FROM channels
			WHERE workspace_id = $2 AND id = $3
			FOR UPDATE
		)
		INSERT INTO channel_work_sessions (
			id, workspace_id, channel_id, root_message_id, goal, status, pause_reason,
			budget, hops_used, summary, closed_at, created_at, updated_at
		)
		SELECT $1, $2, $3, $4, $5, $6, NULL, $7, 0, NULL, NULL, $8, $8
		FROM locked
		WHERE NOT EXISTS (
			SELECT 1 FROM channel_work_sessions
			WHERE channel_id = $3 AND status <> 'closed'
		)
		RETURNING created_at, updated_at
	`
	now := time.Now().UTC()
	err = ws.db.QueryRow(ctx, query,
		s.ID,
		workspaceID,
		s.ChannelID,
		s.RootMessageID,
		s.Goal,
		s.Status,
		s.Budget,
		now,
	).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrWorkSessionActive
		}
		return convertError(err)
	}
	return nil
}

func (ws *workSessionStore) GetWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT ` + workSessionColumns + `
		FROM channel_work_sessions
		WHERE workspace_id = $1 AND id = $2
	`
	return scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id))
}

func (ws *workSessionStore) ActiveWorkSession(ctx context.Context, workspaceID, channelID string) (*domain.WorkSession, error) {
	if workspaceID == "" || channelID == "" {
		return nil, nil
	}

	// No non-closed session is a normal state: absence reads as (nil, nil).
	var none bool
	err := ws.db.QueryRow(ctx,
		`SELECT NOT EXISTS (SELECT 1 FROM channel_work_sessions WHERE workspace_id = $1 AND channel_id = $2 AND status <> 'closed')`,
		workspaceID, channelID,
	).Scan(&none)
	if err != nil {
		return nil, convertError(err)
	}
	if none {
		return nil, nil
	}

	query := `
		SELECT ` + workSessionColumns + `
		FROM channel_work_sessions
		WHERE workspace_id = $1 AND channel_id = $2 AND status <> 'closed'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`
	return scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, channelID))
}

func (ws *workSessionStore) ListWorkSessions(ctx context.Context, workspaceID, channelID string) ([]domain.WorkSession, error) {
	if workspaceID == "" || channelID == "" {
		return []domain.WorkSession{}, nil
	}

	// Newest first.
	query := `
		SELECT ` + workSessionColumns + `
		FROM channel_work_sessions
		WHERE workspace_id = $1 AND channel_id = $2
		ORDER BY created_at DESC, id DESC
	`
	rows, err := ws.db.Query(ctx, query, workspaceID, channelID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	sessions := make([]domain.WorkSession, 0)
	for rows.Next() {
		session, err := scanWorkSession(rows)
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

func (ws *workSessionStore) PauseWorkSession(ctx context.Context, workspaceID, id string, reason domain.WorkSessionPauseReason) (*domain.WorkSession, error) {
	switch reason {
	case domain.WorkSessionPauseAwaitingHuman, domain.WorkSessionPauseBudgetExhausted:
	default:
		return nil, fmt.Errorf("%w: pause reason %q must be %q or %q", domain.ErrInvalid, reason, domain.WorkSessionPauseAwaitingHuman, domain.WorkSessionPauseBudgetExhausted)
	}

	// Optimistic open→paused: the status guard rides in the UPDATE.
	query := `
		UPDATE channel_work_sessions
		SET status = 'paused', pause_reason = $3, updated_at = $4
		WHERE workspace_id = $1 AND id = $2 AND status = 'open'
		RETURNING ` + workSessionColumns + `
	`
	updated, err := scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id, reason, time.Now().UTC()))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	// Zero rows: classify the miss — unknown id, closed, or already paused.
	current, classErr := ws.classifyMiss(ctx, workspaceID, id)
	if classErr != nil {
		return nil, classErr
	}
	if current.Status == domain.WorkSessionClosed {
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, current.Status)
	}
	return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionNotOpen, id, current.Status)
}

func (ws *workSessionStore) ResumeWorkSession(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	// Optimistic paused→open, clearing the pause reason.
	query := `
		UPDATE channel_work_sessions
		SET status = 'open', pause_reason = NULL, updated_at = $3
		WHERE workspace_id = $1 AND id = $2 AND status = 'paused'
		RETURNING ` + workSessionColumns + `
	`
	updated, err := scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id, time.Now().UTC()))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	// Already-open is an accepted no-op: the resumed goal state holds.
	current, classErr := ws.classifyMiss(ctx, workspaceID, id)
	if classErr != nil {
		return nil, classErr
	}
	if current.Status == domain.WorkSessionOpen {
		return current, nil
	}
	return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, current.Status)
}

func (ws *workSessionStore) ConsumeWorkSessionHop(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	// Atomic hop consumption: only while open AND hops_used < budget — the
	// guard lives inside the UPDATE, so concurrent consumers serialize in the
	// database and the budget can never overshoot.
	query := `
		UPDATE channel_work_sessions
		SET hops_used = hops_used + 1, updated_at = $3
		WHERE workspace_id = $1 AND id = $2 AND status = 'open' AND hops_used < budget
		RETURNING ` + workSessionColumns + `
	`
	updated, err := scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id, time.Now().UTC()))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	current, classErr := ws.classifyMiss(ctx, workspaceID, id)
	if classErr != nil {
		return nil, classErr
	}
	if current.Status == domain.WorkSessionClosed {
		return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, current.Status)
	}
	if current.Status == domain.WorkSessionOpen {
		// Open but out of budget.
		return nil, fmt.Errorf("%w: session %q has used %d of %d hops", domain.ErrWorkSessionHopUnavailable, id, current.HopsUsed, current.Budget)
	}
	// Paused: the hop cannot be consumed.
	return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionHopUnavailable, id, current.Status)
}

func (ws *workSessionStore) CloseWorkSession(ctx context.Context, workspaceID, id, summary string) (*domain.WorkSession, error) {
	// Any non-closed → closed; closed is terminal. The pause reason clears —
	// closed is a clean terminal state (the summary is the record).
	now := time.Now().UTC()
	query := `
		UPDATE channel_work_sessions
		SET status = 'closed', pause_reason = NULL, summary = $3, closed_at = $4, updated_at = $4
		WHERE workspace_id = $1 AND id = $2 AND status <> 'closed'
		RETURNING ` + workSessionColumns + `
	`
	updated, err := scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id, summary, now))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	// Zero rows: unknown id, or the session was already closed (terminal).
	current, classErr := ws.classifyMiss(ctx, workspaceID, id)
	if classErr != nil {
		return nil, classErr
	}
	return nil, fmt.Errorf("%w: session %q is %q", domain.ErrWorkSessionClosed, id, current.Status)
}

func (ws *workSessionStore) ListOpenWorkSessions(ctx context.Context) ([]domain.WorkSession, error) {
	query := `
		SELECT ` + workSessionColumns + `
		FROM channel_work_sessions
		WHERE status <> 'closed'
		ORDER BY created_at ASC, id ASC
	`
	rows, err := ws.db.Query(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	sessions := make([]domain.WorkSession, 0)
	for rows.Next() {
		session, err := scanWorkSession(rows)
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

// classifyMiss re-reads a session after a zero-row guarded UPDATE to classify
// the conflict: unknown id → ErrNotFound, closed → ErrWorkSessionClosed,
// otherwise the row itself (callers map non-open states to their specific
// sentinel).
func (ws *workSessionStore) classifyMiss(ctx context.Context, workspaceID, id string) (*domain.WorkSession, error) {
	query := `
		SELECT ` + workSessionColumns + `
		FROM channel_work_sessions
		WHERE workspace_id = $1 AND id = $2
	`
	return scanWorkSession(ws.db.QueryRow(ctx, query, workspaceID, id))
}
