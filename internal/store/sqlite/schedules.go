package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type scheduleStore struct{ e executor }

const scheduleColumns = `id, name, agent_id, prompt, cron, enabled, next_fire_at,
last_run_at, last_run_status, last_run_error, last_run_session_id, created_at, updated_at`

// nullTime encodes a *time.Time as a nullable column value: nil is NULL,
// anything else RFC3339Nano in UTC (see formatTime).
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(t.UTC())
}

func (s *scheduleStore) Create(ctx context.Context, sched *store.Schedule) error {
	if sched.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create schedule: %w", err)
		}
		sched.ID = id
	}
	stampCreated(&sched.CreatedAt, &sched.UpdatedAt)

	_, err := s.e.ExecContext(ctx, `
INSERT INTO schedules (`+scheduleColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sched.ID, sched.Name, sched.AgentID, sched.Prompt, sched.Cron, boolInt(sched.Enabled),
		nullTime(sched.NextFireAt), nullTime(sched.LastRunAt),
		sched.LastRunStatus, sched.LastRunError, sched.LastRunSessionID,
		formatTime(sched.CreatedAt), formatTime(sched.UpdatedAt),
	)
	if err != nil {
		// The unique index on name rides the column's NOCASE collation, so
		// the violation fires on case-insensitive duplicates too.
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("create schedule: %w", store.ErrDuplicate)
		}
		return fmt.Errorf("create schedule: %w", err)
	}
	return nil
}

func (s *scheduleStore) ByID(ctx context.Context, id string) (*store.Schedule, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = ?`, id)
	sched, err := scanSchedule(row)
	if err != nil {
		return nil, readErr("get schedule", err)
	}
	return sched, nil
}

// ByName matches on the NOCASE name column, so the comparison is
// case-insensitive.
func (s *scheduleStore) ByName(ctx context.Context, name string) (*store.Schedule, error) {
	row := s.e.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE name = ?`, name)
	sched, err := scanSchedule(row)
	if err != nil {
		return nil, readErr("get schedule", err)
	}
	return sched, nil
}

func (s *scheduleStore) List(ctx context.Context) ([]store.Schedule, error) {
	// name carries COLLATE NOCASE, so the sort is case-insensitive.
	rows, err := s.e.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]store.Schedule, 0)
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("list schedules: %w", err)
		}
		schedules = append(schedules, *sched)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return schedules, nil
}

// ListDue selects the enabled schedules holding a due slot. The <= now
// comparison runs in Go, not SQL: RFC3339Nano trims trailing zeros, so the
// TEXT ordering puts "…09:00:00Z" after "…09:00:00.5Z" ('Z' > '.') and a
// lexical WHERE would miss whole-second slots.
func (s *scheduleStore) ListDue(ctx context.Context, now time.Time) ([]store.Schedule, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+scheduleColumns+` FROM schedules
WHERE enabled = 1 AND next_fire_at IS NOT NULL
ORDER BY next_fire_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]store.Schedule, 0)
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("list due schedules: %w", err)
		}
		if !sched.NextFireAt.After(now) {
			schedules = append(schedules, *sched)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	return schedules, nil
}

func (s *scheduleStore) Update(ctx context.Context, sched *store.Schedule) error {
	sched.UpdatedAt = nowUTC()
	res, err := s.e.ExecContext(ctx, `
UPDATE schedules SET name = ?, agent_id = ?, prompt = ?, cron = ?, enabled = ?, next_fire_at = ?, updated_at = ?
WHERE id = ?`,
		sched.Name, sched.AgentID, sched.Prompt, sched.Cron, boolInt(sched.Enabled),
		nullTime(sched.NextFireAt), formatTime(sched.UpdatedAt), sched.ID,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("update schedule %s: %w", sched.ID, store.ErrDuplicate)
		}
		return fmt.Errorf("update schedule %s: %w", sched.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update schedule %s: %w", sched.ID, store.ErrNotFound)
	}
	return nil
}

func (s *scheduleStore) Delete(ctx context.Context, id string) error {
	res, err := s.e.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete schedule %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// RecordFire writes the last-run state and the advanced slot in one UPDATE
// (design D3): the mark-and-advance lands atomically or not at all.
func (s *scheduleStore) RecordFire(ctx context.Context, id string, fire store.ScheduleFire) error {
	res, err := s.e.ExecContext(ctx, `
UPDATE schedules SET last_run_at = ?, last_run_status = ?, last_run_error = ?,
last_run_session_id = ?, next_fire_at = ?, updated_at = ?
WHERE id = ?`,
		formatTime(fire.At.UTC()), fire.Status, fire.Error, fire.SessionID,
		nullTime(fire.NextFireAt), formatTime(nowUTC()), id,
	)
	if err != nil {
		return fmt.Errorf("record schedule fire %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("record schedule fire %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanSchedule scans one schedules row from a single- or multi-row result.
func scanSchedule(row scanner) (*store.Schedule, error) {
	var sched store.Schedule
	var nextFireAt, lastRunAt sql.NullString
	var createdAt, updatedAt string

	err := row.Scan(&sched.ID, &sched.Name, &sched.AgentID, &sched.Prompt, &sched.Cron, &sched.Enabled,
		&nextFireAt, &lastRunAt, &sched.LastRunStatus, &sched.LastRunError, &sched.LastRunSessionID,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if nextFireAt.Valid {
		t, err := parseTime(nextFireAt.String)
		if err != nil {
			return nil, err
		}
		sched.NextFireAt = &t
	}
	if lastRunAt.Valid {
		t, err := parseTime(lastRunAt.String)
		if err != nil {
			return nil, err
		}
		sched.LastRunAt = &t
	}
	if sched.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if sched.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &sched, nil
}
