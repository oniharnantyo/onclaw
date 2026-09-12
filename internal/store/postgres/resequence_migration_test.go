//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// TestIntegration_ResequenceSessionEvents covers migration 000045
// (fix-session-event-ordering design D3): the capped-history append bug left
// sessions past 100 events with dozens of rows tied at one seq, so every
// seq-ordered load returned an arbitrary subset. The repair re-sequences each
// session by (occurred_at, event_id) to row_number() - 1, leaving event ids
// and payloads untouched, and is idempotent — a healthy log already carries
// that numbering, so a second pass rewrites nothing.
func TestIntegration_ResequenceSessionEvents(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_reseq_%s", hex.EncodeToString(b))

	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s", baseDSN, separator, schemaName)
	schemaConn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer schemaConn.Close(ctx)

	mig := postgres.NewMigrator(schemaDSN)

	// 1. Migrate to the version just before the repair so the corrupted
	// pre-repair shape can be seeded with raw inserts (explicit seq values —
	// that is exactly how the capped-allocation bug produced the ties).
	if err := mig.MigrateToVersion(44); err != nil {
		t.Fatalf("failed to migrate to version 44: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 44 || dirty {
		t.Fatalf("expected clean version 44 before fixtures, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	var workspaceID string
	if err := schemaConn.QueryRow(ctx,
		`INSERT INTO workspaces (slug, name) VALUES ('reseq-ws', 'Reseq WS') RETURNING id`,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("failed to insert workspace: %v", err)
	}

	// Corruption shape from production (design.md context): 60 rows tied at
	// seq=100 plus 16 rows tied at seq=101, occurred_at strictly increasing.
	const corruptedSession = "sess_reseq_corrupt"
	const tiedFirst, tiedSecond = 60, 16
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	type seededEvent struct {
		eventID    string
		seq        int64
		payload    string
		occurredAt time.Time
	}
	var corrupted []seededEvent
	for i := 0; i < tiedFirst+tiedSecond; i++ {
		seq := int64(100)
		if i >= tiedFirst {
			seq = 101
		}
		corrupted = append(corrupted, seededEvent{
			eventID:    fmt.Sprintf("evt_%03d", i),
			seq:        seq,
			payload:    fmt.Sprintf(`{"n":%d}`, i),
			occurredAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	// A healthy session whose numbering the repair must leave untouched (the
	// seq <> new_seq guard) — exercises the per-session partition.
	const healthySession = "sess_reseq_healthy"
	var healthy []seededEvent
	for i := 0; i < 3; i++ {
		healthy = append(healthy, seededEvent{
			eventID:    fmt.Sprintf("hvt_%03d", i),
			seq:        int64(i),
			payload:    fmt.Sprintf(`{"h":%d}`, i),
			occurredAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	seed := func(sessionID string, events []seededEvent) {
		t.Helper()
		for _, e := range events {
			if _, err := schemaConn.Exec(ctx,
				`INSERT INTO session_events (session_id, event_id, seq, kind, payload, occurred_at, workspace_id)
				 VALUES ($1, $2, $3, 'message', $4, $5, $6)`,
				sessionID, e.eventID, e.seq, e.payload, e.occurredAt, workspaceID,
			); err != nil {
				t.Fatalf("failed to seed event %s: %v", e.eventID, err)
			}
		}
	}
	seed(corruptedSession, corrupted)
	seed(healthySession, healthy)

	// 2. Apply 000045 (and anything stacked above it).
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to migrate up: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v < 45 || dirty {
		t.Fatalf("expected clean version >= 45 after up, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	// loadLog returns the session log in the authoritative (occurred_at,
	// event_id) order.
	loadLog := func(stage, sessionID string) []seededEvent {
		t.Helper()
		rows, err := schemaConn.Query(ctx,
			`SELECT event_id, seq, payload, occurred_at FROM session_events
			 WHERE session_id = $1 ORDER BY occurred_at ASC, event_id ASC`, sessionID,
		)
		if err != nil {
			t.Fatalf("[%s] failed to query session log: %v", stage, err)
		}
		defer rows.Close()
		var got []seededEvent
		for rows.Next() {
			var e seededEvent
			if err := rows.Scan(&e.eventID, &e.seq, &e.payload, &e.occurredAt); err != nil {
				t.Fatalf("[%s] failed to scan event: %v", stage, err)
			}
			got = append(got, e)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("[%s] rows error: %v", stage, err)
		}
		return got
	}

	// 3. The corrupted log is re-sequenced to strictly increasing 0..N-1
	// matching (occurred_at, event_id) order, with ids and payloads unchanged.
	repaired := loadLog("after repair", corruptedSession)
	if len(repaired) != len(corrupted) {
		t.Fatalf("expected %d rows after repair, got %d", len(corrupted), len(repaired))
	}
	for i, e := range repaired {
		want := corrupted[i]
		if e.seq != int64(i) {
			t.Errorf("row %d: expected seq %d, got %d", i, i, e.seq)
		}
		if e.eventID != want.eventID {
			t.Errorf("row %d: event id changed, got %q want %q", i, e.eventID, want.eventID)
		}
		if e.payload != want.payload {
			t.Errorf("row %d (%s): payload changed, got %s want %s", i, e.eventID, e.payload, want.payload)
		}
	}

	// 4. The healthy log is untouched.
	afterHealthy := loadLog("after repair", healthySession)
	if len(afterHealthy) != len(healthy) {
		t.Fatalf("expected %d healthy rows, got %d", len(healthy), len(afterHealthy))
	}
	for i, e := range afterHealthy {
		if e.seq != healthy[i].seq || e.eventID != healthy[i].eventID || e.payload != healthy[i].payload {
			t.Errorf("healthy row %d changed: got {%s %d %s} want {%s %d %s}",
				i, e.eventID, e.seq, e.payload, healthy[i].eventID, healthy[i].seq, healthy[i].payload)
		}
	}

	// 5. Idempotence: the down migration is a documented no-op (pre-repair
	// tied values are unrecoverable), so re-running the repair is down past
	// 000045 then up again — the log must come out byte-identical.
	beforeCorrupted := loadLog("before re-run", corruptedSession)
	beforeHealthy := loadLog("before re-run", healthySession)

	if err := mig.MigrateToVersion(44); err != nil {
		t.Fatalf("failed to migrate down to version 44: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 44 || dirty {
		t.Fatalf("expected clean version 44 after down, got v=%d dirty=%v err=%v", v, dirty, err)
	}
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to re-run migrations above 000044: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v < 45 || dirty {
		t.Fatalf("expected clean version >= 45 after re-run, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	reCorrupted := loadLog("after re-run", corruptedSession)
	if fmt.Sprint(reCorrupted) != fmt.Sprint(beforeCorrupted) {
		t.Errorf("expected re-run to leave the corrupted-then-repaired log identical")
		for i := range beforeCorrupted {
			if i >= len(reCorrupted) || reCorrupted[i] != beforeCorrupted[i] {
				t.Errorf("row %d: got %#v want %#v", i, reCorrupted[i], beforeCorrupted[i])
			}
		}
	}
	reHealthy := loadLog("after re-run", healthySession)
	if fmt.Sprint(reHealthy) != fmt.Sprint(beforeHealthy) {
		t.Errorf("expected re-run to leave the healthy log identical")
	}
}
