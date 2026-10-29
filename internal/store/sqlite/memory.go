package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

type memoryStore struct{ e executor }

const memoryTraceColumns = `id, agent_id, session_id, turn_id, origin, query, hits, created_at`

const memoryNoteStateColumns = `agent_id, note_name, retention, recall_count, distinct_queries,
last_recall, first_seen`

const memoryStagingColumns = `agent_id, source_ref, signals, first_staged, promoted`

const memorySweepColumns = `agent_id, watermark, last_start, last_finish, outcome, counts`

func (s *memoryStore) AppendTrace(ctx context.Context, t store.MemoryTrace) (store.MemoryTrace, error) {
	if t.ID == "" {
		id, err := newID()
		if err != nil {
			return store.MemoryTrace{}, fmt.Errorf("append memory trace: %w", err)
		}
		t.ID = id
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = nowUTC()
	}
	if t.Hits == nil {
		t.Hits = []store.MemoryTraceHit{}
	}

	hits, err := jsonMarshal(t.Hits)
	if err != nil {
		return store.MemoryTrace{}, fmt.Errorf("append memory trace: %w", err)
	}

	_, err = s.e.ExecContext(ctx, `
INSERT INTO memory_traces (`+memoryTraceColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.AgentID, t.SessionID, t.TurnID, t.Origin, t.Query, hits, formatTime(t.CreatedAt),
	)
	if err != nil {
		return store.MemoryTrace{}, fmt.Errorf("append memory trace: %w", err)
	}
	return t, nil
}

func (s *memoryStore) ListTracesSince(ctx context.Context, agentID string, since time.Time) ([]store.MemoryTrace, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+memoryTraceColumns+` FROM memory_traces
WHERE agent_id = ?
ORDER BY created_at, id`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list memory traces: %w", err)
	}
	defer rows.Close()

	traces := make([]store.MemoryTrace, 0)
	for rows.Next() {
		t, err := scanMemoryTrace(rows)
		if err != nil {
			return nil, fmt.Errorf("list memory traces: %w", err)
		}
		// RFC3339Nano TEXT ordering puts "…09:00:00Z" after "…09:00:00.5Z"
		// (see ListDue), so the since comparison runs in Go, not SQL.
		if !t.CreatedAt.Before(since) {
			traces = append(traces, *t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list memory traces: %w", err)
	}
	return traces, nil
}

func (s *memoryStore) UpsertNoteState(ctx context.Context, st store.MemoryNoteState) error {
	if st.FirstSeen.IsZero() {
		st.FirstSeen = nowUTC()
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO memory_note_state (`+memoryNoteStateColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (agent_id, note_name) DO UPDATE SET
retention = excluded.retention,
recall_count = excluded.recall_count,
distinct_queries = excluded.distinct_queries,
last_recall = excluded.last_recall,
first_seen = excluded.first_seen`,
		st.AgentID, st.Name, st.Retention, st.RecallCount, st.DistinctQueries,
		nullTime(st.LastRecall), formatTime(st.FirstSeen),
	)
	if err != nil {
		return fmt.Errorf("upsert note state %s: %w", st.Name, err)
	}
	return nil
}

func (s *memoryStore) ListNoteStates(ctx context.Context, agentID string) ([]store.MemoryNoteState, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+memoryNoteStateColumns+` FROM memory_note_state
WHERE agent_id = ?
ORDER BY note_name`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list note states: %w", err)
	}
	defer rows.Close()

	states := make([]store.MemoryNoteState, 0)
	for rows.Next() {
		st, err := scanNoteState(rows)
		if err != nil {
			return nil, fmt.Errorf("list note states: %w", err)
		}
		states = append(states, *st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list note states: %w", err)
	}
	return states, nil
}

func (s *memoryStore) UpsertStaging(ctx context.Context, st store.MemoryStaging) error {
	if st.FirstStaged.IsZero() {
		st.FirstStaged = nowUTC()
	}
	_, err := s.e.ExecContext(ctx, `
INSERT INTO memory_staging (`+memoryStagingColumns+`)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (agent_id, source_ref) DO UPDATE SET
signals = excluded.signals,
first_staged = excluded.first_staged,
promoted = excluded.promoted`,
		st.AgentID, st.SourceRef, st.Signals, formatTime(st.FirstStaged), boolInt(st.Promoted),
	)
	if err != nil {
		return fmt.Errorf("upsert staging %s: %w", st.SourceRef, err)
	}
	return nil
}

func (s *memoryStore) ListStaging(ctx context.Context, agentID string) ([]store.MemoryStaging, error) {
	rows, err := s.e.QueryContext(ctx, `
SELECT `+memoryStagingColumns+` FROM memory_staging
WHERE agent_id = ?
ORDER BY source_ref`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list staging: %w", err)
	}
	defer rows.Close()

	rowsOut := make([]store.MemoryStaging, 0)
	for rows.Next() {
		st, err := scanStaging(rows)
		if err != nil {
			return nil, fmt.Errorf("list staging: %w", err)
		}
		rowsOut = append(rowsOut, *st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list staging: %w", err)
	}
	return rowsOut, nil
}

func (s *memoryStore) ExpireStaging(ctx context.Context, agentID string, before time.Time) (int, error) {
	res, err := s.e.ExecContext(ctx, `
DELETE FROM memory_staging
WHERE agent_id = ? AND first_staged < ?`, agentID, formatTime(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("expire staging: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire staging: %w", err)
	}
	return int(n), nil
}

func (s *memoryStore) GetSweep(ctx context.Context, agentID string) (store.MemorySweep, bool, error) {
	row := s.e.QueryRowContext(ctx, `
SELECT `+memorySweepColumns+` FROM memory_sweeps WHERE agent_id = ?`, agentID)
	sw, err := scanSweep(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.MemorySweep{}, false, nil
		}
		return store.MemorySweep{}, false, fmt.Errorf("get sweep state: %w", err)
	}
	return *sw, true, nil
}

func (s *memoryStore) PutSweep(ctx context.Context, sw store.MemorySweep) error {
	_, err := s.e.ExecContext(ctx, `
INSERT INTO memory_sweeps (`+memorySweepColumns+`)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (agent_id) DO UPDATE SET
watermark = excluded.watermark,
last_start = excluded.last_start,
last_finish = excluded.last_finish,
outcome = excluded.outcome,
counts = excluded.counts`,
		sw.AgentID, nullTime(&sw.Watermark), nullTime(sw.LastStart), nullTime(sw.LastFinish),
		sw.Outcome, sw.Counts,
	)
	if err != nil {
		return fmt.Errorf("put sweep state: %w", err)
	}
	return nil
}

// scanMemoryTrace scans one memory_traces row from a single- or multi-row
// result.
func scanMemoryTrace(row scanner) (*store.MemoryTrace, error) {
	var t store.MemoryTrace
	var hits, createdAt string

	if err := row.Scan(&t.ID, &t.AgentID, &t.SessionID, &t.TurnID, &t.Origin, &t.Query, &hits, &createdAt); err != nil {
		return nil, err
	}
	t.Hits = []store.MemoryTraceHit{}
	if err := jsonUnmarshal(hits, &t.Hits); err != nil {
		return nil, err
	}
	var err error
	if t.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// scanNoteState scans one memory_note_state row from a single- or multi-row
// result.
func scanNoteState(row scanner) (*store.MemoryNoteState, error) {
	var st store.MemoryNoteState
	var lastRecall, firstSeen sql.NullString

	if err := row.Scan(&st.AgentID, &st.Name, &st.Retention, &st.RecallCount, &st.DistinctQueries,
		&lastRecall, &firstSeen); err != nil {
		return nil, err
	}
	if lastRecall.Valid {
		t, err := parseTime(lastRecall.String)
		if err != nil {
			return nil, err
		}
		st.LastRecall = &t
	}
	var err error
	if st.FirstSeen, err = parseTime(firstSeen.String); err != nil {
		return nil, err
	}
	return &st, nil
}

// scanStaging scans one memory_staging row from a single- or multi-row result.
func scanStaging(row scanner) (*store.MemoryStaging, error) {
	var st store.MemoryStaging
	var firstStaged string

	if err := row.Scan(&st.AgentID, &st.SourceRef, &st.Signals, &firstStaged, &st.Promoted); err != nil {
		return nil, err
	}
	var err error
	if st.FirstStaged, err = parseTime(firstStaged); err != nil {
		return nil, err
	}
	return &st, nil
}

// scanSweep scans one memory_sweeps row from a single- or multi-row result.
// A NULL watermark means the sweep has not ingested yet (the zero time); NULL
// start/finish mean that sweep phase has not happened.
func scanSweep(row scanner) (*store.MemorySweep, error) {
	var sw store.MemorySweep
	var watermark, lastStart, lastFinish sql.NullString

	if err := row.Scan(&sw.AgentID, &watermark, &lastStart, &lastFinish, &sw.Outcome, &sw.Counts); err != nil {
		return nil, err
	}
	if watermark.Valid {
		t, err := parseTime(watermark.String)
		if err != nil {
			return nil, err
		}
		sw.Watermark = t
	}
	if lastStart.Valid {
		t, err := parseTime(lastStart.String)
		if err != nil {
			return nil, err
		}
		sw.LastStart = &t
	}
	if lastFinish.Valid {
		t, err := parseTime(lastFinish.String)
		if err != nil {
			return nil, err
		}
		sw.LastFinish = &t
	}
	return &sw, nil
}
