//go:build integration

package postgres_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// jsonEqual compares two JSON bodies semantically (jsonb storage does not
// preserve key order or whitespace).
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// Integration coverage for the consolidator's postgres stores
// (integrate-agent-zero-memory D12): the merge primitive (SupersedeInto),
// the multi-evidence links (AddNoteEvidence / ListNoteEvidence), the topic
// setter, and the per-workspace last morning report — plus tenant scoping
// on every read.

func TestIntegration_MemoryEvidence_NoteLifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, _, _, _, _ := seedMemoryScopesFixtures(t, s)
	notes := s.MemoryNotes()

	canonical := newPGMemoryNote(ws, domain.MemoryVisibilityShared, "", "The deploy window is Tuesday")
	canonical.ID = "11111111-1111-4111-8111-111111111111"
	canonical.SourceEventID = "evt-c"
	dup := newPGMemoryNote(ws, domain.MemoryVisibilityShared, "", "The deploy window is Tuesday")
	dup.ID = "22222222-2222-4222-8222-222222222222"
	dup.SourceEventID = "evt-d"
	for _, note := range []*domain.MemoryNote{canonical, dup} {
		if err := notes.InsertNote(ctx, note, domain.MemoryVisibilityShared); err != nil {
			t.Fatalf("insert note: %v", err)
		}
	}

	// Multi-evidence: the folded cluster's source events link on the
	// survivor, idempotent per (note, source event).
	if err := notes.AddNoteEvidence(ctx, ws, canonical.ID, []string{"evt-c", "evt-d"}); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if err := notes.AddNoteEvidence(ctx, ws, canonical.ID, []string{"evt-d", "evt-e"}); err != nil {
		t.Fatalf("re-add evidence: %v", err)
	}
	evidence, err := notes.ListNoteEvidence(ctx, ws, canonical.ID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidence) != 3 {
		t.Fatalf("expected three idempotent links, got %+v", evidence)
	}
	ids := []string{evidence[0].SourceEventID, evidence[1].SourceEventID, evidence[2].SourceEventID}
	if ids[0] != "evt-c" || ids[1] != "evt-d" || ids[2] != "evt-e" {
		t.Fatalf("links must be oldest-first, got %v", ids)
	}
	for _, ev := range evidence {
		if ev.AddedAt.IsZero() {
			t.Fatalf("link %s must carry its add time", ev.SourceEventID)
		}
	}

	// The merge primitive: the duplicate folds INTO the survivor — the
	// survivor keeps its row and the duplicate points at it (D6).
	if err := notes.SupersedeInto(ctx, ws, dup.ID, canonical.ID); err != nil {
		t.Fatalf("supersede into: %v", err)
	}
	got, err := notes.GetNote(ctx, ws, "", "", canonical.ID)
	if err != nil || got == nil || got.SupersededBy != nil {
		t.Fatalf("the canonical must stay current, got (%+v, %v)", got, err)
	}
	history, err := notes.ListNotesForUI(ctx, ws, "", "", store.MemoryNoteFilters{History: true})
	if err != nil || len(history) != 2 {
		t.Fatalf("nothing is deleted (D6), got (%d, %v)", len(history), err)
	}
	gotDup, err := notes.GetNote(ctx, ws, "", "", dup.ID)
	if err != nil || gotDup == nil || gotDup.SupersededBy == nil || *gotDup.SupersededBy != canonical.ID {
		t.Fatalf("the duplicate must point at the canonical, got (%+v, %v)", gotDup, err)
	}
	if err := notes.SupersedeInto(ctx, ws, dup.ID, canonical.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict re-folding, got %v", err)
	}
	if err := notes.SupersedeInto(ctx, ws, canonical.ID, canonical.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid self-folding, got %v", err)
	}

	// The topic setter labels live notes only.
	if err := notes.SetNoteTopic(ctx, ws, canonical.ID, "deployments"); err != nil {
		t.Fatalf("set topic: %v", err)
	}
	got, err = notes.GetNote(ctx, ws, "", "", canonical.ID)
	if err != nil || got == nil || got.Topic == nil || *got.Topic != "deployments" {
		t.Fatalf("expected the topic label, got (%+v, %v)", got, err)
	}
	if err := notes.SetNoteTopic(ctx, ws, dup.ID, "deployments"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound relabeling a superseded note, got %v", err)
	}

	// Tenant scoping: a foreign workspace finds nothing (the join through
	// memory_notes enforces the partition even though the evidence table
	// carries no workspace column).
	other := &domain.Workspace{Slug: "pg-mem-evidence-other", Name: "Other"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if evidence, err := notes.ListNoteEvidence(ctx, other.ID, canonical.ID); err != nil || len(evidence) != 0 {
		t.Fatalf("cross-workspace evidence reads must be empty, got (%+v, %v)", evidence, err)
	}
	if err := notes.AddNoteEvidence(ctx, other.ID, canonical.ID, []string{"evt-c"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace evidence writes must not resolve, got %v", err)
	}
}

func TestIntegration_MemoryReports_SaveGetRoundTrip(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, _, _, _, _ := seedMemoryScopesFixtures(t, s)
	reports := s.MemoryReports()

	// Absence is a normal state (the MemoryStore convention).
	got, err := reports.Get(ctx, ws)
	if got != nil || err != nil {
		t.Fatalf("expected (nil, nil) for an absent report, got (%+v, %v)", got, err)
	}

	first := []byte(`{"generated_at":"2026-09-16T02:00:00Z","conflicts":[],"merges":[],"extraction_failures":2}`)
	firstAt := time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC)
	if err := reports.Save(ctx, ws, first, firstAt); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = reports.Get(ctx, ws)
	if err != nil || got == nil {
		t.Fatalf("get after save: (%+v, %v)", got, err)
	}
	// jsonb does not preserve key order or whitespace — compare the bodies
	// semantically; the wire keys are what the Memory pane reads.
	if !jsonEqual(first, got.Report) {
		t.Fatalf("report body drifted:\n got %s\nwant %s", got.Report, first)
	}
	if !got.GeneratedAt.Equal(firstAt) || got.WorkspaceID != ws {
		t.Fatalf("report metadata drifted: %+v", got)
	}

	// Saving replaces: the last report per workspace wins.
	second := []byte(`{"generated_at":"2026-09-17T02:00:00Z","conflicts":[],"merges":[],"extraction_failures":0}`)
	secondAt := firstAt.Add(24 * time.Hour)
	if err := reports.Save(ctx, ws, second, secondAt); err != nil {
		t.Fatalf("save replacement: %v", err)
	}
	if got, err := reports.Get(ctx, ws); err != nil || got == nil || !jsonEqual(second, got.Report) || !got.GeneratedAt.Equal(secondAt) {
		t.Fatalf("expected the replacement report, got (%+v, %v)", got, err)
	}

	// Unknown workspaces read as absent.
	if got, err := reports.Get(ctx, "11111111-1111-4111-8111-999999999999"); got != nil || err != nil {
		t.Fatalf("unknown workspace must read absent, got (%+v, %v)", got, err)
	}
}
