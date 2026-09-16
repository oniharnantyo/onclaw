package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// heartbeatAutoPauseStreak is the failure-streak length at which a heartbeat
// auto-pauses (add-agent-heartbeat D12); kept in lockstep with the fake
// adapter's constant.
const heartbeatAutoPauseStreak = 5

// heartbeatStore implements storeport.HeartbeatStore for PostgreSQL
// (add-agent-heartbeat 2.2). Every query is workspace-scoped; the exception
// is ClaimDueHeartbeats, which is intentionally global — it serves the
// ticker and guards itself with FOR UPDATE SKIP LOCKED.
type heartbeatStore struct {
	db Executor
}

// NewHeartbeatStore creates a new HeartbeatStore with the given database executor.
func NewHeartbeatStore(db Executor) storeport.HeartbeatStore {
	return &heartbeatStore{db: db}
}

const heartbeatColumns = `
	id, workspace_id, agent_id, created_by, prompt, expr, active_start,
	active_end, delivery, enabled, next_tick_at, last_tick, failure_streak,
	created_at, updated_at
`

func scanHeartbeat(row pgx.Row) (*domain.Heartbeat, error) {
	var hb domain.Heartbeat
	var createdBy *string
	var delivery, lastTick []byte
	err := row.Scan(
		&hb.ID,
		&hb.WorkspaceID,
		&hb.AgentID,
		&createdBy,
		&hb.Prompt,
		&hb.Expr,
		&hb.ActiveStart,
		&hb.ActiveEnd,
		&delivery,
		&hb.Enabled,
		&hb.NextTickAt,
		&lastTick,
		&hb.FailureStreak,
		&hb.CreatedAt,
		&hb.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if createdBy != nil {
		hb.CreatedBy = createdBy
	}
	if hb.Delivery, err = unmarshalHeartbeatDelivery(delivery); err != nil {
		return nil, err
	}
	if hb.LastTick, err = unmarshalHeartbeatLastTick(lastTick); err != nil {
		return nil, err
	}
	return &hb, nil
}

// unmarshalHeartbeatDelivery decodes the delivery jsonb object.
func unmarshalHeartbeatDelivery(data []byte) (domain.HeartbeatDelivery, error) {
	var d domain.HeartbeatDelivery
	if len(data) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("%w: invalid delivery jsonb: %v", domain.ErrInvalid, err)
	}
	return d, nil
}

// unmarshalHeartbeatLastTick decodes the last_tick jsonb object; NULL reads
// back as nil (no outcome yet).
func unmarshalHeartbeatLastTick(data []byte) (*domain.HeartbeatLastRun, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var lt domain.HeartbeatLastRun
	if err := json.Unmarshal(data, &lt); err != nil {
		return nil, fmt.Errorf("%w: invalid last_tick jsonb: %v", domain.ErrInvalid, err)
	}
	return &lt, nil
}

// marshalHeartbeatDelivery encodes the delivery jsonb object.
func marshalHeartbeatDelivery(d domain.HeartbeatDelivery) ([]byte, error) {
	return json.Marshal(d)
}

// marshalHeartbeatLastTick encodes the last_tick jsonb; nil persists as SQL
// NULL (absence of an outcome is a normal state).
func marshalHeartbeatLastTick(lt *domain.HeartbeatLastRun) ([]byte, error) {
	if lt == nil {
		return nil, nil
	}
	return json.Marshal(lt)
}

// emptyClockAsNil maps an empty optional clock string to SQL NULL: ” would
// otherwise count as "set" against chk_agent_heartbeats_active_hours_shape's
// both-or-neither pairing.
func emptyClockAsNil(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return v
}

// GetHeartbeat returns the agent's heartbeat; absence — including a foreign
// workspace's agent — reads as (nil, nil).
func (hs *heartbeatStore) GetHeartbeat(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error) {
	if workspaceID == "" || agentID == "" {
		return nil, nil
	}

	query := `
		SELECT ` + heartbeatColumns + `
		FROM agent_heartbeats
		WHERE workspace_id = $1 AND agent_id = $2
	`
	hb, err := scanHeartbeat(hs.db.QueryRow(ctx, query, workspaceID, agentID))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return hb, nil
}

// PutHeartbeat create-or-replaces the agent's single heartbeat (design D1:
// exactly one per agent): the INSERT rides the
// uq_agent_heartbeats_workspace_id_agent_id constraint with DO UPDATE, so a
// second heartbeat for an agent can never exist. The store persists the
// struct as given — derivation is caller-side. Identity fields (id,
// created_by, created_at) are immutable on replace and returned so the
// caller's struct stays faithful.
func (hs *heartbeatStore) PutHeartbeat(ctx context.Context, workspaceID, agentID string, hb *domain.Heartbeat) error {
	if hb == nil || workspaceID == "" || agentID == "" {
		return domain.ErrInvalid
	}
	hb.WorkspaceID = workspaceID
	hb.AgentID = agentID

	// FK parity: workspace, agent, and creator must exist (the agent FK alone
	// would admit cross-workspace agents).
	var one bool
	err := hs.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, workspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	err = hs.db.QueryRow(ctx,
		`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
		workspaceID, agentID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
		}
		return convertError(err)
	}
	if hb.CreatedBy != nil && *hb.CreatedBy != "" {
		err = hs.db.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, *hb.CreatedBy).Scan(&one)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: user not found", domain.ErrNotFound)
			}
			return convertError(err)
		}
	}

	now := time.Now().UTC()
	if hb.ID == "" {
		hb.ID = uuid.NewString()
	}
	if hb.CreatedAt.IsZero() {
		hb.CreatedAt = now
	}
	if hb.UpdatedAt.IsZero() {
		hb.UpdatedAt = now
	}

	delivery, err := marshalHeartbeatDelivery(hb.Delivery)
	if err != nil {
		return convertError(err)
	}
	lastTick, err := marshalHeartbeatLastTick(hb.LastTick)
	if err != nil {
		return convertError(err)
	}

	query := `
		INSERT INTO agent_heartbeats (
			id, workspace_id, agent_id, created_by, prompt, expr, active_start,
			active_end, delivery, enabled, next_tick_at, last_tick,
			failure_streak, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT (workspace_id, agent_id) DO UPDATE SET
			prompt = EXCLUDED.prompt,
			expr = EXCLUDED.expr,
			active_start = EXCLUDED.active_start,
			active_end = EXCLUDED.active_end,
			delivery = EXCLUDED.delivery,
			enabled = EXCLUDED.enabled,
			next_tick_at = EXCLUDED.next_tick_at,
			last_tick = EXCLUDED.last_tick,
			failure_streak = EXCLUDED.failure_streak,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at, created_by
	`
	err = hs.db.QueryRow(ctx, query,
		hb.ID,
		hb.WorkspaceID,
		hb.AgentID,
		hb.CreatedBy,
		hb.Prompt,
		hb.Expr,
		emptyClockAsNil(hb.ActiveStart),
		emptyClockAsNil(hb.ActiveEnd),
		delivery,
		hb.Enabled,
		hb.NextTickAt,
		lastTick,
		hb.FailureStreak,
		hb.CreatedAt,
		hb.UpdatedAt,
	).Scan(&hb.ID, &hb.CreatedAt, &hb.CreatedBy)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// DeleteHeartbeat removes the agent's heartbeat; its tick records die with it
// (ON DELETE CASCADE). An absent heartbeat is domain.ErrNotFound.
func (hs *heartbeatStore) DeleteHeartbeat(ctx context.Context, workspaceID, agentID string) error {
	if workspaceID == "" || agentID == "" {
		return domain.ErrNotFound
	}

	tag, err := hs.db.Exec(ctx,
		`DELETE FROM agent_heartbeats WHERE workspace_id = $1 AND agent_id = $2`,
		workspaceID, agentID,
	)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClaimDueHeartbeats atomically claims due heartbeats (design D14): one
// transaction locks the due rows FOR UPDATE SKIP LOCKED — so concurrent
// claimers can never fire the same tick — and advances each row's
// next_tick_at to its next future occurrence (computed in the workspace
// timezone via domain.NextRun) in that same transaction, so a catch-up after
// downtime fires once and reschedules without replaying.
func (hs *heartbeatStore) ClaimDueHeartbeats(ctx context.Context, now time.Time, limit int) ([]storeport.HeartbeatClaim, error) {
	if limit <= 0 {
		return []storeport.HeartbeatClaim{}, nil
	}

	tx, err := hs.db.(txBeginner).Begin(ctx)
	if err != nil {
		return nil, convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the due rows. SKIP LOCKED is the correctness linchpin: a second
	// claimer running against the same due rows skips the locked ones
	// instead of blocking, so a tick is claimed exactly once.
	rows, err := tx.Query(ctx, `
		SELECT `+heartbeatColumns+`
		FROM agent_heartbeats
		WHERE enabled AND next_tick_at IS NOT NULL AND next_tick_at <= $1
		ORDER BY next_tick_at ASC, id ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, now, limit)
	if err != nil {
		return nil, convertError(err)
	}

	claimed := make([]*domain.Heartbeat, 0, limit)
	for rows.Next() {
		hb, err := scanHeartbeat(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, hb)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, convertError(err)
	}
	rows.Close()

	claims := make([]storeport.HeartbeatClaim, 0, len(claimed))
	for _, hb := range claimed {
		// The workspace tz lookup rides the same transaction so it reads the
		// same snapshot as the claimed rows.
		loc, err := workspaceLocation(ctx, tx, hb.WorkspaceID)
		if err != nil {
			return nil, err
		}
		next, err := domain.NextRun(hb.Expr, now, loc)
		if err != nil {
			return nil, err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE agent_heartbeats
			SET next_tick_at = $3,
			    updated_at = $4
			WHERE id = $1 AND workspace_id = $2
		`, hb.ID, hb.WorkspaceID, next, now)
		if err != nil {
			return nil, convertError(err)
		}
		if tag.RowsAffected() == 0 {
			// The row vanished inside the transaction (agent/workspace
			// cascade by a concurrent writer): skip instead of claiming a
			// ghost.
			continue
		}
		hb.NextTickAt = next
		claims = append(claims, storeport.HeartbeatClaim{Heartbeat: hb})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, convertError(err)
	}
	return claims, nil
}

// StartHeartbeatRun inserts a tick record with status running. The heartbeat
// must exist within the run's workspace (the plain FK alone would admit
// cross-workspace heartbeat references).
func (hs *heartbeatStore) StartHeartbeatRun(ctx context.Context, run *domain.HeartbeatRun) error {
	if run == nil || run.WorkspaceID == "" || run.HeartbeatID == "" || run.SessionID == "" {
		return domain.ErrInvalid
	}

	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	run.Status = domain.HeartbeatRunStatusRunning
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	var one bool
	err := hs.db.QueryRow(ctx,
		`SELECT true FROM agent_heartbeats WHERE workspace_id = $1 AND id = $2`,
		run.WorkspaceID, run.HeartbeatID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: heartbeat not found in workspace", domain.ErrNotFound)
		}
		return convertError(err)
	}

	query := `
		INSERT INTO heartbeat_runs (
			id, workspace_id, heartbeat_id, agent_id, session_id, trigger,
			status, started_at, duration_ms, tokens_used, delivery_status, error
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
	`
	_, err = hs.db.Exec(ctx, query,
		run.ID,
		run.WorkspaceID,
		run.HeartbeatID,
		run.AgentID,
		run.SessionID,
		run.Trigger,
		run.Status,
		run.StartedAt,
		run.DurationMS,
		run.TokensUsed,
		run.DeliveryStatus,
		run.Error,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

// FinishHeartbeatRun writes the tick outcome — including the turn's persisted
// Langfuse trace id (000050 precedent) — and mirrors it into the heartbeat's
// last_tick snapshot in one transaction: the drain writes both once, and no
// reader may observe one without the other.
func (hs *heartbeatStore) FinishHeartbeatRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string, traceID string) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrNotFound
	}

	tx, err := hs.db.(txBeginner).Begin(ctx)
	if err != nil {
		return convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var heartbeatID, sessionID, trigger string
	var startedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE heartbeat_runs
		SET status = $3,
		    duration_ms = $4,
		    tokens_used = $5,
		    delivery_status = $6,
		    error = $7,
		    trace_id = $8
		WHERE workspace_id = $1 AND id = $2
		RETURNING heartbeat_id, session_id, trigger, started_at
	`, workspaceID, runID, status, durationMS, tokensUsed, deliveryStatus, errMsg, traceID,
	).Scan(&heartbeatID, &sessionID, &trigger, &startedAt)
	if err != nil {
		return convertError(err)
	}

	lastTick, err := marshalHeartbeatLastTick(&domain.HeartbeatLastRun{
		Status:         status,
		Trigger:        trigger,
		StartedAt:      startedAt,
		DurationMS:     durationMS,
		TokensUsed:     tokensUsed,
		SessionID:      sessionID,
		DeliveryStatus: deliveryStatus,
		Error:          errMsg,
	})
	if err != nil {
		return convertError(err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE agent_heartbeats
		SET last_tick = $3,
		    updated_at = $4
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, heartbeatID, lastTick, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}

	return tx.Commit(ctx)
}

// ApplyHeartbeatOutcome applies the failure-streak accounting in one atomic
// statement (design D12): completed resets the streak, failed increments it
// and — at the fifth consecutive failure — auto-pauses the heartbeat
// (enabled off, next tick cleared). Every other status — blocked policy
// grief, cancelled, skipped guards — leaves the streak untouched. The
// RETURNING expression reads the post-update streak: for a failed outcome it
// is the pre-update streak plus one, so it crosses the auto-pause length
// exactly when this very failure triggered the pause.
func (hs *heartbeatStore) ApplyHeartbeatOutcome(ctx context.Context, workspaceID, heartbeatID string, status string) (bool, error) {
	if workspaceID == "" || heartbeatID == "" {
		return false, domain.ErrNotFound
	}

	query := `
		UPDATE agent_heartbeats
		SET failure_streak = CASE
				WHEN $3 = 'completed' THEN 0
				WHEN $3 = 'failed' THEN failure_streak + 1
				ELSE failure_streak
			END,
		    enabled = CASE
		    	WHEN $3 = 'failed' AND failure_streak + 1 >= $4 THEN false
		    	ELSE enabled
		    END,
		    next_tick_at = CASE
		    	WHEN $3 = 'failed' AND failure_streak + 1 >= $4 THEN NULL
		    	ELSE next_tick_at
		    END,
		    updated_at = $5
		WHERE workspace_id = $1 AND id = $2
		RETURNING $3 = 'failed' AND failure_streak >= $4
	`
	var paused bool
	err := hs.db.QueryRow(ctx, query,
		workspaceID,
		heartbeatID,
		status,
		heartbeatAutoPauseStreak,
		time.Now().UTC(),
	).Scan(&paused)
	if err != nil {
		return false, convertError(err)
	}
	return paused, nil
}

// ListHeartbeatRuns returns the heartbeat's tick records newest-first with
// the total across all pages in the same round trip.
func (hs *heartbeatStore) ListHeartbeatRuns(ctx context.Context, workspaceID, heartbeatID string, limit, offset int) ([]domain.HeartbeatRun, int, error) {
	if workspaceID == "" || heartbeatID == "" {
		return []domain.HeartbeatRun{}, 0, nil
	}

	query := `
		SELECT id, workspace_id, heartbeat_id, agent_id, session_id, trigger,
		       status, started_at, duration_ms, tokens_used, delivery_status,
		       error, trace_id, COUNT(*) OVER () AS total
		FROM heartbeat_runs
		WHERE workspace_id = $1 AND heartbeat_id = $2
		ORDER BY started_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`
	rows, err := hs.db.Query(ctx, query, workspaceID, heartbeatID, limit, offset)
	if err != nil {
		return nil, 0, convertError(err)
	}
	defer rows.Close()

	runs := make([]domain.HeartbeatRun, 0)
	total := 0
	for rows.Next() {
		var r domain.HeartbeatRun
		if err := rows.Scan(
			&r.ID,
			&r.WorkspaceID,
			&r.HeartbeatID,
			&r.AgentID,
			&r.SessionID,
			&r.Trigger,
			&r.Status,
			&r.StartedAt,
			&r.DurationMS,
			&r.TokensUsed,
			&r.DeliveryStatus,
			&r.Error,
			&r.TraceID,
			&total,
		); err != nil {
			return nil, 0, convertError(err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, convertError(err)
	}
	return runs, total, nil
}
