package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// Store-level coverage for the consolidator's ports (integrate-agent-zero-
// memory D12): the merge primitive (SupersedeInto), the multi-evidence
// links (AddNoteEvidence / ListNoteEvidence), the topic setter, and the
// per-workspace last morning report (MemoryReportStore).

// seedEvidenceWorld creates one unique workspace world for these tests and
// returns the workspace id.
func seedEvidenceWorld(t *testing.T, s store.Store, suffix string) string {
	t.Helper()
	wsID, _, _ := seedMemoryFixtures(t, s, "mem-reports-"+suffix, "mem-reports-"+suffix+"@example.com", "atlas-"+suffix)
	return wsID
}

func insertMemoryNote(t *testing.T, ctx context.Context, s store.Store, wsID, id, content, sourceEventID string) *domain.MemoryNote {
	t.Helper()
	note := newMemoryNote(wsID, domain.MemoryVisibilityShared, "", content)
	note.ID = id
	note.SourceEventID = sourceEventID
	if err := s.MemoryNotes().InsertNote(ctx, note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("insert note %s: %v", id, err)
	}
	return note
}

func TestMemoryReportStore_SaveGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID := seedEvidenceWorld(t, s, "r")

	// Absence is a normal state (the MemoryStore convention).
	got, err := s.MemoryReports().Get(ctx, wsID)
	if got != nil || err != nil {
		t.Fatalf("expected (nil, nil) for an absent report, got (%+v, %v)", got, err)
	}

	first := []byte(`{"generated_at":"2026-09-16T02:00:00Z","conflicts":[],"merges":[],"extraction_failures":2}`)
	firstAt := time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC)
	if err := s.MemoryReports().Save(ctx, wsID, first, firstAt); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = s.MemoryReports().Get(ctx, wsID)
	if err != nil || got == nil {
		t.Fatalf("get after save: (%+v, %v)", got, err)
	}
	if string(got.Report) != string(first) {
		t.Fatalf("report bytes drifted:\n got %s\nwant %s", got.Report, first)
	}
	if !got.GeneratedAt.Equal(firstAt) {
		t.Fatalf("generated_at drifted: %v vs %v", got.GeneratedAt, firstAt)
	}
	if got.WorkspaceID != wsID {
		t.Fatalf("workspace attribution drifted: %q", got.WorkspaceID)
	}

	// Saving replaces: one row per workspace, the last report wins.
	second := []byte(`{"generated_at":"2026-09-17T02:00:00Z","conflicts":[],"merges":[],"extraction_failures":0}`)
	secondAt := firstAt.Add(24 * time.Hour)
	if err := s.MemoryReports().Save(ctx, wsID, second, secondAt); err != nil {
		t.Fatalf("save replacement: %v", err)
	}
	got, err = s.MemoryReports().Get(ctx, wsID)
	if err != nil || got == nil || string(got.Report) != string(second) || !got.GeneratedAt.Equal(secondAt) {
		t.Fatalf("expected the replacement report, got (%+v, %v)", got, err)
	}

	// Unknown workspaces read as absent; saving for one is rejected.
	if got, err := s.MemoryReports().Get(ctx, "no-such-ws"); got != nil || err != nil {
		t.Fatalf("unknown workspace must read absent, got (%+v, %v)", got, err)
	}
	if err := s.MemoryReports().Save(ctx, "no-such-ws", second, secondAt); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound saving for an unknown workspace, got %v", err)
	}
	if err := s.MemoryReports().Save(ctx, wsID, nil, secondAt); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an empty report body, got %v", err)
	}
}

func TestMemoryNoteStore_SupersedeInto(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID := seedEvidenceWorld(t, s, "a")
	notes := s.MemoryNotes()
	canonical := insertMemoryNote(t, ctx, s, wsID, "note-canonical", "The deploy window is Tuesday", "evt-c")
	dup := insertMemoryNote(t, ctx, s, wsID, "note-dup", "The deploy window is Tuesday", "evt-d")

	// The survivor keeps its row; the duplicate is pointed at it (D6).
	if err := notes.SupersedeInto(ctx, wsID, dup.ID, canonical.ID); err != nil {
		t.Fatalf("supersede into: %v", err)
	}
	got, err := notes.GetNote(ctx, wsID, "", "", canonical.ID)
	if err != nil || got == nil || got.SupersededBy != nil {
		t.Fatalf("the canonical must stay current, got (%+v, %v)", got, err)
	}
	history, err := notes.ListNotesForUI(ctx, wsID, "", "", store.MemoryNoteFilters{History: true})
	if err != nil || len(history) != 2 {
		t.Fatalf("nothing is deleted (D6), got (%d, %v)", len(history), err)
	}
	gotDup, err := notes.GetNote(ctx, wsID, "", "", dup.ID)
	if err != nil || gotDup == nil {
		t.Fatalf("the folded duplicate stays queryable, got (%+v, %v)", gotDup, err)
	}
	if gotDup.SupersededBy == nil || *gotDup.SupersededBy != canonical.ID {
		t.Fatalf("the duplicate must point at the canonical, got %+v", gotDup.SupersededBy)
	}

	// Re-folding a folded note is a conflict; self-folding and unknown
	// targets are rejected.
	if err := notes.SupersedeInto(ctx, wsID, dup.ID, canonical.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict re-folding, got %v", err)
	}
	if err := notes.SupersedeInto(ctx, wsID, canonical.ID, canonical.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid self-folding, got %v", err)
	}
	if err := notes.SupersedeInto(ctx, wsID, "note-missing", canonical.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown duplicate, got %v", err)
	}
	if err := notes.SupersedeInto(ctx, wsID, canonical.ID, "note-missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown survivor, got %v", err)
	}
	// Tenant scoping: a foreign workspace id finds nothing.
	foreignWS := seedEvidenceWorld(t, s, "b")
	if err := notes.SupersedeInto(ctx, foreignWS, dup.ID, canonical.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace folds must not resolve, got %v", err)
	}
}

func TestMemoryNoteStore_EvidenceLinks(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID := seedEvidenceWorld(t, s, "a")
	notes := s.MemoryNotes()
	note := insertMemoryNote(t, ctx, s, wsID, "note-1", "The deploy window is Tuesday", "evt-1")

	// An unlinked note lists empty, not an error.
	evidence, err := notes.ListNoteEvidence(ctx, wsID, note.ID)
	if err != nil || len(evidence) != 0 {
		t.Fatalf("expected an empty evidence set, got (%+v, %v)", evidence, err)
	}

	if err := notes.AddNoteEvidence(ctx, wsID, note.ID, []string{"evt-1", "evt-2", "", "evt-3"}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	evidence, err = notes.ListNoteEvidence(ctx, wsID, note.ID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidence) != 3 {
		t.Fatalf("expected three links (empty ids skipped), got %+v", evidence)
	}
	for _, ev := range evidence {
		if ev.AddedAt.IsZero() {
			t.Fatalf("link %s must carry its add time", ev.SourceEventID)
		}
	}

	// Idempotent per (note, source event): re-adding never duplicates and
	// keeps the first link's timestamp.
	first := evidence[0]
	if err := notes.AddNoteEvidence(ctx, wsID, note.ID, []string{"evt-1", "evt-4"}); err != nil {
		t.Fatalf("re-add evidence: %v", err)
	}
	evidence, err = notes.ListNoteEvidence(ctx, wsID, note.ID)
	if err != nil || len(evidence) != 4 {
		t.Fatalf("expected four links after the idempotent re-add, got (%+v, %v)", evidence, err)
	}
	if !evidence[0].AddedAt.Equal(first.AddedAt) || evidence[0].SourceEventID != first.SourceEventID {
		t.Fatalf("re-adding must keep the first link, got %+v vs %+v", evidence[0], first)
	}

	// Links are additive and never delete anything: the raw source events
	// are untouched by construction — there is no evidence removal path.
	if err := notes.AddNoteEvidence(ctx, wsID, "note-missing", []string{"evt-1"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound linking an unknown note, got %v", err)
	}

	// Tenant scoping through the workspace partition.
	foreignWS := seedEvidenceWorld(t, s, "c")
	if evidence, err := notes.ListNoteEvidence(ctx, foreignWS, note.ID); err != nil || len(evidence) != 0 {
		t.Fatalf("cross-workspace evidence reads must be empty, got (%+v, %v)", evidence, err)
	}
}

func TestMemoryNoteStore_SetNoteTopic(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID := seedEvidenceWorld(t, s, "a")
	notes := s.MemoryNotes()
	note := insertMemoryNote(t, ctx, s, wsID, "note-1", "Postgres migrations run Friday", "evt-1")

	if err := notes.SetNoteTopic(ctx, wsID, note.ID, "databases"); err != nil {
		t.Fatalf("set topic: %v", err)
	}
	got, err := notes.GetNote(ctx, wsID, "", "", note.ID)
	if err != nil || got == nil || got.Topic == nil || *got.Topic != "databases" {
		t.Fatalf("expected the topic label, got (%+v, %v)", got, err)
	}

	// Blank topics and unknown notes are rejected; superseded notes have
	// left the browsable set and cannot be relabeled.
	if err := notes.SetNoteTopic(ctx, wsID, note.ID, "   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a blank topic, got %v", err)
	}
	if err := notes.SetNoteTopic(ctx, wsID, "note-missing", "databases"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown note, got %v", err)
	}
	dup := insertMemoryNote(t, ctx, s, wsID, "note-2", "Postgres migrations run Friday morning", "evt-2")
	if err := notes.SupersedeInto(ctx, wsID, dup.ID, note.ID); err != nil {
		t.Fatalf("fold: %v", err)
	}
	if err := notes.SetNoteTopic(ctx, wsID, dup.ID, "databases"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound relabeling a superseded note, got %v", err)
	}
}
