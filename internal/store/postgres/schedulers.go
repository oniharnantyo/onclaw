package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// schedulerMissedGraceWindow is how long past its instant a once scheduler may
// still fire on catch-up; beyond it the claim archives the row as missed
// instead of firing (kept in lockstep with the fake adapter's constant).
const schedulerMissedGraceWindow = time.Hour

// txBeginner is the transaction-start capability both Executor
// implementations provide: *pgxpool.Pool and pgx.Tx. ClaimDueSchedulers and
// FinishSchedulerRun need it — their row writes are atomic only inside one
// transaction.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// schedulerStore implements storeport.SchedulerStore for PostgreSQL
// (integrate-scheduler 1.6). Every query is workspace-scoped; the exception
// is ClaimDueSchedulers, which is intentionally global — it serves the ticker
// and guards itself with FOR UPDATE SKIP LOCKED.
type schedulerStore struct {
	db Executor
}

// NewSchedulerStore creates a new SchedulerStore with the given database executor.
func NewSchedulerStore(db Executor) storeport.SchedulerStore {
	return &schedulerStore{db: db}
}

const schedulerColumns = `
	id, workspace_id, agent_id, created_by, name, prompt, kind, expr, run_at,
	delivery, enabled, next_run_at, last_run, created_at, updated_at
`

func scanScheduler(row pgx.Row) (*domain.Scheduler, error) {
	var s domain.Scheduler
	var createdBy *string
	var expr *string
	var delivery, lastRun []byte
	err := row.Scan(
		&s.ID,
		&s.WorkspaceID,
		&s.AgentID,
		&createdBy,
		&s.Name,
		&s.Prompt,
		&s.Kind,
		&expr,
		&s.RunAt,
		&delivery,
		&s.Enabled,
		&s.NextRunAt,
		&lastRun,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if createdBy != nil {
		s.CreatedBy = createdBy
	}
	if expr != nil {
		s.Expr = *expr
	}
	if s.Delivery, err = unmarshalSchedulerDelivery(delivery); err != nil {
		return nil, err
	}
	if s.LastRun, err = unmarshalSchedulerLastRun(lastRun); err != nil {
		return nil, err
	}
	return &s, nil
}

// unmarshalSchedulerDelivery decodes the delivery jsonb object.
func unmarshalSchedulerDelivery(data []byte) (domain.SchedulerDelivery, error) {
	var d domain.SchedulerDelivery
	if len(data) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("%w: invalid delivery jsonb: %v", domain.ErrInvalid, err)
	}
	return d, nil
}

// unmarshalSchedulerLastRun decodes the last_run jsonb object; NULL reads
// back as nil (no outcome yet).
func unmarshalSchedulerLastRun(data []byte) (*domain.SchedulerLastRun, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var lr domain.SchedulerLastRun
	if err := json.Unmarshal(data, &lr); err != nil {
		return nil, fmt.Errorf("%w: invalid last_run jsonb: %v", domain.ErrInvalid, err)
	}
	return &lr, nil
}

// isSchedulerNameViolation reports whether the error is a unique violation on
// uq_schedulers_workspace_id_agent_id_name, as opposed to the primary key.
func isSchedulerNameViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation &&
			pgErr.ConstraintName == "uq_schedulers_workspace_id_agent_id_name"
	}
	return false
}

// workspaceLocation resolves the workspace's IANA timezone for schedule
// interpretation, falling back to UTC when the zone cannot be loaded
// (workspace timezones are IANA-validated at the domain layer, so this is a
// corruption guard, not a validation path). Must run inside the caller's
// transaction when invoked from ClaimDueSchedulers so the tz rides the same
// snapshot as the claimed rows.
func workspaceLocation(ctx context.Context, db Executor, workspaceID string) (*time.Location, error) {
	var tz *string
	err := db.QueryRow(ctx,
		`SELECT timezone FROM workspaces WHERE id = $1`,
		workspaceID,
	).Scan(&tz)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.UTC, nil
		}
		return nil, convertError(err)
	}
	if tz == nil || *tz == "" {
		return time.UTC, nil
	}
	if loc, err := time.LoadLocation(*tz); err == nil {
		return loc, nil
	}
	return time.UTC, nil
}

// computeNextRunAt derives the stored next fire time: absent while
// paused/archived, the one-shot instant, or the next future occurrence of the
// expression in the workspace timezone.
func computeNextRunAt(ctx context.Context, db Executor, s *domain.Scheduler, now time.Time) (*time.Time, error) {
	if !s.Enabled {
		return nil, nil
	}
	if s.Kind == domain.SchedulerKindOnce {
		t := s.RunAt.UTC()
		return &t, nil
	}
	loc, err := workspaceLocation(ctx, db, s.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return domain.NextRun(s.Expr, now, loc)
}

// marshalSchedulerDelivery encodes the delivery jsonb object.
func marshalSchedulerDelivery(d domain.SchedulerDelivery) ([]byte, error) {
	return json.Marshal(d)
}

// marshalSchedulerLastRun encodes the last_run jsonb; nil persists as SQL NULL
// (absence of an outcome is a normal state).
func marshalSchedulerLastRun(lr *domain.SchedulerLastRun) ([]byte, error) {
	if lr == nil {
		return nil, nil
	}
	return json.Marshal(lr)
}

func (ss *schedulerStore) CreateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error {
	if s == nil {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	s.WorkspaceID = workspaceID
	if err := domain.ValidateScheduler(s, now, true); err != nil {
		return err
	}

	// FK parity: workspace, agent, and creator must exist (the agent FK alone
	// would admit cross-workspace agents).
	var one bool
	err := ss.db.QueryRow(ctx, `SELECT true FROM workspaces WHERE id = $1`, workspaceID).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
		}
		return convertError(err)
	}
	err = ss.db.QueryRow(ctx,
		`SELECT true FROM agents WHERE workspace_id = $1 AND id = $2`,
		workspaceID, s.AgentID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
		}
		return convertError(err)
	}
	if s.CreatedBy != nil && *s.CreatedBy != "" {
		err = ss.db.QueryRow(ctx, `SELECT true FROM users WHERE id = $1`, *s.CreatedBy).Scan(&one)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: user not found", domain.ErrNotFound)
			}
			return convertError(err)
		}
	}

	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	next, err := computeNextRunAt(ctx, ss.db, s, now)
	if err != nil {
		return err
	}
	s.NextRunAt = next

	delivery, err := marshalSchedulerDelivery(s.Delivery)
	if err != nil {
		return convertError(err)
	}
	lastRun, err := marshalSchedulerLastRun(s.LastRun)
	if err != nil {
		return convertError(err)
	}

	query := `
		INSERT INTO schedulers (
			id, workspace_id, agent_id, created_by, name, prompt, kind, expr, run_at,
			delivery, enabled, next_run_at, last_run, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
	`
	_, err = ss.db.Exec(ctx, query,
		s.ID,
		s.WorkspaceID,
		s.AgentID,
		s.CreatedBy,
		s.Name,
		s.Prompt,
		s.Kind,
		emptyAsNil(s.Expr),
		s.RunAt,
		delivery,
		s.Enabled,
		s.NextRunAt,
		lastRun,
		s.CreatedAt,
		s.UpdatedAt,
	)
	if err != nil {
		if isSchedulerNameViolation(err) {
			return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
		}
		return convertError(err)
	}
	return nil
}

// emptyAsNil maps the empty string to SQL NULL for the nullable schedule
// columns: an empty expr on a once row would violate chk_schedulers_kind_shape.
func emptyAsNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (ss *schedulerStore) GetScheduler(ctx context.Context, workspaceID, id string) (*domain.Scheduler, error) {
	if workspaceID == "" || id == "" {
		return nil, nil
	}

	query := `
		SELECT ` + schedulerColumns + `
		FROM schedulers
		WHERE workspace_id = $1 AND id = $2
	`
	s, err := scanScheduler(ss.db.QueryRow(ctx, query, workspaceID, id))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (ss *schedulerStore) ListSchedulers(ctx context.Context, workspaceID string) ([]domain.Scheduler, error) {
	if workspaceID == "" {
		return []domain.Scheduler{}, nil
	}

	query := `
		SELECT ` + schedulerColumns + `
		FROM schedulers
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := ss.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	schedulers := make([]domain.Scheduler, 0)
	for rows.Next() {
		s, err := scanScheduler(rows)
		if err != nil {
			return nil, err
		}
		schedulers = append(schedulers, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return schedulers, nil
}

func (ss *schedulerStore) UpdateScheduler(ctx context.Context, workspaceID string, s *domain.Scheduler) error {
	if s == nil || s.ID == "" {
		return domain.ErrInvalid
	}
	now := time.Now().UTC()
	s.WorkspaceID = workspaceID
	if err := domain.ValidateScheduler(s, now, false); err != nil {
		return err
	}

	next, err := computeNextRunAt(ctx, ss.db, s, now)
	if err != nil {
		return err
	}
	delivery, err := marshalSchedulerDelivery(s.Delivery)
	if err != nil {
		return convertError(err)
	}
	lastRun, err := marshalSchedulerLastRun(s.LastRun)
	if err != nil {
		return convertError(err)
	}

	// Editable fields only: created_by and created_at are immutable and
	// returned to keep the caller's struct faithful.
	query := `
		UPDATE schedulers
		SET agent_id = $3,
		    name = $4,
		    prompt = $5,
		    kind = $6,
		    expr = $7,
		    run_at = $8,
		    delivery = $9,
		    enabled = $10,
		    next_run_at = $11,
		    last_run = $12,
		    updated_at = $13
		WHERE workspace_id = $1 AND id = $2
		RETURNING created_at, created_by
	`
	err = ss.db.QueryRow(ctx, query,
		s.WorkspaceID,
		s.ID,
		s.AgentID,
		s.Name,
		s.Prompt,
		s.Kind,
		emptyAsNil(s.Expr),
		s.RunAt,
		delivery,
		s.Enabled,
		next,
		lastRun,
		now,
	).Scan(&s.CreatedAt, &s.CreatedBy)
	if err != nil {
		if isSchedulerNameViolation(err) {
			return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
		}
		return convertError(err)
	}
	s.NextRunAt = next
	return nil
}

func (ss *schedulerStore) DeleteScheduler(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM schedulers
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := ss.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClaimDueSchedulers atomically claims due schedulers (design D3): one
// transaction locks the due rows FOR UPDATE SKIP LOCKED — so concurrent
// claimers can never fire the same occurrence — advances each recurring row's
// next_run_at to its next future occurrence (computed in the workspace
// timezone via domain.NextRun), archives fired once rows, and archives
// once rows overdue beyond the grace window as missed instead of firing.
func (ss *schedulerStore) ClaimDueSchedulers(ctx context.Context, now time.Time, limit int) ([]storeport.SchedulerClaim, error) {
	if limit <= 0 {
		return []storeport.SchedulerClaim{}, nil
	}

	tx, err := ss.db.(txBeginner).Begin(ctx)
	if err != nil {
		return nil, convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the due rows: recurring next_run_at due, or once run_at due.
	// SKIP LOCKED is the correctness linchpin (design D3): a second claimer
	// running against the same due rows skips the locked ones instead of
	// blocking, so an occurrence is claimed exactly once.
	rows, err := tx.Query(ctx, `
		SELECT `+schedulerColumns+`
		FROM schedulers
		WHERE enabled AND (
			(kind = 'recurring' AND next_run_at IS NOT NULL AND next_run_at <= $1) OR
			(kind = 'once' AND run_at IS NOT NULL AND run_at <= $1)
		)
		ORDER BY COALESCE(next_run_at, run_at) ASC, id ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, now, limit)
	if err != nil {
		return nil, convertError(err)
	}

	claimed := make([]*domain.Scheduler, 0, limit)
	for rows.Next() {
		s, err := scanScheduler(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, convertError(err)
	}
	rows.Close()

	claims := make([]storeport.SchedulerClaim, 0, len(claimed))
	for _, s := range claimed {
		missed := false
		var next *time.Time
		switch {
		case s.Kind == domain.SchedulerKindOnce && now.Sub(*s.RunAt) > schedulerMissedGraceWindow:
			// Overdue beyond the grace window: archive as missed, never fire.
			missed = true
			s.Enabled = false
			started := s.RunAt.UTC()
			s.LastRun = &domain.SchedulerLastRun{
				Status:    domain.SchedulerRunStatusMissed,
				Trigger:   domain.SchedulerTriggerScheduled,
				StartedAt: started,
			}
		default:
			// Fire once: recurring reschedules to the next future occurrence
			// at claim time; a once row archives after firing.
			s.Enabled = s.Kind == domain.SchedulerKindRecurring
			next, err = computeNextRunAt(ctx, tx, s, now)
			if err != nil {
				return nil, err
			}
		}

		lastRun, err := marshalSchedulerLastRun(s.LastRun)
		if err != nil {
			return nil, convertError(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE schedulers
			SET enabled = $3,
			    next_run_at = $4,
			    last_run = $5,
			    updated_at = $6
			WHERE id = $1 AND workspace_id = $2
		`, s.ID, s.WorkspaceID, s.Enabled, next, lastRun, now)
		if err != nil {
			return nil, convertError(err)
		}
		if tag.RowsAffected() == 0 {
			// The row vanished inside the transaction (agent/workspace
			// cascade by a concurrent writer): skip instead of claiming a
			// ghost.
			continue
		}
		s.NextRunAt = next
		claims = append(claims, storeport.SchedulerClaim{Scheduler: s, Missed: missed})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, convertError(err)
	}
	return claims, nil
}

func (ss *schedulerStore) StartSchedulerRun(ctx context.Context, run *domain.SchedulerRun) error {
	if run == nil || run.WorkspaceID == "" || run.SchedulerID == "" || run.SessionID == "" {
		return domain.ErrInvalid
	}

	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	run.Status = domain.SchedulerRunStatusRunning
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	// The scheduler must exist within the run's workspace (the plain FK
	// alone would admit cross-workspace scheduler references).
	var one bool
	err := ss.db.QueryRow(ctx,
		`SELECT true FROM schedulers WHERE workspace_id = $1 AND id = $2`,
		run.WorkspaceID, run.SchedulerID,
	).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: scheduler not found in workspace", domain.ErrNotFound)
		}
		return convertError(err)
	}

	query := `
		INSERT INTO scheduler_runs (
			id, workspace_id, scheduler_id, session_id, trigger, status,
			started_at, duration_ms, tokens_used, delivery_status, error
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`
	_, err = ss.db.Exec(ctx, query,
		run.ID,
		run.WorkspaceID,
		run.SchedulerID,
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

// FinishSchedulerRun writes the run outcome and mirrors it into the
// scheduler's last_run column in one transaction — the drain writes both
// once, and no reader may observe one without the other.
func (ss *schedulerStore) FinishSchedulerRun(ctx context.Context, workspaceID, runID string, status string, durationMS int64, tokensUsed int, deliveryStatus string, errMsg string) error {
	if workspaceID == "" || runID == "" {
		return domain.ErrNotFound
	}

	tx, err := ss.db.(txBeginner).Begin(ctx)
	if err != nil {
		return convertError(err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var schedulerID, sessionID, trigger string
	var startedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE scheduler_runs
		SET status = $3,
		    duration_ms = $4,
		    tokens_used = $5,
		    delivery_status = $6,
		    error = $7
		WHERE workspace_id = $1 AND id = $2
		RETURNING scheduler_id, session_id, trigger, started_at
	`, workspaceID, runID, status, durationMS, tokensUsed, deliveryStatus, errMsg,
	).Scan(&schedulerID, &sessionID, &trigger, &startedAt)
	if err != nil {
		return convertError(err)
	}

	lastRun, err := marshalSchedulerLastRun(&domain.SchedulerLastRun{
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
		UPDATE schedulers
		SET last_run = $3,
		    updated_at = $4
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, schedulerID, lastRun, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}

	return tx.Commit(ctx)
}

func (ss *schedulerStore) ListSchedulerRuns(ctx context.Context, workspaceID, schedulerID string, limit, offset int) ([]domain.SchedulerRun, int, error) {
	if workspaceID == "" || schedulerID == "" {
		return []domain.SchedulerRun{}, 0, nil
	}

	// Newest first with the total across all pages in the same round trip.
	query := `
		SELECT id, workspace_id, scheduler_id, session_id, trigger, status,
		       started_at, duration_ms, tokens_used, delivery_status, error,
		       COUNT(*) OVER () AS total
		FROM scheduler_runs
		WHERE workspace_id = $1 AND scheduler_id = $2
		ORDER BY started_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`
	rows, err := ss.db.Query(ctx, query, workspaceID, schedulerID, limit, offset)
	if err != nil {
		return nil, 0, convertError(err)
	}
	defer rows.Close()

	runs := make([]domain.SchedulerRun, 0)
	total := 0
	for rows.Next() {
		var r domain.SchedulerRun
		if err := rows.Scan(
			&r.ID,
			&r.WorkspaceID,
			&r.SchedulerID,
			&r.SessionID,
			&r.Trigger,
			&r.Status,
			&r.StartedAt,
			&r.DurationMS,
			&r.TokensUsed,
			&r.DeliveryStatus,
			&r.Error,
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
