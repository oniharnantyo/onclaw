package memory

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// newTestConsolidator wires a consolidator against the fake store with the
// scripted side-call seam the gate/gister tests use.
func newTestConsolidator(s store.Store, m Model, stats StatsFunc, opts ...ConsolidatorOption) *Consolidator {
	base := []ConsolidatorOption{WithSideCall(WithModelResolver(staticResolver(m)))}
	return NewConsolidator(
		s.MemoryNotes(),
		s.MemoryReports(),
		s.Workspaces(),
		s.MemoryEntities(),
		s.Providers(),
		nil,
		nil,
		stats,
		testLogger,
		append(base, opts...)...,
	)
}

// seedSharedNote inserts one current shared note with an explicit id so
// merge decisions can name it.
func seedSharedNote(t *testing.T, s store.Store, id, content, sourceEventID string, importance int) domain.MemoryNote {
	t.Helper()
	note := &domain.MemoryNote{
		ID:            id,
		WorkspaceID:   testWorkspaceID,
		Visibility:    domain.MemoryVisibilityShared,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-time.Hour),
		LearnedAt:     time.Now().UTC().Add(-time.Hour),
		SourceEventID: sourceEventID,
		Content:       content,
		Importance:    importance,
	}
	if err := s.MemoryNotes().InsertNote(context.Background(), note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note %s: %v", id, err)
	}
	return *note
}

// savedReport fetches the persisted last report.
func savedReport(t *testing.T, s store.Store) *domain.MemoryReport {
	t.Helper()
	r, err := s.MemoryReports().Get(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("get report: %v", err)
	}
	return r
}

// TestConsolidatorTriplicatedFactMerges (task 6.1, D12, spec scenario
// "triplicated fact merges"): three near-identical facts collapse into one
// canonical note whose evidence links to all three sources, and the report
// lists the merge.
func TestConsolidatorTriplicatedFactMerges(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-a", "The deploy window is Tuesday", "evt-a", 5)
	seedSharedNote(t, s, "note-b", "The deploy window is Tuesday", "evt-b", 5)
	seedSharedNote(t, s, "note-c", "The deploy window is Tuesday", "evt-c", 5)

	model := &scriptedModel{responses: []string{`{"canonical_id":"note-c"}`}}
	c := newTestConsolidator(s, model, func() IngestStats { return IngestStats{} })

	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if len(report.Merges) != 1 {
		t.Fatalf("expected the merge in the report, got %+v", report.Merges)
	}
	merge := report.Merges[0]
	if merge.CanonicalID != "note-c" {
		t.Fatalf("expected the model's canonical pick, got %q", merge.CanonicalID)
	}
	if len(merge.MergedIDs) != 2 || merge.MergedIDs[0] != "note-a" || merge.MergedIDs[1] != "note-b" {
		t.Fatalf("expected both duplicates folded, got %+v", merge.MergedIDs)
	}

	notes, err := s.MemoryNotes().ListNotesForUI(context.Background(), testWorkspaceID, testUserID, testAgentID, store.MemoryNoteFilters{History: true})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	current := 0
	for _, note := range notes {
		if note.SupersededBy == nil {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("expected exactly one current note, got %d of %d", current, len(notes))
	}

	// The folded duplicates point at the survivor (D6: history answerable).
	for _, id := range []string{"note-a", "note-b"} {
		got, err := s.MemoryNotes().GetNote(context.Background(), testWorkspaceID, testUserID, testAgentID, id)
		if err != nil || got == nil {
			t.Fatalf("superseded note %s must stay queryable, got (%+v, %v)", id, got, err)
		}
		if got.SupersededBy == nil || *got.SupersededBy != "note-c" {
			t.Fatalf("note %s must point at the canonical, got %+v", id, got.SupersededBy)
		}
	}

	// Multi-evidence: the canonical links all three source events (D12).
	evidence, err := s.MemoryNotes().ListNoteEvidence(context.Background(), testWorkspaceID, "note-c")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	ids := map[string]bool{}
	for _, ev := range evidence {
		ids[ev.SourceEventID] = true
		if ev.AddedAt.IsZero() {
			t.Fatal("evidence links must carry their add time")
		}
	}
	if len(evidence) != 3 || !ids["evt-a"] || !ids["evt-b"] || !ids["evt-c"] {
		t.Fatalf("expected three evidence links on the canonical, got %+v", evidence)
	}

	// One cheap-model call per stage: the merge decision for the single
	// cluster plus the topic batch for its (untopic'd) members — the topic
	// answer comes back empty here and fails soft.
	if calls := len(model.callInputs()); calls != 2 {
		t.Fatalf("expected one merge call and one topic batch, got %d", calls)
	}

	if saved := savedReport(t, s); saved == nil {
		t.Fatal("the report must be persisted as the workspace's last one")
	} else {
		var persisted MorningReport
		if err := json.Unmarshal(saved.Report, &persisted); err != nil {
			t.Fatalf("persisted report must round-trip: %v", err)
		}
		if len(persisted.Merges) != 1 || persisted.Merges[0].CanonicalID != "note-c" {
			t.Fatalf("persisted report lost the merge: %+v", persisted)
		}
	}
}

// TestConsolidatorIsIdempotentOnRerun: a second pass finds no current
// duplicates, adds no merge records, and never duplicates evidence links.
func TestConsolidatorIsIdempotentOnRerun(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-a", "The deploy window is Tuesday", "evt-a", 5)
	seedSharedNote(t, s, "note-b", "The deploy window is Tuesday", "evt-b", 5)

	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })
	if _, err := c.RunNow(context.Background(), testWorkspaceID); err != nil {
		t.Fatalf("first RunNow: %v", err)
	}
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("second RunNow: %v", err)
	}
	if len(report.Merges) != 0 {
		t.Fatalf("the rerun must find nothing to merge, got %+v", report.Merges)
	}
	evidence, err := s.MemoryNotes().ListNoteEvidence(context.Background(), testWorkspaceID, "note-a")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidence) != 2 {
		t.Fatalf("evidence links must never duplicate on rerun, got %+v", evidence)
	}
}

// TestConsolidatorFailSoftWithoutModel (D10): an unresolvable model
// degrades to the deterministic heuristic — the merge still happens, the
// report is still produced, and nothing errors.
func TestConsolidatorFailSoftWithoutModel(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-a", "The staging database resets nightly", "evt-a", 3)
	seedSharedNote(t, s, "note-b", "The staging database resets nightly", "evt-b", 9)

	c := NewConsolidator(
		s.MemoryNotes(), s.MemoryReports(), s.Workspaces(), s.MemoryEntities(), s.Providers(), nil, nil,
		func() IngestStats { return IngestStats{} }, testLogger,
		WithSideCall(WithModelResolver(failingResolver("down"))),
	)
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow must not error with the model down: %v", err)
	}
	if len(report.Merges) != 1 || report.Merges[0].CanonicalID != "note-b" {
		t.Fatalf("expected the heuristic canonical (highest importance), got %+v", report.Merges)
	}
}

// TestConsolidatorLeavesDocumentsUntouched (task 6.1, D7): the pipeline
// never edits USER.md/WORKSPACE.md — contents are byte-identical after a
// pass that merges and flags.
func TestConsolidatorLeavesDocumentsUntouched(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()
	workspaceDoc := "# Workspace\nThe headquarters is in Jakarta."
	userDoc := "# User\nPrefers concise answers."
	if err := s.Memories().UpsertWorkspaceMemory(ctx, testWorkspaceID, workspaceDoc); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}
	if err := s.Memories().UpsertUserMemory(ctx, testWorkspaceID, testUserID, userDoc); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}
	seedSharedNote(t, s, "note-a", "The headquarters is in Jakarta", "evt-a", 5)
	seedSharedNote(t, s, "note-b", "The headquarters is in Jakarta", "evt-b", 5)

	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })
	if _, err := c.RunNow(ctx, testWorkspaceID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	wsDoc, err := s.Memories().WorkspaceMemory(ctx, testWorkspaceID)
	if err != nil || wsDoc == nil || wsDoc.Content != workspaceDoc {
		t.Fatalf("WORKSPACE.md changed: %+v, %v", wsDoc, err)
	}
	uDoc, err := s.Memories().UserMemory(ctx, testWorkspaceID, testUserID)
	if err != nil || uDoc == nil || uDoc.Content != userDoc {
		t.Fatalf("USER.md changed: %+v, %v", uDoc, err)
	}
}

// TestConsolidatorHoldsNoDocStore is the structural half of the D7
// guarantee: no field of the consolidator can even reach the document
// store — the doc-write dependency does not exist on the type.
func TestConsolidatorHoldsNoDocStore(t *testing.T) {
	docStore := reflect.TypeOf((*store.MemoryStore)(nil)).Elem()
	consolidatorType := reflect.TypeOf(Consolidator{})
	for i := 0; i < consolidatorType.NumField(); i++ {
		fieldType := consolidatorType.Field(i).Type
		if fieldType.Kind() == reflect.Interface && fieldType.Implements(docStore) {
			t.Fatalf("Consolidator must not hold a document store, field %s does", consolidatorType.Field(i).Name)
		}
	}
}

// TestConsolidatorConflictsFlowIntoReport (task 6.3, D7): the gate's
// doc-conflict flags surface as the report's review section.
func TestConsolidatorConflictsFlowIntoReport(t *testing.T) {
	s := seedWorld(t)
	flag := docConflictFlag
	flagged := &domain.MemoryNote{
		ID:            "note-flagged",
		WorkspaceID:   testWorkspaceID,
		Visibility:    domain.MemoryVisibilityShared,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-time.Hour),
		LearnedAt:     time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC),
		SourceEventID: "evt-flag",
		Content:       "The invoice prefix is INV-X, contradicting the workspace doc",
		Importance:    5,
		ConflictFlag:  &flag,
	}
	if err := s.MemoryNotes().InsertNote(context.Background(), flagged, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed flagged note: %v", err)
	}

	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("expected the flagged note in the conflicts section, got %+v", report.Conflicts)
	}
	conflict := report.Conflicts[0]
	if conflict.NoteID != "note-flagged" || conflict.Document != docConflictFlag {
		t.Fatalf("conflict attribution drifted: %+v", conflict)
	}
	if conflict.Excerpt == "" || conflict.FlaggedAt.IsZero() {
		t.Fatalf("conflict must carry excerpt and flag time: %+v", conflict)
	}
	if !conflict.FlaggedAt.Equal(flagged.LearnedAt) {
		t.Fatalf("flagged_at must be the note's learned time, got %v", conflict.FlaggedAt)
	}
}

// TestConsolidatorExtractionFailureDelta (task 6.3): the report counts the
// extraction failures of the period — the delta since the workspace's last
// pass, not the process lifetime total.
func TestConsolidatorExtractionFailureDelta(t *testing.T) {
	s := seedWorld(t)
	var mu sync.Mutex
	var failed int64
	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats {
		mu.Lock()
		defer mu.Unlock()
		return IngestStats{Failed: failed}
	})

	mu.Lock()
	failed = 2
	mu.Unlock()
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if report.ExtractionFailures != 2 {
		t.Fatalf("expected the delta since zero (2), got %d", report.ExtractionFailures)
	}

	mu.Lock()
	failed = 5
	mu.Unlock()
	report, err = c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if report.ExtractionFailures != 3 {
		t.Fatalf("expected the delta since the last report (3), got %d", report.ExtractionFailures)
	}
}

// TestConsolidatorTopicFolding (task 6.1): untopic'd notes get the model's
// labels; a note the model could not label stays unlabeled; a malformed
// batch answer folds nothing and does not error (fail-soft per batch).
func TestConsolidatorTopicFolding(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-x", "Postgres migrations run Friday", "evt-x", 5)
	seedSharedNote(t, s, "note-y", "Grafana dashboards live under obs", "evt-y", 5)
	model := &scriptedModel{responses: []string{`[{"id":"note-x","topic":"databases"}]`}}
	c := newTestConsolidator(s, model, func() IngestStats { return IngestStats{} })

	if _, err := c.RunNow(context.Background(), testWorkspaceID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	got, err := s.MemoryNotes().GetNote(context.Background(), testWorkspaceID, testUserID, testAgentID, "note-x")
	if err != nil || got == nil || got.Topic == nil || *got.Topic != "databases" {
		t.Fatalf("expected note-x labeled, got (%+v, %v)", got, err)
	}
	got, err = s.MemoryNotes().GetNote(context.Background(), testWorkspaceID, testUserID, testAgentID, "note-y")
	if err != nil || got == nil || got.Topic != nil {
		t.Fatalf("the unlabeled note must stay unlabeled, got (%+v, %v)", got, err)
	}
}

// TestConsolidatorTopicBatchFailSoft: an unparseable topic answer is logged
// and skipped — RunNow still succeeds and labels nothing.
func TestConsolidatorTopicBatchFailSoft(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-x", "Postgres migrations run Friday", "evt-x", 5)
	c := newTestConsolidator(s, &scriptedModel{responses: []string{"not json at all"}}, func() IngestStats { return IngestStats{} })
	report, err := c.RunNow(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("a malformed topic answer must fail soft: %v", err)
	}
	if report.GeneratedAt.IsZero() {
		t.Fatal("the report must still be produced")
	}
	got, _ := s.MemoryNotes().GetNote(context.Background(), testWorkspaceID, testUserID, testAgentID, "note-x")
	if got == nil || got.Topic != nil {
		t.Fatalf("the malformed batch must fold nothing, got %+v", got)
	}
}

// TestConsolidatorRunNowMatchesNightlyPath (task 6.2): the sweep fires the
// same code path as RunNow — a workspace whose local slot has crossed gets
// its pass, its report persisted, without RunNow being called directly.
func TestConsolidatorRunNowMatchesNightlyPath(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-a", "The deploy window is Tuesday", "evt-a", 5)
	seedSharedNote(t, s, "note-b", "The deploy window is Tuesday", "evt-b", 5)

	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })

	// Backdate the workspace's mark so its current local slot is due.
	slot := lastConsolidationSlot(workspaceLoc("UTC"), time.Now())
	c.marksMu.Lock()
	c.marks[testWorkspaceID] = slot.Add(-time.Minute)
	c.marksMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.sweep(ctx)

	r := savedReport(t, s)
	if r == nil {
		t.Fatal("the nightly sweep must persist the morning report")
	}
	var persisted MorningReport
	if err := json.Unmarshal(r.Report, &persisted); err != nil {
		t.Fatalf("persisted report must round-trip: %v", err)
	}
	if len(persisted.Merges) != 1 {
		t.Fatalf("the sweep must perform the same merge RunNow would, got %+v", persisted.Merges)
	}
	// After the pass, the workspace is no longer due in its slot.
	c.marksMu.Lock()
	after := c.marks[testWorkspaceID]
	c.marksMu.Unlock()
	if !after.After(slot) {
		t.Fatalf("the pass must record its mark after the slot, got %v vs %v", after, slot)
	}
}

// TestConsolidatorNightlySlotMath pins the ~02:00-local alignment math:
// the last slot is at or before now, the next slot strictly after, and both
// land exactly on the local consolidation hour.
func TestConsolidatorNightlySlotMath(t *testing.T) {
	now := time.Date(2026, 9, 16, 14, 30, 0, 0, time.UTC)
	for _, tz := range []string{"UTC", "Asia/Jakarta", "America/New_York"} {
		loc := workspaceLoc(tz)
		last := lastConsolidationSlot(loc, now)
		next := nextConsolidationSlot(loc, now)
		if last.After(now) {
			t.Fatalf("%s: last slot must not be in the future: %v", tz, last)
		}
		if !next.After(now) {
			t.Fatalf("%s: next slot must be in the future: %v", tz, next)
		}
		if got := last.In(loc).Format("15:04"); got != "02:00" {
			t.Fatalf("%s: slots must land on the local consolidation hour, last=%s", tz, got)
		}
		if got := next.In(loc).Format("15:04"); got != "02:00" {
			t.Fatalf("%s: slots must land on the local consolidation hour, next=%s", tz, got)
		}
	}

	// An unknown timezone falls back to UTC rather than stalling the pass.
	if workspaceLoc("Mars/Olympus") != time.UTC {
		t.Fatal("unknown timezones must fall back to UTC")
	}
}

// TestConsolidatorLifecycle: Start/Stop mirror the scheduler/heartbeat
// semantics — a disabled consolidator starts nothing, Stop is idempotent,
// and RunNow works regardless.
func TestConsolidatorLifecycle(t *testing.T) {
	s := seedWorld(t)
	seedSharedNote(t, s, "note-a", "The deploy window is Tuesday", "evt-a", 5)
	seedSharedNote(t, s, "note-b", "The deploy window is Tuesday", "evt-b", 5)

	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} }, WithEnabled(false))
	started := context.Background()
	c.Start(started) // disabled: no loop
	c.Stop()
	c.Stop() // idempotent

	// RunNow is the manual path and always works.
	if _, err := c.RunNow(context.Background(), testWorkspaceID); err != nil {
		t.Fatalf("RunNow after stop: %v", err)
	}

	// The enabled consolidator starts, stops cleanly, and stops again.
	enabled := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })
	enabled.Start(started)
	done := make(chan struct{})
	go func() {
		enabled.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop must return promptly when no pass is in flight")
	}
}

// TestConsolidatorMissingWorkspaceErrors: the pass runs only for a real
// workspace row.
func TestConsolidatorMissingWorkspaceErrors(t *testing.T) {
	s := seedWorld(t)
	c := newTestConsolidator(s, &scriptedModel{}, func() IngestStats { return IngestStats{} })
	if _, err := c.RunNow(context.Background(), "no-such-workspace"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown workspace, got %v", err)
	}
}
