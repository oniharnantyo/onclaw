package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func nowPointer() time.Time { return time.Now().UTC() }

func seedNote(t *testing.T, s store.Store, visibility domain.MemoryVisibility, ownerUserID, ownerAgentID, content string) domain.MemoryNote {
	t.Helper()
	note := domain.MemoryNote{
		WorkspaceID:   testWorkspaceID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     nowPointer(),
		LearnedAt:     nowPointer(),
		SourceEventID: "src-1",
		Content:       content,
		Importance:    5,
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		note.UserID = &ownerUserID
	case domain.MemoryVisibilityAgent:
		note.AgentID = &ownerAgentID
	}
	if err := s.MemoryNotes().InsertNote(context.Background(), &note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	return note
}

func seedGist(t *testing.T, s store.Store, visibility domain.MemoryVisibility, ownerUserID, description string) domain.MemoryEvent {
	t.Helper()
	event := domain.MemoryEvent{
		WorkspaceID:   testWorkspaceID,
		AgentID:       testAgentID,
		SessionID:     testSessionID,
		TurnID:        "turn-1",
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     nowPointer(),
		LearnedAt:     nowPointer(),
		SourceEventID: "src-event-1",
		Description:   description,
		Outcome:       "recorded",
	}
	if visibility == domain.MemoryVisibilityUser {
		event.UserID = &ownerUserID
	}
	if err := s.MemoryEvents().InsertEvent(context.Background(), &event); err != nil {
		t.Fatalf("seed gist: %v", err)
	}
	return event
}

// TestSearchIdentityPassThrough (D8): the caller is the only scope input —
// a member sees shared rows, their own user rows, and the serving agent's
// rows, never another member's user rows. The Query carries no identity to
// override.
func TestSearchIdentityPassThrough(t *testing.T) {
	s := seedWorld(t)
	otherUser := secondUserID(t, s)
	seedNote(t, s, domain.MemoryVisibilityShared, "", "", "The workspace is called Acme")
	seedNote(t, s, domain.MemoryVisibilityUser, testUserID, "", "Alice prefers email over calls")
	seedNote(t, s, domain.MemoryVisibilityUser, otherUser, "", "Bob prefers Slack over calls")
	seedNote(t, s, domain.MemoryVisibilityAgent, "", testAgentID, "Atlas joins calls in English by default")

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())
	ctx := context.Background()

	alice := Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}
	res, err := searcher.Search(ctx, alice, Query{Text: "call", Limit: 10})
	if err != nil {
		t.Fatalf("search as alice: %v", err)
	}
	if len(res.Notes) != 3 { // shared + alice's own + the agent's
		t.Fatalf("alice must see shared + own + agent rows, got %d: %+v", len(res.Notes), res.Notes)
	}
	for _, n := range res.Notes {
		if n.Visibility == domain.MemoryVisibilityUser && *n.UserID != testUserID {
			t.Fatalf("search leaked another member's user row: %+v", n)
		}
	}

	bob := Caller{WorkspaceID: testWorkspaceID, UserID: otherUser, AgentID: testAgentID}
	res, err = searcher.Search(ctx, bob, Query{Text: "call", Limit: 10})
	if err != nil {
		t.Fatalf("search as bob: %v", err)
	}
	if len(res.Notes) != 3 {
		t.Fatalf("bob must see shared + own + agent rows, got %d", len(res.Notes))
	}
	for _, n := range res.Notes {
		if n.Visibility == domain.MemoryVisibilityUser && *n.UserID != otherUser {
			t.Fatalf("search leaked alice's user row to bob: %+v", n)
		}
	}
}

// TestSearchEventsScoped: the episodic store obeys the same predicate.
func TestSearchEventsScoped(t *testing.T) {
	s := seedWorld(t)
	otherUser := secondUserID(t, s)
	seedGist(t, s, domain.MemoryVisibilityUser, testUserID, "Alice discussed the deploy window")
	seedGist(t, s, domain.MemoryVisibilityUser, otherUser, "Bob discussed the deploy window")
	seedGist(t, s, domain.MemoryVisibilityAgent, "", "Atlas summarized the deploy window run")

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())
	res, err := searcher.Search(context.Background(),
		Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID},
		Query{Text: "deploy window"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Events) != 2 { // alice's own + the serving agent's — never Bob's
		t.Fatalf("alice must see her own gist plus the agent's, got %d", len(res.Events))
	}
	for _, ev := range res.Events {
		if ev.Visibility == domain.MemoryVisibilityUser && *ev.UserID != testUserID {
			t.Fatalf("search leaked another member's gist: %+v", ev)
		}
	}
}

// TestSearchZeroResultIsStructured: an empty result is empty slices, not an
// error — including an empty query, which never reaches the stores'
// invalid-input contract.
func TestSearchZeroResultIsStructured(t *testing.T) {
	s := seedWorld(t)
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())

	res, err := searcher.Search(context.Background(), Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}, Query{Text: "  "})
	if err != nil {
		t.Fatalf("empty query must not error: %v", err)
	}
	if res.Notes == nil || res.Events == nil {
		t.Fatalf("zero results must be empty slices, got %+v", res)
	}
	if len(res.Notes) != 0 || len(res.Events) != 0 {
		t.Fatalf("expected no results, got %+v", res)
	}

	res, err = searcher.Search(context.Background(), Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}, Query{Text: "nothing matches this"})
	if err != nil {
		t.Fatalf("zero-hit query must not error: %v", err)
	}
	if len(res.Notes) != 0 || len(res.Events) != 0 {
		t.Fatalf("expected no results, got %+v", res)
	}
}

// TestSearchFiltersAndLimit: visibility/topic/time-window filters narrow, and
// the limit caps the combined result (notes first).
func TestSearchFiltersAndLimit(t *testing.T) {
	s := seedWorld(t)
	seedNote(t, s, domain.MemoryVisibilityUser, testUserID, "", "Deploy windows topic note")
	seedNote(t, s, domain.MemoryVisibilityAgent, "", testAgentID, "Deploy windows agent note")
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())
	caller := Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}
	ctx := context.Background()

	res, err := searcher.Search(ctx, caller, Query{Text: "deploy", VisibilityFilter: domain.MemoryVisibilityAgent})
	if err != nil {
		t.Fatalf("filtered search: %v", err)
	}
	if len(res.Notes) != 1 || res.Notes[0].Visibility != domain.MemoryVisibilityAgent {
		t.Fatalf("visibility filter must narrow, got %+v", res.Notes)
	}

	res, err = searcher.Search(ctx, caller, Query{Text: "deploy", Limit: 1})
	if err != nil {
		t.Fatalf("limited search: %v", err)
	}
	if len(res.Notes)+len(res.Events) != 1 {
		t.Fatalf("limit must cap the combined result, got %+v", res)
	}
}

// TestPrefetchCaps (D8 pin): at most 5 candidates within the hard 500-char
// total text budget, each carrying its evidence pointer and visibility.
func TestPrefetchCaps(t *testing.T) {
	s := seedWorld(t)
	long := strings.Repeat("the deploy window is tuesdays ", 30) // ~900 chars
	for i := 0; i < 7; i++ {
		seedNote(t, s, domain.MemoryVisibilityShared, "", "", long)
	}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())

	candidates, err := searcher.Prefetch(context.Background(), Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}, "deploy window")
	if err != nil {
		t.Fatalf("prefetch: %v", err)
	}
	if len(candidates) > prefetchCandidates {
		t.Fatalf("prefetch must cap at %d candidates, got %d", prefetchCandidates, len(candidates))
	}
	total := 0
	for _, c := range candidates {
		if c.SourceEventID == "" {
			t.Fatalf("every candidate must carry its evidence pointer: %+v", c)
		}
		if c.Visibility == "" {
			t.Fatalf("every candidate must carry its visibility: %+v", c)
		}
		total += len(c.Text)
	}
	if total > prefetchBudgetChars {
		t.Fatalf("prefetch text budget exceeded: %d > %d chars", total, prefetchBudgetChars)
	}
	if total == 0 {
		t.Fatal("prefetch must fill its budget with the matching notes")
	}
}

// TestPrefetchEmptyTextReturnsNothing: no query, no candidates, no error.
func TestPrefetchEmptyTextReturnsNothing(t *testing.T) {
	s := seedWorld(t)
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())
	candidates, err := searcher.Prefetch(context.Background(), Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}, "   ")
	if err != nil {
		t.Fatalf("empty prefetch must not error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %+v", candidates)
	}
}

// TestPrefetchPrefersNotesAndFitsBudget: notes rank ahead of events; the
// budget truncates a candidate that would overflow, so the cap is exact.
func TestPrefetchPrefersNotesAndFitsBudget(t *testing.T) {
	s := seedWorld(t)
	seedNote(t, s, domain.MemoryVisibilityUser, testUserID, "", "deploy "+strings.Repeat("a", 400))
	seedNote(t, s, domain.MemoryVisibilityUser, testUserID, "", "deploy "+strings.Repeat("b", 300))
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents())

	candidates, err := searcher.Prefetch(context.Background(), Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}, "deploy")
	if err != nil {
		t.Fatalf("prefetch: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected both candidates to inject (the second truncated), got %+v", candidates)
	}
	total := 0
	for _, c := range candidates {
		total += len(c.Text)
	}
	if total != prefetchBudgetChars {
		t.Fatalf("the budget must be filled exactly under the cap, got %d", total)
	}
}
