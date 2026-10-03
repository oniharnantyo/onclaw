package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// gateOpsJSON renders a scripted gate response.
func gateOpsJSON(ops ...string) string {
	return "```json\n[" + strings.Join(ops, ",") + "]\n```"
}

func addOp(content, visibility string) string {
	return fmt.Sprintf(`{"op":"ADD","content":%q,"visibility":%q,"importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}`, content, visibility)
}

func curateOnce(t *testing.T, s store.Store, response string, job IngestJob, material []domain.SessionEvent) (GateResult, error) {
	t.Helper()
	gate := newTestGate(s, &scriptedModel{responses: []string{response}})
	return gate.Curate(context.Background(), job, material, time.Now().UTC().Add(-time.Hour), "e2")
}

func curateMaterial(t *testing.T) []domain.SessionEvent {
	t.Helper()
	base := time.Now().UTC().Add(-time.Hour)
	return []domain.SessionEvent{
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Our payment provider is Stripe."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Understood, Stripe it is."),
	}
}

func storedNotes(t *testing.T, s store.Store) []domain.MemoryNote {
	t.Helper()
	notes, err := s.MemoryNotes().ListNotesForUI(context.Background(), testWorkspaceID, testUserID, testAgentID, store.MemoryNoteFilters{History: true})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	return notes
}

// TestGateAppliesOpsWithProvenance: ADD ops commit with the full birth tuple
// (D5) — dialogue origin (manual is forbidden for pipeline writes), window
// start as event_time, and the window's end event as the evidence pointer.
func TestGateAppliesOpsWithProvenance(t *testing.T) {
	s := seedWorld(t)
	windowStart := time.Now().UTC().Add(-time.Hour)
	gate := newTestGate(s, &scriptedModel{responses: []string{gateOpsJSON(addOp("Our payment provider is Stripe.", "user"))}})
	material := curateMaterial(t)
	windowStart = material[0].OccurredAt

	result, err := gate.Curate(context.Background(), testJob(), material, windowStart, "e2")
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected 1 committed note, got %+v", result)
	}
	if result.Counts.User != 1 || result.Counts.Shared != 0 || result.Counts.Agent != 0 {
		t.Fatalf("expected a user-visibility count, got %+v", result.Counts)
	}

	notes := storedNotes(t, s)
	if len(notes) != 1 {
		t.Fatalf("expected 1 stored note, got %d", len(notes))
	}
	note := notes[0]
	if note.Origin != domain.MemoryOriginDialogue {
		t.Fatalf("pipeline writes must be dialogue-provenanced, got %q", note.Origin)
	}
	if !note.EventTime.Equal(windowStart) {
		t.Fatalf("event_time must be the window start, got %v want %v", note.EventTime, windowStart)
	}
	if note.LearnedAt.IsZero() {
		t.Fatal("learned_at must be set")
	}
	if note.SourceEventID != "e2" {
		t.Fatalf("source_event_id must point at the window end, got %q", note.SourceEventID)
	}
	if note.UserID == nil || *note.UserID != testUserID || note.AgentID != nil {
		t.Fatalf("user-visibility note must be owned by the chatting member, got user=%v agent=%v", note.UserID, note.AgentID)
	}
}

// TestGateMalformedJSONFailsSoft: an unparseable model response fails the
// stage — the error returns, nothing is written.
func TestGateMalformedJSONFailsSoft(t *testing.T) {
	s := seedWorld(t)
	_, err := curateOnce(t, s, "I could not produce JSON today, sorry.", testJob(), curateMaterial(t))
	if err == nil {
		t.Fatal("expected a malformed response to error")
	}
	if notes := storedNotes(t, s); len(notes) != 0 {
		t.Fatalf("no notes must be written on a malformed response, got %d", len(notes))
	}
}

// TestGateEmptyMaterialIsQuietNoop: no usable material is not an error and
// spends no model call.
func TestGateEmptyMaterialIsQuietNoop(t *testing.T) {
	s := seedWorld(t)
	model := &scriptedModel{responses: []string{gateOpsJSON(addOp("never consulted", "user"))}}
	gate := newTestGate(s, model)
	result, err := gate.Curate(context.Background(), testJob(), nil, time.Now().UTC(), "e2")
	if err != nil {
		t.Fatalf("empty material must not error: %v", err)
	}
	if len(result.NoteIDs) != 0 {
		t.Fatalf("expected no commits, got %+v", result)
	}
	if len(model.callInputs()) != 0 {
		t.Fatalf("the model must not be called for empty material, got %d calls", len(model.callInputs()))
	}
}

// TestGateDedupeCollapsesNearDuplicate: an ADD that substantially duplicates
// an existing note with no new information collapses to NOOP.
func TestGateDedupeCollapsesNearDuplicate(t *testing.T) {
	s := seedWorld(t)
	userID := testUserID
	existing := &domain.MemoryNote{
		WorkspaceID:   testWorkspaceID,
		Visibility:    domain.MemoryVisibilityUser,
		UserID:        &userID,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-2 * time.Hour),
		LearnedAt:     time.Now().UTC().Add(-2 * time.Hour),
		SourceEventID: "e0",
		Content:       "Our payment provider is Stripe",
		Importance:    5,
	}
	if err := s.MemoryNotes().InsertNote(context.Background(), existing, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	// Same fact, no new information — the ADD must collapse.
	result, err := curateOnce(t, s, gateOpsJSON(addOp("Our payment provider is Stripe", "user")), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 0 {
		t.Fatalf("near-duplicate must collapse to NOOP, got %+v", result)
	}
	if notes := storedNotes(t, s); len(notes) != 1 {
		t.Fatalf("expected the pre-existing note only, got %d", len(notes))
	}
}

// TestGateSupersedeWritesCorrectionChain (D6): SUPERSEDE commits a new row
// pointing at the old one and stamps the old row's superseded_by.
func TestGateSupersedeWritesCorrectionChain(t *testing.T) {
	s := seedWorld(t)
	userID := testUserID
	old := &domain.MemoryNote{
		WorkspaceID:   testWorkspaceID,
		Visibility:    domain.MemoryVisibilityUser,
		UserID:        &userID,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Now().UTC().Add(-2 * time.Hour),
		LearnedAt:     time.Now().UTC().Add(-2 * time.Hour),
		SourceEventID: "e0",
		Content:       "Our payment provider is Stripe",
		Importance:    5,
	}
	if err := s.MemoryNotes().InsertNote(context.Background(), old, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	op := fmt.Sprintf(`{"op":"SUPERSEDE","content":"Our payment provider is Midtrans","visibility":"user","importance":6,"pin":false,"explicit_request":false,"supersedes":%q,"topic":null,"conflict_with_doc":false}`, old.ID)
	result, err := curateOnce(t, s, gateOpsJSON(op), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the correction to commit, got %+v", result)
	}

	notes := storedNotes(t, s) // History included
	if len(notes) != 2 {
		t.Fatalf("expected old + correction rows, got %d", len(notes))
	}
	var corrected, correction *domain.MemoryNote
	for i := range notes {
		if notes[i].ID == old.ID {
			corrected = &notes[i]
		} else {
			correction = &notes[i]
		}
	}
	if corrected == nil || correction == nil {
		t.Fatalf("expected the supersede chain, got %+v", notes)
	}
	if corrected.SupersededBy == nil || *corrected.SupersededBy != correction.ID {
		t.Fatalf("old row must point at the correction, got %+v", corrected)
	}
	if correction.Supersedes == nil || *correction.Supersedes != old.ID {
		t.Fatalf("correction must point at the old row, got %+v", correction)
	}
	if correction.Content != "Our payment provider is Midtrans" {
		t.Fatalf("correction content drifted: %q", correction.Content)
	}
}

// TestGateSupersedeTargetMissingSkipsOpButKeepsBatch: one rejected op never
// rejects the whole batch (D4).
func TestGateSupersedeTargetMissingSkipsOpButKeepsBatch(t *testing.T) {
	s := seedWorld(t)
	missing := "note-does-not-exist"
	op := fmt.Sprintf(`{"op":"UPDATE","content":"Orphan correction","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":%q,"topic":null,"conflict_with_doc":false}`, missing)
	response := gateOpsJSON(op, addOp("The deploy window is Tuesday", "user"))

	result, err := curateOnce(t, s, response, testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("a rejected op must not fail the batch: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the healthy op to commit, got %+v", result)
	}
	if notes := storedNotes(t, s); len(notes) != 1 {
		t.Fatalf("expected only the healthy note, got %d", len(notes))
	}
}

// TestGateClampsVisibilityToCeiling (D4): a DM can birth at most
// user-visibility facts — a shared proposal is clamped, never rejected.
func TestGateClampsVisibilityToCeiling(t *testing.T) {
	s := seedWorld(t)
	result, err := curateOnce(t, s, gateOpsJSON(addOp("Even phrased as share-with-everyone, this is a DM fact", "shared")), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the clamped note to commit, got %+v", result)
	}
	if result.Counts.User != 1 {
		t.Fatalf("expected the note stored at the ceiling (user), got %+v", result.Counts)
	}
	note := storedNotes(t, s)[0]
	if note.Visibility != domain.MemoryVisibilityUser {
		t.Fatalf("expected user visibility after the clamp, got %q", note.Visibility)
	}
	if note.UserID == nil || *note.UserID != testUserID {
		t.Fatalf("clamped note must be owned by the chatting member, got %+v", note.UserID)
	}
}

// TestGateExplicitRequestFlagsImportanceAndPin (task 3.4): an explicit
// remember-request commits high-importance and pin-eligible.
func TestGateExplicitRequestFlagsImportanceAndPin(t *testing.T) {
	s := seedWorld(t)
	op := `{"op":"ADD","content":"The user asked to remember: invoice prefix is INV-","visibility":"user","importance":3,"pin":false,"explicit_request":true,"supersedes":null,"topic":null,"conflict_with_doc":false}`
	result, err := curateOnce(t, s, gateOpsJSON(op), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the remembered fact to commit, got %+v", result)
	}
	note := storedNotes(t, s)[0]
	if note.Importance < highImportance {
		t.Fatalf("explicit requests must land high-importance (>= %d), got %d", highImportance, note.Importance)
	}
	if !note.Pinned {
		t.Fatal("explicit requests must be pin-eligible (pinned)")
	}
}

// TestGateUnknownVisibilityDefaultsNarrowest (D4): an unknown tier falls
// back to the narrowest tier (agent), never the widest.
func TestGateUnknownVisibilityDefaultsNarrowest(t *testing.T) {
	s := seedWorld(t)
	result, err := curateOnce(t, s, gateOpsJSON(addOp("Atlas restarts itself when the provider 5xx loops", "banana")), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the note to commit, got %+v", result)
	}
	if result.Counts.Agent != 1 {
		t.Fatalf("expected an agent-visibility note, got %+v", result.Counts)
	}
	note := storedNotes(t, s)[0]
	if note.Visibility != domain.MemoryVisibilityAgent {
		t.Fatalf("unknown tiers must default to the narrowest, got %q", note.Visibility)
	}
	if note.AgentID == nil || *note.AgentID != testAgentID {
		t.Fatalf("agent-visibility note must be owned by the producing agent, got %+v", note.AgentID)
	}
}

// TestGateConflictWithDocFlagsNote (D7): a doc contradiction is stored with
// the review flag — the documents themselves are never touched.
func TestGateConflictWithDocFlagsNote(t *testing.T) {
	s := seedWorld(t)
	op := `{"op":"ADD","content":"The headquarters is in Bandung","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":true}`
	result, err := curateOnce(t, s, gateOpsJSON(op), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 1 {
		t.Fatalf("expected the flagged note to commit, got %+v", result)
	}
	note := storedNotes(t, s)[0]
	if note.ConflictFlag == nil || *note.ConflictFlag != docConflictFlag {
		t.Fatalf("expected the doc-conflict review flag, got %+v", note.ConflictFlag)
	}
}

// TestGateNoopOnlyTurnStoresNothing: unusable material yields NOOP — no
// rows, a clean zero summary.
func TestGateNoopOnlyTurnStoresNothing(t *testing.T) {
	s := seedWorld(t)
	result, err := curateOnce(t, s, gateOpsJSON(`{"op":"NOOP","content":"","visibility":"user","importance":0,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}`), testJob(), curateMaterial(t))
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if len(result.NoteIDs) != 0 {
		t.Fatalf("NOOP must store nothing, got %+v", result)
	}
	if notes := storedNotes(t, s); len(notes) != 0 {
		t.Fatalf("expected an empty store, got %d notes", len(notes))
	}
}

// TestLogCurationDecisionCountsOps pins the per-curation info record: one
// line per curation decision with the proposed ops broken out by kind and
// the committed/rejected outcome — the write-side counterpart of the intent
// gate's per-turn decision log.
func TestLogCurationDecisionCountsOps(t *testing.T) {
	log := &captureLogHandler{}
	job := IngestJob{WorkspaceID: "ws-1", AgentID: "agent-1", SessionID: "sess-1", TurnID: "turn-9"}

	logCurationDecision(slog.New(log), job, []gateOp{{Op: "ADD"}, {Op: "ADD"}, {Op: "UPDATE"}, {Op: ""}}, 2, 1, 150*time.Millisecond)

	if len(log.records) != 1 {
		t.Fatalf("expected exactly one curation decision record, got %d", len(log.records))
	}
	rec := log.records[0]
	if rec.Level != slog.LevelInfo || rec.Message != "memory: curation gate decided turn facts" {
		t.Fatalf("curation log = %s/%q, want info %q", rec.Level, rec.Message, "memory: curation gate decided turn facts")
	}
	got := map[string]string{}
	rec.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	want := map[string]string{
		"workspace_id": "ws-1", "agent_id": "agent-1", "session_id": "sess-1", "turn_id": "turn-9",
		"ops_proposed": "4", "ops_add": "2", "ops_update": "1", "ops_supersede": "0", "ops_noop": "1",
		"committed": "2", "rejected": "1",
	}
	for key, expect := range want {
		if got[key] != expect {
			t.Errorf("curation log %s = %q, want %q", key, got[key], expect)
		}
	}
	if _, ok := got["elapsed_ms"]; !ok {
		t.Errorf("curation log missing elapsed_ms")
	}
}
