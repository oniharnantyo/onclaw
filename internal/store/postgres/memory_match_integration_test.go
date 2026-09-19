//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// seedMemoryMatchNotes seeds a shared-tier corpus covering every matcher
// scenario (fix-memory-prefetch-matching 1.3): the canonical partial-overlap
// note, a two-term and a one-term note for rank ordering (the ranked note is
// back-dated so rank must beat learned_at), a pinned note proving pins stay
// dominant, a verbatim identifier only the ILIKE leg can find, a note whose
// terms all tokenize away for the zero-term query, and a pure decoy.
func seedMemoryMatchNotes(t *testing.T, s store.Store, wsID string) map[string]*domain.MemoryNote {
	t.Helper()
	ctx := context.Background()
	notes := s.MemoryNotes()
	seed := func(content string, mutate func(*domain.MemoryNote)) *domain.MemoryNote {
		t.Helper()
		n := newPGMemoryNote(wsID, domain.MemoryVisibilityShared, "", content)
		if mutate != nil {
			mutate(n)
		}
		if err := notes.InsertNote(ctx, n, domain.MemoryVisibilityShared); err != nil {
			t.Fatalf("insert note %q: %v", content, err)
		}
		return n
	}
	return map[string]*domain.MemoryNote{
		// Query "billing migration verified" matches: 2 terms.
		"migrated": seed("Customer billing has been migrated to the new provider.", nil),
		// 3 terms, back-dated: must still rank ahead of "migrated".
		"verified": seed("Billing migration verified by the team.", func(n *domain.MemoryNote) {
			n.LearnedAt = time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
		}),
		// 1 term.
		"questions": seed("Billing questions go to the finance channel.", nil),
		// Pinned but only 1 term: the pin outranks lexical quality (D3).
		"helpline": seed("Billing helpline answers within the hour.", func(n *domain.MemoryNote) {
			n.Pinned = true
		}),
		// Verbatim identifier: one fused lexeme, zero tsquery hits.
		"email": seed("Ping ops@service-now.com when the billing job finishes.", nil),
		// No surviving terms for "!!!"; only the ILIKE leg can find it.
		"bang": seed("Budget approved!!!", nil),
		// Matches none of the seeded queries.
		"coffee": seed("Coffee rotation starts next Monday.", nil),
	}
}

func TestIntegration_MemorySearch_NotesMultiWordMatching(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, userA, _, agentX, _ := seedMemoryScopesFixtures(t, s)
	seeded := seedMemoryMatchNotes(t, s, ws)
	search := func(query string, filters store.MemoryNoteFilters) []domain.MemoryNote {
		t.Helper()
		got, err := s.MemoryNotes().SearchNotes(ctx, ws, userA, agentX, query, filters)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return got
	}
	pos := func(results []domain.MemoryNote, n *domain.MemoryNote) int {
		t.Helper()
		for i, got := range results {
			if got.ID == n.ID {
				return i
			}
		}
		t.Fatalf("note %q missing from results", n.Content)
		return -1
	}

	t.Run("multi-word partial overlap retrieves the one-term note", func(t *testing.T) {
		got := search("payment provider billing", store.MemoryNoteFilters{})
		if len(got) == 0 {
			t.Fatal("expected hits for a multi-word query with partial overlap")
		}
		pos(got, seeded["migrated"])
		for _, n := range got {
			if n.ID == seeded["coffee"].ID {
				t.Fatal("note matching no query term must not be returned")
			}
		}
	})

	t.Run("rank ordering puts more-terms matches ahead, pin still dominant", func(t *testing.T) {
		got := search("billing migration verified", store.MemoryNoteFilters{})
		// Pinned (1 term) > 3 terms (back-dated) > 2 terms > 1 term.
		if !(pos(got, seeded["helpline"]) < pos(got, seeded["verified"]) &&
			pos(got, seeded["verified"]) < pos(got, seeded["migrated"]) &&
			pos(got, seeded["migrated"]) < pos(got, seeded["questions"])) {
			t.Fatalf("expected pin > more-terms > fewer-terms order, got: %v", noteContents(got))
		}
		// Top-k truncation keeps the closest matches.
		top := search("billing migration verified", store.MemoryNoteFilters{Limit: 2})
		if len(top) != 2 || top[0].ID != seeded["helpline"].ID || top[1].ID != seeded["verified"].ID {
			t.Fatalf("expected [helpline verified] in top-2, got: %v", noteContents(top))
		}
	})

	t.Run("verbatim substring still matches when no term lexically matches", func(t *testing.T) {
		got := search("ops@service-now.com", store.MemoryNoteFilters{})
		if len(got) != 1 || got[0].ID != seeded["email"].ID {
			t.Fatalf("expected exactly the verbatim-identifier note, got: %v", noteContents(got))
		}
	})

	t.Run("single-word behavior unchanged", func(t *testing.T) {
		got := search("billing", store.MemoryNoteFilters{})
		want := map[string]bool{
			seeded["migrated"].ID: true, seeded["verified"].ID: true,
			seeded["questions"].ID: true, seeded["helpline"].ID: true,
			seeded["email"].ID: true,
		}
		if len(got) != len(want) {
			t.Fatalf("expected %d billing hits, got %d: %v", len(want), len(got), noteContents(got))
		}
		for _, n := range got {
			if !want[n.ID] {
				t.Fatalf("unexpected hit for single-word query: %q", n.Content)
			}
		}
	})

	t.Run("query that tokenizes to nothing falls back to ILIKE without error", func(t *testing.T) {
		got := search("!!!", store.MemoryNoteFilters{})
		if len(got) != 1 || got[0].ID != seeded["bang"].ID {
			t.Fatalf("expected exactly the verbatim-bang note via the ILIKE fallback, got: %v", noteContents(got))
		}
	})
}

func noteContents(notes []domain.MemoryNote) []string {
	contents := make([]string, 0, len(notes))
	for _, n := range notes {
		contents = append(contents, n.Content)
	}
	return contents
}

func eventDescriptions(events []domain.MemoryEvent) []string {
	descriptions := make([]string, 0, len(events))
	for _, e := range events {
		descriptions = append(descriptions, e.Description)
	}
	return descriptions
}

// seedMemoryMatchEvents seeds the events mirror of the notes corpus
// (fix-memory-prefetch-matching 2.1). Matching runs over
// description || ' ' || outcome, so one event carries its hit term in the
// outcome to prove the outcome half participates.
func seedMemoryMatchEvents(t *testing.T, s store.Store, wsID, agentID string) map[string]*domain.MemoryEvent {
	t.Helper()
	ctx := context.Background()
	events := s.MemoryEvents()
	turn := 0
	seed := func(description, outcome string, mutate func(*domain.MemoryEvent)) *domain.MemoryEvent {
		t.Helper()
		turn++
		e := newPGMemoryEvent(wsID, agentID, "sess_match", fmt.Sprintf("turn_match_%d", turn), domain.MemoryVisibilityShared, "", description)
		e.Outcome = outcome
		if mutate != nil {
			mutate(e)
		}
		if err := events.InsertEvent(ctx, e); err != nil {
			t.Fatalf("insert event %q: %v", description, err)
		}
		return e
	}
	return map[string]*domain.MemoryEvent{
		// Query "billing migration verified" matches: 2 terms.
		"migrated": seed("Customer billing has been migrated to the new provider.", "", nil),
		// 3 terms, back-dated: must still rank ahead of "migrated".
		"verified": seed("Billing migration verified by the team.", "clean", func(e *domain.MemoryEvent) {
			e.EventTime = time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
		}),
		// 1 term.
		"questions": seed("Billing questions go to the finance channel.", "", nil),
		// 1 term, carried by the outcome half of the concatenated vector.
		"relocation": seed("The office relocation is scheduled for November.", "Postponed after the billing audit.", nil),
		// Verbatim identifier: one fused lexeme, zero tsquery hits.
		"email": seed("Ping ops@service-now.com when the billing job finishes.", "", nil),
		// No surviving terms for "!!!"; only the ILIKE leg can find it.
		"bang": seed("Budget approved!!!", "", nil),
		// Matches none of the seeded queries.
		"coffee": seed("Coffee rotation starts next Monday.", "", nil),
	}
}

func TestIntegration_MemorySearch_EventsMultiWordMatching(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, userA, _, agentX, _ := seedMemoryScopesFixtures(t, s)
	seeded := seedMemoryMatchEvents(t, s, ws, agentX)
	search := func(query string, filters store.MemoryEventFilters) []domain.MemoryEvent {
		t.Helper()
		got, err := s.MemoryEvents().SearchEvents(ctx, ws, userA, agentX, query, filters)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return got
	}
	pos := func(results []domain.MemoryEvent, e *domain.MemoryEvent) int {
		t.Helper()
		for i, got := range results {
			if got.ID == e.ID {
				return i
			}
		}
		t.Fatalf("event %q missing from results", e.Description)
		return -1
	}

	t.Run("multi-word partial overlap retrieves the one-term event", func(t *testing.T) {
		got := search("payment provider billing", store.MemoryEventFilters{})
		if len(got) == 0 {
			t.Fatal("expected hits for a multi-word query with partial overlap")
		}
		pos(got, seeded["migrated"])
		for _, e := range got {
			if e.ID == seeded["coffee"].ID {
				t.Fatal("event matching no query term must not be returned")
			}
		}
	})

	t.Run("rank ordering puts more-terms matches ahead of recency", func(t *testing.T) {
		got := search("billing migration verified", store.MemoryEventFilters{})
		// 3 terms (back-dated) > 2 terms > 1 term: rank beats event_time.
		if !(pos(got, seeded["verified"]) < pos(got, seeded["migrated"]) &&
			pos(got, seeded["migrated"]) < pos(got, seeded["questions"])) {
			t.Fatalf("expected more-terms-first order across event_time, got: %v", eventDescriptions(got))
		}
	})

	t.Run("outcome half of the vector participates in matching", func(t *testing.T) {
		got := search("relocation billing", store.MemoryEventFilters{})
		if len(got) == 0 {
			t.Fatal("expected the outcome-carried term to match")
		}
		pos(got, seeded["relocation"])
	})

	t.Run("verbatim substring still matches when no term lexically matches", func(t *testing.T) {
		got := search("ops@service-now.com", store.MemoryEventFilters{})
		if len(got) != 1 || got[0].ID != seeded["email"].ID {
			t.Fatalf("expected exactly the verbatim-identifier event, got: %v", eventDescriptions(got))
		}
	})

	t.Run("query that tokenizes to nothing falls back to ILIKE without error", func(t *testing.T) {
		got := search("!!!", store.MemoryEventFilters{})
		if len(got) != 1 || got[0].ID != seeded["bang"].ID {
			t.Fatalf("expected exactly the verbatim-bang event via the ILIKE fallback, got: %v", eventDescriptions(got))
		}
	})
}
