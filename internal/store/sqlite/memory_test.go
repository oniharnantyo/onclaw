package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// seedMemoryAgent creates one agent to own memory rows (the memory foreign
// keys target agents.id).
func seedMemoryAgent(t *testing.T, ctx context.Context, s *Store, name, slug string) *store.Agent {
	t.Helper()
	agent := &store.Agent{Name: name, Slug: slug}
	require.NoError(t, s.Agents().Create(ctx, agent))
	return agent
}

// TestMemory_TracesRoundTrip covers the recall-trace contract: append assigns
// the id and stamps the creation time (or preserves provided ones), the hits
// round-trip as ranked path/score pairs, and list-since filters by creation
// time oldest first.
func TestMemory_TracesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mem := s.Memory()

	agent := seedMemoryAgent(t, ctx, s, "scout", "scout")

	older := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	first, err := mem.AppendTrace(ctx, store.MemoryTrace{
		AgentID: agent.ID, SessionID: "sess-1", TurnID: "turn-1",
		Origin: store.MemoryOriginUser, Query: "deployment preference",
		Hits: []store.MemoryTraceHit{
			{Path: "memory/deploy.md", Score: 0.92},
			{Path: "memory/MEMORY.md", Score: 0.41},
		},
		CreatedAt: older,
	})
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{32}$`, first.ID)
	require.Equal(t, older, first.CreatedAt, "a provided creation time is preserved")

	second, err := mem.AppendTrace(ctx, store.MemoryTrace{
		AgentID: agent.ID, Origin: store.MemoryOriginScheduler, Query: "cron",
	})
	require.NoError(t, err)
	require.False(t, second.CreatedAt.IsZero(), "a zero creation time is stamped")
	require.Empty(t, second.Hits, "a trace without hits carries an empty hit list")

	traces, err := mem.ListTracesSince(ctx, agent.ID, time.Time{})
	require.NoError(t, err)
	require.Len(t, traces, 2)
	require.Equal(t, first.ID, traces[0].ID, "traces list oldest first")
	require.Equal(t, first, traces[0], "the stored trace round-trips verbatim")

	traces, err = mem.ListTracesSince(ctx, agent.ID, older.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, traces, 1, "list-since cuts older traces")
	require.Equal(t, second.ID, traces[0].ID)

	traces, err = mem.ListTracesSince(ctx, "ffffffffffffffffffffffffffffffff", time.Time{})
	require.NoError(t, err)
	require.Empty(t, traces, "another agent's traces are never listed")

	_, err = mem.AppendTrace(ctx, store.MemoryTrace{
		AgentID: "ffffffffffffffffffffffffffffffff", Origin: store.MemoryOriginUser, Query: "q",
	})
	require.Error(t, err, "an unknown agent id must violate the foreign key")
}

// TestMemory_NoteStateRoundTrip covers the retention-state contract: upsert
// inserts and overwrites keyed by (agent, note name), a zero first-seen is
// stamped, and the list is per-agent and name-ordered. Note the rows carry
// scores and counts only — no note text anywhere.
func TestMemory_NoteStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mem := s.Memory()

	scout := seedMemoryAgent(t, ctx, s, "scout", "scout")
	mapper := seedMemoryAgent(t, ctx, s, "mapper", "mapper")

	lastRecall := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	require.NoError(t, mem.UpsertNoteState(ctx, store.MemoryNoteState{
		AgentID: scout.ID, Name: "deploy", Retention: 0.8,
		RecallCount: 3, DistinctQueries: 2, LastRecall: &lastRecall,
	}))
	require.NoError(t, mem.UpsertNoteState(ctx, store.MemoryNoteState{
		AgentID: scout.ID, Name: "api-keys", Retention: 0.4,
	}))
	require.NoError(t, mem.UpsertNoteState(ctx, store.MemoryNoteState{
		AgentID: mapper.ID, Name: "deploy", Retention: 0.1,
	}))

	states, err := mem.ListNoteStates(ctx, scout.ID)
	require.NoError(t, err)
	require.Len(t, states, 2, "another agent's rows are never listed")
	require.Equal(t, "api-keys", states[0].Name, "note states list name-ordered")

	deploy := states[1]
	require.Equal(t, "deploy", deploy.Name)
	require.InDelta(t, 0.8, deploy.Retention, 1e-9)
	require.Equal(t, 3, deploy.RecallCount)
	require.Equal(t, 2, deploy.DistinctQueries)
	require.NotNil(t, deploy.LastRecall)
	require.Equal(t, lastRecall, *deploy.LastRecall)
	require.False(t, deploy.FirstSeen.IsZero(), "a zero first-seen is stamped")

	apiKeys := states[0]
	require.Nil(t, apiKeys.LastRecall, "a never-recalled note carries no last-recall time")

	// The sweep's read-modify-write: retention moves, first-seen is carried
	// over, and the never-recalled note gains a last recall.
	updated := deploy
	updated.Retention = 0.9
	updated.RecallCount = 5
	require.NoError(t, mem.UpsertNoteState(ctx, updated))

	states, err = mem.ListNoteStates(ctx, scout.ID)
	require.NoError(t, err)
	require.Len(t, states, 2, "an upsert must not duplicate the row")
	got := states[1]
	require.InDelta(t, 0.9, got.Retention, 1e-9)
	require.Equal(t, 5, got.RecallCount)
	require.Equal(t, deploy.FirstSeen, got.FirstSeen, "the carried-over first-seen survives")
}

// TestMemory_StagingRoundTrip covers the promotion-staging contract: upsert
// keyed by (agent, source ref), the promoted flag flips on a later upsert,
// and expiry removes only the rows staged before the cutoff, returning the
// count.
func TestMemory_StagingRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mem := s.Memory()

	agent := seedMemoryAgent(t, ctx, s, "scout", "scout")

	old := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	require.NoError(t, mem.UpsertStaging(ctx, store.MemoryStaging{
		AgentID: agent.ID, SourceRef: "memory/2026-09-01.md#L4",
		Signals: `{"relevance":0.9}`, FirstStaged: old,
	}))
	require.NoError(t, mem.UpsertStaging(ctx, store.MemoryStaging{
		AgentID: agent.ID, SourceRef: "memory/2026-10-20.md#L11",
		Signals: `{"relevance":0.85}`, FirstStaged: recent,
	}))

	staged, err := mem.ListStaging(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, staged, 2)
	require.Equal(t, "memory/2026-09-01.md#L4", staged[0].SourceRef, "staging lists source-ordered")
	require.False(t, staged[0].Promoted)
	require.Equal(t, old, staged[0].FirstStaged)

	// Promotion flips the flag through a read-modify-write upsert; the
	// source ref is never staged twice.
	promoted := staged[0]
	promoted.Promoted = true
	require.NoError(t, mem.UpsertStaging(ctx, promoted))

	staged, err = mem.ListStaging(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, staged, 2)
	require.True(t, staged[0].Promoted)

	// The 30-day expiry (design D8): only the old row goes, and the count
	// reports exactly what was removed.
	expired, err := mem.ExpireStaging(ctx, agent.ID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, 1, expired)

	staged, err = mem.ListStaging(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, staged, 1)
	require.Equal(t, "memory/2026-10-20.md#L11", staged[0].SourceRef)

	expired, err = mem.ExpireStaging(ctx, agent.ID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, 0, expired, "expiry is idempotent")
}

// TestMemory_SweepRoundTrip covers the sweep-state contract: absence is a
// normal state, put creates the agent's row, and a second put replaces it
// whole (the heartbeat is a rewrite, design D13). A zero watermark round-trips
// as unset.
func TestMemory_SweepRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mem := s.Memory()

	agent := seedMemoryAgent(t, ctx, s, "scout", "scout")

	_, found, err := mem.GetSweep(ctx, agent.ID)
	require.NoError(t, err)
	require.False(t, found, "an agent never swept has no sweep row")

	start := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	finish := start.Add(2 * time.Second)
	watermark := finish
	require.NoError(t, mem.PutSweep(ctx, store.MemorySweep{
		AgentID: agent.ID, Watermark: watermark,
		LastStart: &start, LastFinish: &finish,
		Outcome: "ok", Counts: `{"promoted":2,"archived":1}`,
	}))

	got, found, err := mem.GetSweep(ctx, agent.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, watermark, got.Watermark)
	require.NotNil(t, got.LastStart)
	require.Equal(t, start, *got.LastStart)
	require.NotNil(t, got.LastFinish)
	require.Equal(t, finish, *got.LastFinish)
	require.Equal(t, "ok", got.Outcome)
	require.Equal(t, `{"promoted":2,"archived":1}`, got.Counts)

	// The next heartbeat replaces the whole row, including clearing the
	// nullable times back to unset.
	require.NoError(t, mem.PutSweep(ctx, store.MemorySweep{
		AgentID: agent.ID, Outcome: "failed",
	}))

	got, found, err = mem.GetSweep(ctx, agent.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, got.Watermark.IsZero(), "a zero watermark is stored as unset")
	require.Nil(t, got.LastStart)
	require.Nil(t, got.LastFinish)
	require.Equal(t, "failed", got.Outcome)

	_, found, err = mem.GetSweep(ctx, "ffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	require.False(t, found)
}

// TestMemory_CascadeOnAgentDelete covers migration 000016's foreign keys:
// deleting the agents row removes all four kinds of the agent's memory rows
// while other agents' rows survive untouched.
func TestMemory_CascadeOnAgentDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mem := s.Memory()

	doomed := seedMemoryAgent(t, ctx, s, "doomed", "doomed")
	survivor := seedMemoryAgent(t, ctx, s, "survivor", "survivor")

	seed := func(agent *store.Agent) {
		t.Helper()
		_, err := mem.AppendTrace(ctx, store.MemoryTrace{
			AgentID: agent.ID, Origin: store.MemoryOriginUser, Query: "q-" + agent.ID,
		})
		require.NoError(t, err)
		require.NoError(t, mem.UpsertNoteState(ctx, store.MemoryNoteState{
			AgentID: agent.ID, Name: "note", Retention: 0.5,
		}))
		require.NoError(t, mem.UpsertStaging(ctx, store.MemoryStaging{
			AgentID: agent.ID, SourceRef: "memory/2026-10-27.md#L1",
		}))
		require.NoError(t, mem.PutSweep(ctx, store.MemorySweep{AgentID: agent.ID, Outcome: "ok"}))
	}
	seed(doomed)
	seed(survivor)

	require.NoError(t, s.Agents().Delete(ctx, doomed.ID))

	traces, err := mem.ListTracesSince(ctx, doomed.ID, time.Time{})
	require.NoError(t, err)
	require.Empty(t, traces, "deleting the agent must cascade its traces away")
	states, err := mem.ListNoteStates(ctx, doomed.ID)
	require.NoError(t, err)
	require.Empty(t, states, "deleting the agent must cascade its note states away")
	staged, err := mem.ListStaging(ctx, doomed.ID)
	require.NoError(t, err)
	require.Empty(t, staged, "deleting the agent must cascade its staging away")
	_, found, err := mem.GetSweep(ctx, doomed.ID)
	require.NoError(t, err)
	require.False(t, found, "deleting the agent must cascade its sweep state away")

	traces, err = mem.ListTracesSince(ctx, survivor.ID, time.Time{})
	require.NoError(t, err)
	require.Len(t, traces, 1, "another agent's traces must survive the cascade")
	states, err = mem.ListNoteStates(ctx, survivor.ID)
	require.NoError(t, err)
	require.Len(t, states, 1, "another agent's note states must survive the cascade")
	staged, err = mem.ListStaging(ctx, survivor.ID)
	require.NoError(t, err)
	require.Len(t, staged, 1, "another agent's staging must survive the cascade")
	_, found, err = mem.GetSweep(ctx, survivor.ID)
	require.NoError(t, err)
	require.True(t, found, "another agent's sweep state must survive the cascade")
}

// TestMemory_NoNoteContentColumn pins design D1's hard line: the four memory
// tables hold derived state only — their column sets contain no note-content
// column, because the workspace files are the source of truth.
func TestMemory_NoNoteContentColumn(t *testing.T) {
	s := newTestStore(t)

	allowed := map[string][]string{
		"memory_traces": {"id", "agent_id", "session_id", "turn_id", "origin", "query", "hits", "created_at"},
		"memory_note_state": {
			"agent_id", "note_name", "retention", "recall_count",
			"distinct_queries", "last_recall", "first_seen",
		},
		"memory_staging": {"agent_id", "source_ref", "signals", "first_staged", "promoted"},
		"memory_sweeps":  {"agent_id", "watermark", "last_start", "last_finish", "outcome", "counts"},
	}

	for table, expected := range allowed {
		got := tableColumns(t, s.db, table)
		require.ElementsMatch(t, expected, got,
			"table %s must hold exactly its derived-state columns (no note content)", table)
	}
}

// tableColumns reads one table's column names via PRAGMA table_info. The
// table name is a repo-internal constant, so interpolation is safe.
func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	require.NoError(t, err)
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk))
		cols = append(cols, name)
	}
	require.NoError(t, rows.Err())
	return cols
}

// TestConcurrent_SweepTxAndTraceInserts covers the data-store spec's
// concurrency requirement: a sweep's batched retention write (one WithTx
// transaction) committing while concurrent memory_search trace inserts race
// it — both sides complete under WAL with the busy timeout, and no write is
// lost. Run under -race.
func TestConcurrent_SweepTxAndTraceInserts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	agent := seedMemoryAgent(t, ctx, s, "swept", "swept")

	const (
		sweepRounds     = 5
		notesPerSweep   = 10
		traceWriters    = 4
		tracesPerWriter = 20
	)

	var wg sync.WaitGroup
	errs := make(chan error, 1+traceWriters)

	// The sweep side: each round rewrites every note's retention state plus
	// the heartbeat inside one transaction.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 0; round < sweepRounds; round++ {
			err := s.WithTx(ctx, func(tx store.Store) error {
				for i := 0; i < notesPerSweep; i++ {
					st := store.MemoryNoteState{
						AgentID: agent.ID, Name: fmt.Sprintf("note-%02d", i),
						Retention:       float64(round) / 10,
						RecallCount:     round,
						DistinctQueries: i % 3,
					}
					if err := tx.Memory().UpsertNoteState(ctx, st); err != nil {
						return err
					}
				}
				return tx.Memory().PutSweep(ctx, store.MemorySweep{AgentID: agent.ID, Outcome: "ok"})
			})
			if err != nil {
				errs <- fmt.Errorf("sweep round %d: %w", round, err)
				return
			}
		}
	}()

	// The turn side: memory_search records one trace row per call.
	for w := 0; w < traceWriters; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < tracesPerWriter; i++ {
				_, err := s.Memory().AppendTrace(ctx, store.MemoryTrace{
					AgentID: agent.ID, Origin: store.MemoryOriginUser,
					Query: fmt.Sprintf("q-%d-%d", w, i),
				})
				if err != nil {
					errs <- fmt.Errorf("trace writer %d: %w", w, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent write failed: %v", err)
	}

	traces, err := s.Memory().ListTracesSince(ctx, agent.ID, time.Time{})
	require.NoError(t, err)
	require.Len(t, traces, traceWriters*tracesPerWriter, "no trace may be lost")

	states, err := s.Memory().ListNoteStates(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, states, notesPerSweep, "every retention row must land exactly once")

	_, found, err := s.Memory().GetSweep(ctx, agent.ID)
	require.NoError(t, err)
	require.True(t, found, "the sweep heartbeat must be recorded")
}

// TestOpen_MigratesFromPreviousVersion covers task 4.1's upgrade path: a
// database at the previous schema version (v15) migrates to v16 on open,
// creating the four memory tables without touching existing rows, and the
// memory store is usable immediately afterwards.
func TestOpen_MigratesFromPreviousVersion(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/v15.db"

	// Build a database at schema v15: every embedded migration but the
	// newest one.
	var upToV15 []migration
	for _, m := range mustEmbeddedMigrations(t) {
		if m.Version <= 15 {
			upToV15 = append(upToV15, m)
		}
	}
	require.Len(t, upToV15, 15)

	st15, err := openWith(ctx, path, upToV15)
	require.NoError(t, err)
	agent := &store.Agent{Name: "scout", Slug: "scout"}
	require.NoError(t, st15.Agents().Create(ctx, agent))
	require.NoError(t, st15.Close())

	db := openRaw(t, path)
	for _, table := range []string{"memory_traces", "memory_note_state", "memory_staging", "memory_sweeps"} {
		require.False(t, tableExists(t, db, table), "a v15 database must not hold %s yet", table)
	}

	// Opening with the full embedded list applies 000016 and keeps the data.
	st, err := Open(ctx, path)
	require.NoError(t, err, "a v15 database must migrate on open")
	t.Cleanup(func() { _ = st.Close() })

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 17, version, "the latest migration must be recorded as applied")

	for _, table := range []string{"memory_traces", "memory_note_state", "memory_staging", "memory_sweeps"} {
		require.True(t, tableExists(t, db, table), "table %s must exist after the upgrade", table)
	}

	got, err := st.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err, "existing rows must survive the v16 migration")
	require.Equal(t, "scout", got.Name)

	trace, err := st.Memory().AppendTrace(ctx, store.MemoryTrace{
		AgentID: agent.ID, Origin: store.MemoryOriginUser, Query: "post-upgrade",
	})
	require.NoError(t, err, "the memory store must be usable right after the upgrade")
	traces, err := st.Memory().ListTracesSince(ctx, agent.ID, time.Time{})
	require.NoError(t, err)
	require.Len(t, traces, 1)
	require.Equal(t, trace.ID, traces[0].ID)
}
