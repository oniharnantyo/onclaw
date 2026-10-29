package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// sweepBase is the fixed instant every sweep test runs at; files and traces
// are placed relative to it.
var sweepBase = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

const sweepAgent = "agent-1"

// newTestSweeper returns a sweeper over the fake store with the clock pinned
// to sweepBase.
func newTestSweeper(st store.Store) *Sweeper {
	sw := NewSweeper(st)
	sw.now = func() time.Time { return sweepBase }
	return sw
}

// writeMemoryFile writes one file into the workspace and backdates it, so
// the sweep's mid-write guard (files newer than now−10s are skipped) never
// trips on test fixtures.
func writeMemoryFile(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

// addTrace records one recall trace with an explicit origin, query, time and
// hits (the fake store stamps zero fields, so the time must be explicit for
// the sweep's watermark windows to be deterministic).
func addTrace(t *testing.T, st store.Store, agentID, origin, query string, at time.Time, hits ...store.MemoryTraceHit) {
	t.Helper()
	_, err := st.Memory().AppendTrace(context.Background(), store.MemoryTrace{
		AgentID:   agentID,
		Origin:    origin,
		Query:     query,
		Hits:      hits,
		CreatedAt: at,
	})
	require.NoError(t, err)
}

// userHit is a full-score hit on a workspace-relative memory path.
func userHit(path string) store.MemoryTraceHit {
	return store.MemoryTraceHit{Path: path, Score: 1}
}

// qualifyingTraces records four user-origin recalls of the daily note on
// three distinct queries spread over four days — evidence that clears all
// three promotion gates (score ≈ 0.81 with the line's one wiki-link).
func qualifyingTraces(t *testing.T, st store.Store, agentID, hitPath string) {
	offsets := []time.Duration{-1 * time.Hour, -25 * time.Hour, -49 * time.Hour, -73 * time.Hour}
	queries := []string{"staging port", "database", "where does staging run", "staging port"}
	for i, off := range offsets {
		addTrace(t, st, agentID, store.MemoryOriginUser, queries[i], sweepBase.Add(off), userHit(hitPath))
	}
}

// promoteDailyNote seeds a workspace with one daily note whose first line
// carries a wiki-link, plus one ordinary fact note.
func promoteDailyNote(t *testing.T, workspace string) Layout {
	t.Helper()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))
	writeMemoryFile(t, layout.DailyPath(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)),
		"Staging runs on port 5432 [[staging]]\n", sweepBase.Add(-72*time.Hour))
	writeMemoryFile(t, layout.NotePath("prefers-dark-mode"), noteRaw("prefers-dark-mode", "The user prefers dark mode in every tool"), sweepBase.Add(-48*time.Hour))
	return layout
}

// noteRaw renders a minimal valid fact note.
func noteRaw(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\ntype: user\n---\n\nBody of " + name + ".\n"
}

// snapshotFiles reads every file under root into a path→bytes map, so an
// idempotency test can compare the whole tree byte-for-byte.
func snapshotFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(raw)
		return nil
	})
	require.NoError(t, err)
	return out
}

// TestSweepAgent_PromotesGatedLineOnce is the exact-once scenario: a daily
// line whose cumulative evidence clears all three gates is appended to the
// promoted subsection with its source reference and date; a later sweep with
// one more recall leaves the subsection carrying it exactly once.
func TestSweepAgent_PromotesGatedLineOnce(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := promoteDailyNote(t, workspace)
	qualifyingTraces(t, st, sweepAgent, "memory/2026-10-25.md")

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err, "the sweep must have created MEMORY.md")
	text := string(index)
	require.Contains(t, text, "Promoted from daily notes:")
	require.Contains(t, text, "- Staging runs on port 5432 [[staging]]")
	require.Contains(t, text, "  - Source: memory/2026-10-25.md#L1")
	require.Contains(t, text, "  - Promoted: 2026-10-27")
	require.Equal(t, 1, strings.Count(text, "memory/2026-10-25.md#L1"), "promoted exactly once")

	// The free section above the markers is empty here; the fact note is
	// listed in the managed section.
	require.Contains(t, text, "- [prefers-dark-mode](prefers-dark-mode.md) — The user prefers dark mode in every tool")

	// The staging row is flagged promoted, and the sweep row recorded the
	// run.
	staging, err := st.Memory().ListStaging(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Len(t, staging, 1)
	require.True(t, staging[0].Promoted)

	sweepRow, ok, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, OutcomeOK, sweepRow.Outcome)
	require.Equal(t, sweepBase.Add(-WriteGuardWindow), sweepRow.Watermark)

	// A repeated recall between sweeps: the line stays exactly once.
	later := sweepBase.Add(10 * time.Minute)
	addTrace(t, st, sweepAgent, store.MemoryOriginUser, "staging port again", later.Add(-5*time.Minute), userHit("memory/2026-10-25.md"))
	sw.now = func() time.Time { return later }
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	again, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Equal(t, text, string(again), "an already-promoted line must not promote twice")
}

// TestSweepAgent_IdempotentNoNewInput is the re-run scenario: a second sweep
// with no new notes, daily notes, or traces changes no memory file and no
// derived row (note states, staging, traces), and leaves the sweep row's
// watermark and outcome untouched. The heartbeat fields — last start and
// finish, and this run's counts — are the record OF the sweep, not derived
// state OF the memory: D13 requires a successful sweep to be visible, so
// they advance (with the wall clock) or restate (these counts) on every run
// by design.
func TestSweepAgent_IdempotentNoNewInput(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := promoteDailyNote(t, workspace)
	qualifyingTraces(t, st, sweepAgent, "memory/2026-10-25.md")

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	filesBefore := snapshotFiles(t, layout.Dir)
	statesBefore, err := st.Memory().ListNoteStates(context.Background(), sweepAgent)
	require.NoError(t, err)
	stagingBefore, err := st.Memory().ListStaging(context.Background(), sweepAgent)
	require.NoError(t, err)
	tracesBefore, err := st.Memory().ListTracesSince(context.Background(), sweepAgent, time.Time{})
	require.NoError(t, err)
	sweepBefore, ok, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	require.Equal(t, filesBefore, snapshotFiles(t, layout.Dir), "no memory file may change")

	statesAfter, err := st.Memory().ListNoteStates(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Equal(t, statesBefore, statesAfter, "no note-state row may change")

	stagingAfter, err := st.Memory().ListStaging(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Equal(t, stagingBefore, stagingAfter, "no staging row may change")

	tracesAfter, err := st.Memory().ListTracesSince(context.Background(), sweepAgent, time.Time{})
	require.NoError(t, err)
	require.Len(t, tracesAfter, len(tracesBefore), "the sweep never writes traces")

	sweepAfter, ok, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, sweepBefore.Watermark, sweepAfter.Watermark,
		"no new input leaves the watermark untouched")
	require.Equal(t, sweepBefore.Outcome, sweepAfter.Outcome)
	// The heartbeat's own fields restate for this run: this sweep saw no
	// candidates and did nothing, and its counts say so.
	require.Equal(t, `{"candidates":0,"promotions":0,"archivals":0,"expired":0}`, sweepAfter.Counts)
}

// TestSweep_FailureIsolation covers the isolation scenario: one agent's
// sweep failing is recorded with its reason and does not stop another
// agent's sweep.
func TestSweep_FailureIsolation(t *testing.T) {
	st := fake.New()

	// Agent A's memory path is occupied by a file: reading the memory
	// directory fails, failing the sweep partway.
	wsBroken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(wsBroken, DirName), []byte("not a directory"), 0600))

	// Agent B is a normal workspace with a note.
	wsHealthy := t.TempDir()
	healthyLayout := NewLayout(wsHealthy)
	writeMemoryFile(t, healthyLayout.NotePath("prefers-dark-mode"), noteRaw("prefers-dark-mode", "The user prefers dark mode"), sweepBase.Add(-time.Hour))

	sw := newTestSweeper(st)
	results := sw.Sweep(context.Background(), []SweepTarget{
		{AgentID: "agent-broken", WorkspaceDir: wsBroken},
		{AgentID: "agent-healthy", WorkspaceDir: wsHealthy},
	})

	require.Len(t, results, 2)
	require.Error(t, results[0].Err, "the broken agent's sweep must fail")
	require.NoError(t, results[1].Err, "the healthy agent's sweep must proceed")

	brokenRow, ok, err := st.Memory().GetSweep(context.Background(), "agent-broken")
	require.NoError(t, err)
	require.True(t, ok, "the failure must be recorded")
	require.True(t, strings.HasPrefix(brokenRow.Outcome, OutcomeError+": "))
	require.Contains(t, brokenRow.Outcome, "memory: read", "the reason is recorded with the failure")
	require.Equal(t, time.Time{}, brokenRow.Watermark, "a failed sweep leaves the watermark untouched")

	healthyIndex, err := os.ReadFile(healthyLayout.IndexPath())
	require.NoError(t, err)
	require.Contains(t, string(healthyIndex), "prefers-dark-mode")
	healthyRow, ok, err := st.Memory().GetSweep(context.Background(), "agent-healthy")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, OutcomeOK, healthyRow.Outcome)
}

// modelCallSpy stands in for a model provider: if any part of the sweep ever
// reached a model, this counter would move. The sweeper cannot even be
// handed one — NewSweeper takes only a store — so the compile-time shape and
// the zero counter together prove the hard constraint.
type modelCallSpy struct {
	calls atomic.Int64
}

// Generate pretends to be the one method a provider has.
func (m *modelCallSpy) Generate(_ context.Context, _ string) (string, error) {
	m.calls.Add(1)
	return "", nil
}

// TestSweep_MakesZeroModelCalls is the constraint test: a full sweep over
// notes, daily notes, and traces of every origin receives zero model calls.
func TestSweep_MakesZeroModelCalls(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	promoteDailyNote(t, workspace)
	qualifyingTraces(t, st, sweepAgent, "memory/2026-10-25.md")
	addTrace(t, st, sweepAgent, store.MemoryOriginScheduler, "scheduled", sweepBase.Add(-2*time.Hour), userHit("memory/2026-10-25.md"))
	addTrace(t, st, sweepAgent, store.MemoryOriginSubagent, "child", sweepBase.Add(-2*time.Hour), userHit("memory/2026-10-25.md"))

	// The constructor takes no provider argument — this line is the
	// compile-time half of the proof.
	sw := newTestSweeper(st)
	spy := &modelCallSpy{}

	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))
	require.Equal(t, int64(0), spy.calls.Load(), "no model call may happen anywhere in the sweep")
}

// TestSweepAgent_MidWriteGuardSkipsFreshFiles covers the mid-write guard: a
// file modified within the guard window is skipped this sweep, its index
// entry deferred, and the watermark advanced so the next sweep re-reads it.
func TestSweepAgent_MidWriteGuardSkipsFreshFiles(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))
	// Fresh mtime = the sweep's own now: inside the guard window.
	writeMemoryFile(t, layout.NotePath("brand-new"), noteRaw("brand-new", "Written seconds ago"), sweepBase)

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.NotContains(t, string(index), "brand-new", "a mid-write file must be skipped")

	row, _, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Equal(t, sweepBase.Add(-WriteGuardWindow), row.Watermark, "the guard advances the watermark so the file is re-read")

	// The write settles: the next sweep ingests it.
	writeMemoryFile(t, layout.NotePath("brand-new"), noteRaw("brand-new", "Written seconds ago"), sweepBase.Add(-time.Hour))
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err = os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Contains(t, string(index), "brand-new", "the settled file must be ingested on the next sweep")
}

// TestSweepAgent_RetentionAndArchive covers the retention scenarios at the
// sweep level: a recalled note outranks a control, a fresh note below the
// floor survives inside its grace, a stale below-floor note archives, the
// archived content is intact (never deleted), and a restored note returns to
// the index on the next sweep.
func TestSweepAgent_RetentionAndArchive(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))

	// Two first-sight notes with disjoint content: recalled gets a
	// user-origin recall, control gets nothing.
	writeMemoryFile(t, layout.NotePath("recalled"), noteRaw("recalled", "The deploy pipeline lives in CircleCI"), sweepBase.Add(-48*time.Hour))
	writeMemoryFile(t, layout.NotePath("control"), noteRaw("control", "Lunch order rotates on Fridays"), sweepBase.Add(-48*time.Hour))
	addTrace(t, st, sweepAgent, store.MemoryOriginUser, "ci", sweepBase.Add(-time.Hour), userHit("memory/recalled.md"))

	// A stale note below the floor, outside its grace: archives this sweep.
	writeMemoryFile(t, layout.NotePath("stale"), noteRaw("stale", "An ancient fact nobody recalls"), sweepBase.Add(-40*24*time.Hour))
	require.NoError(t, st.Memory().UpsertNoteState(context.Background(), store.MemoryNoteState{
		AgentID:   sweepAgent,
		Name:      "stale",
		Retention: 0.05,
	}))

	// A fresh note below the floor but inside its grace: protected.
	writeMemoryFile(t, layout.NotePath("fresh"), noteRaw("fresh", "A young fact nobody recalls"), sweepBase.Add(-10*24*time.Hour))
	require.NoError(t, st.Memory().UpsertNoteState(context.Background(), store.MemoryNoteState{
		AgentID:   sweepAgent,
		Name:      "fresh",
		Retention: 0.0,
	}))

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	states, err := st.Memory().ListNoteStates(context.Background(), sweepAgent)
	require.NoError(t, err)
	byName := make(map[string]store.MemoryNoteState, len(states))
	for _, s := range states {
		byName[s.Name] = s
	}
	require.Greater(t, byName["recalled"].Retention, byName["control"].Retention,
		"a user-origin recall must reinforce retention versus a control")
	require.Equal(t, 1, byName["recalled"].RecallCount)

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Contains(t, string(index), "recalled")
	require.Contains(t, string(index), "control")
	require.Contains(t, string(index), "fresh", "a fresh note is never archived")
	require.NotContains(t, string(index), "stale", "an archived note leaves the index")

	_, statErr := os.Stat(layout.NotePath("stale"))
	require.True(t, os.IsNotExist(statErr))
	archived, readErr := os.ReadFile(filepath.Join(layout.ArchiveDir(), "stale.md"))
	require.NoError(t, readErr, "archiving is a move, never a delete")
	require.Contains(t, string(archived), "An ancient fact nobody recalls")

	// Restore: the note moved back is listed again on the next sweep.
	require.NoError(t, RestoreNote(layout, "stale"))
	writeMemoryFile(t, layout.NotePath("stale"), noteRaw("stale", "An ancient fact nobody recalls"), sweepBase.Add(-20*24*time.Hour))
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err = os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Contains(t, string(index), "stale", "a restored note is listed again")
	_, statErr = os.Stat(filepath.Join(layout.ArchiveDir(), "stale.md"))
	require.True(t, os.IsNotExist(statErr), "restoring moves the file back out of the archive")
}

// TestSweepAgent_SupersededNoteLeavesIndex covers the supersede scenario: on
// the sweep that observes the superseding note, the superseded note leaves
// the managed section and — once past its grace — the workspace too.
func TestSweepAgent_SupersededNoteLeavesIndex(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))

	writeMemoryFile(t, layout.NotePath("old-api"), noteRaw("old-api", "The old API base URL"), sweepBase.Add(-40*24*time.Hour))
	writeMemoryFile(t, layout.NotePath("new-api"), "---\nname: new-api\ndescription: The new API base URL\ntype: project\nsupersedes:\n  - old-api\n---\n\nhttps://api.example.com\n", sweepBase.Add(-2*time.Hour))

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Contains(t, string(index), "new-api")
	require.NotContains(t, string(index), "old-api", "a superseded note leaves the managed section")

	_, statErr := os.Stat(filepath.Join(layout.ArchiveDir(), "old-api.md"))
	require.NoError(t, statErr, "past its grace, the superseded note archives on the observing sweep")
}

// TestSweepAgent_StagingExpires covers the 30-day expiry: a staged candidate
// that never passed the gates is removed from staging and is no longer a
// candidate.
func TestSweepAgent_StagingExpires(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))
	writeMemoryFile(t, layout.NotePath("some-note"), noteRaw("some-note", "A fact"), sweepBase.Add(-48*time.Hour))

	require.NoError(t, st.Memory().UpsertStaging(context.Background(), store.MemoryStaging{
		AgentID:     sweepAgent,
		SourceRef:   "memory/2026-10-25.md#L1",
		FirstStaged: sweepBase.Add(-31 * 24 * time.Hour),
	}))

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	staging, err := st.Memory().ListStaging(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Empty(t, staging, "a candidate 31 days past first staging must expire")

	row, _, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Contains(t, row.Counts, `"expired":1`)
}

// TestSweepAgent_FreeSectionPreservedByteForByte covers the index contract:
// a handwritten MEMORY.md without markers keeps its content byte-for-byte as
// the free section, markers are appended, and a second sweep with no changes
// is byte-identical.
func TestSweepAgent_FreeSectionPreservedByteForByte(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))

	free := "# My memory\n\nHandwritten notes with trailing spaces   \n\nand a blank line.\n"
	writeMemoryFile(t, layout.IndexPath(), free, sweepBase.Add(-24*time.Hour))
	writeMemoryFile(t, layout.NotePath("prefers-dark-mode"), noteRaw("prefers-dark-mode", "The user prefers dark mode"), sweepBase.Add(-24*time.Hour))

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(index), free), "the free section must be preserved byte-for-byte")
	require.Contains(t, string(index), managedBegin)
	require.Contains(t, string(index), "prefers-dark-mode")

	before := snapshotFiles(t, layout.Dir)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))
	require.Equal(t, before, snapshotFiles(t, layout.Dir), "a second sweep with no changes is byte-identical")
}

// TestSweepAgent_MissingMemoryDirIsInert covers the migration note: a sweep
// for an agent without a memory directory records a heartbeat and creates
// nothing.
func TestSweepAgent_MissingMemoryDirIsInert(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	_, err := os.Stat(NewLayout(workspace).Dir)
	require.True(t, os.IsNotExist(err), "the sweep must not create the memory directory")

	row, ok, err := st.Memory().GetSweep(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.True(t, ok, "the heartbeat records the run")
	require.Equal(t, OutcomeOK, row.Outcome)
}

// TestSweepAgent_MaxTenPromotions covers the promotion cap: twelve lines
// sharing qualifying evidence yield at most ten promotions, the rest stay
// staged unpromoted.
func TestSweepAgent_MaxTenPromotions(t *testing.T) {
	st := fake.New()
	workspace := t.TempDir()
	layout := NewLayout(workspace)
	require.NoError(t, os.MkdirAll(layout.Dir, 0700))

	var body strings.Builder
	for i := 1; i <= 12; i++ {
		body.WriteString("Distinct fact number ")
		body.WriteString(strings.Repeat("x", i))
		body.WriteString(" recorded today\n")
	}
	writeMemoryFile(t, layout.DailyPath(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)), body.String(), sweepBase.Add(-72*time.Hour))

	// Ten recalls over three distinct queries and days: frequency saturates
	// and every line's cumulative score clears the gates.
	offsets := []time.Duration{-1 * time.Hour, -25 * time.Hour, -49 * time.Hour}
	queries := []string{"q1", "q2", "q3"}
	for round := 0; round < 3; round++ {
		for qi, q := range queries {
			at := sweepBase.Add(offsets[qi]).Add(time.Duration(round) * time.Hour)
			addTrace(t, st, sweepAgent, store.MemoryOriginUser, q, at, userHit("memory/2026-10-25.md"))
		}
	}

	sw := newTestSweeper(st)
	require.NoError(t, sw.SweepAgent(context.Background(), SweepTarget{AgentID: sweepAgent, WorkspaceDir: workspace}))

	index, err := os.ReadFile(layout.IndexPath())
	require.NoError(t, err)
	require.Equal(t, MaxPromotionsPerSweep, strings.Count(string(index), "  - Source: memory/2026-10-25.md#L"),
		"at most ten lines promote per sweep")

	staging, err := st.Memory().ListStaging(context.Background(), sweepAgent)
	require.NoError(t, err)
	require.Len(t, staging, 12)
	promoted := 0
	for _, row := range staging {
		if row.Promoted {
			promoted++
		}
	}
	require.Equal(t, MaxPromotionsPerSweep, promoted, "the unchosen lines remain staged unpromoted")
}
