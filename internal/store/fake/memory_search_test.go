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

// The fake's multi-word memory search contract (fix-memory-prefetch-matching
// D4): any tokenized query term hitting the lowercased haystack suffices, the
// whole lowercased query stays an always-match leg (verbatim identifiers),
// and results order by descending matched-term count before the store's
// existing ordering. The same table runs against the note store and the
// event store — the parity contract shared with the postgres table test.

var memorySearchTable = []struct {
	name  string
	query string
	// seed holds the haystacks in insertion order; each later row is newer,
	// so "newest first" would reverse the seed order absent a rank override.
	seed []string
	want []string // expected result haystacks, in order
}{
	{
		// The headline starvation case: the old whole-string match required
		// every term as one literal substring and returned nothing.
		name:  "multi-word query retrieves a row containing only one term",
		query: "payment provider billing",
		seed: []string{
			"customer billing has been migrated",
			"unrelated deploy postmortem",
		},
		want: []string{"customer billing has been migrated"},
	},
	{
		name:  "more matched terms rank ahead of fewer",
		query: "payment provider billing",
		seed: []string{
			"provider on-call rota",              // 1 term
			"billing retries when provider slow", // 2 terms
		},
		want: []string{
			"billing retries when provider slow",
			"provider on-call rota",
		},
	},
	{
		// The whole lowercased query as a verbatim substring (a hyphenated
		// identifier, tokenized into the same terms) always matches.
		name:  "verbatim hyphenated identifier matches and ranks first",
		query: "payment-provider-billing",
		seed: []string{
			"billing notes only",                        // 1 term
			"rolled out payment-provider-billing today", // whole query verbatim, 3 terms
		},
		want: []string{
			"rolled out payment-provider-billing today",
			"billing notes only",
		},
	},
	{
		name:  "single-word behavior is unchanged",
		query: "midtrans",
		seed: []string{
			"Midtrans keys rotated",
			"Stripe keys rotated",
		},
		want: []string{"Midtrans keys rotated"},
	},
	{
		// Stopwords ride the english text-search configuration (design risk
		// note): to_tsquery('english') drops them from the shaped tsquery, so
		// a row whose only overlap is a stopword stays out and fake/postgres
		// membership stays aligned on noisy natural-language queries.
		name:  "stopword-only overlap does not match",
		query: "escalation phrase is page the on-call",
		seed: []string{
			"Atlas rehearsed the failover drill.", // matches only the stopword "the"
			"escalation phrase page call",         // real terms still answer
		},
		want: []string{"escalation phrase page call"},
	},
	{
		// Equal matched-term counts fall through to the store's existing
		// ordering: the newer row first.
		name:  "equal term counts keep the recency tie-break",
		query: "billing",
		seed: []string{
			"billing runbook from march", // older
			"billing cutover finished",   // newer
		},
		want: []string{
			"billing cutover finished",
			"billing runbook from march",
		},
	},
	{
		// Empty query keeps today's semantics: no match filtering at all, so
		// the listing returns every visible row newest first.
		name:  "empty query returns everything unfiltered",
		query: "",
		seed: []string{
			"alpha billing",
			"beta gamma",
		},
		want: []string{
			"beta gamma",
			"alpha billing",
		},
	},
}

// requireMemorySearchOrder asserts the result haystacks exactly, in order.
func requireMemorySearchOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("result count mismatch:\n got %q\nwant %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result order mismatch at %d:\n got %q\nwant %q", i, got, want)
		}
	}
}

func TestMemoryNoteStore_SearchAnyTermTable(t *testing.T) {
	for _, tc := range memorySearchTable {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := fake.New()
			ws, user, agent := seedMemoryFixtures(t, s, "mem-search-ws", "mem-search@example.com", "atlas")
			notes := s.MemoryNotes()

			base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
			for i, content := range tc.seed {
				n := newMemoryNote(ws, domain.MemoryVisibilityShared, "", content)
				at := base.Add(time.Duration(i) * time.Minute)
				n.EventTime = at
				n.LearnedAt = at
				if err := notes.InsertNote(ctx, n, domain.MemoryVisibilityShared); err != nil {
					t.Fatalf("insert seed %d: %v", i, err)
				}
			}

			if tc.query == "" {
				got, err := notes.ListNotesForUI(ctx, ws, user, agent, store.MemoryNoteFilters{})
				if err != nil {
					t.Fatalf("list notes: %v", err)
				}
				contents := make([]string, 0, len(got))
				for _, n := range got {
					contents = append(contents, n.Content)
				}
				requireMemorySearchOrder(t, contents, tc.want)
				if _, err := notes.SearchNotes(ctx, ws, user, agent, "", store.MemoryNoteFilters{}); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid for an empty search query, got %v", err)
				}
				return
			}

			found, err := notes.SearchNotes(ctx, ws, user, agent, tc.query, store.MemoryNoteFilters{})
			if err != nil {
				t.Fatalf("search notes: %v", err)
			}
			contents := make([]string, 0, len(found))
			for _, n := range found {
				contents = append(contents, n.Content)
			}
			requireMemorySearchOrder(t, contents, tc.want)
		})
	}
}

func TestMemoryEventStore_SearchAnyTermTable(t *testing.T) {
	for _, tc := range memorySearchTable {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := fake.New()
			ws, user, agent := seedMemoryFixtures(t, s, "mem-search-ev-ws", "mem-search-ev@example.com", "atlas")
			events := s.MemoryEvents()

			base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
			for i, content := range tc.seed {
				e := newMemoryEvent(ws, agent, "sess-search", "turn-search", domain.MemoryVisibilityShared, "", content)
				at := base.Add(time.Duration(i) * time.Minute)
				e.EventTime = at
				e.LearnedAt = at
				if err := events.InsertEvent(ctx, e); err != nil {
					t.Fatalf("insert seed %d: %v", i, err)
				}
			}

			if tc.query == "" {
				got, err := events.ListEventsForUI(ctx, ws, user, agent, store.MemoryEventFilters{})
				if err != nil {
					t.Fatalf("list events: %v", err)
				}
				descriptions := make([]string, 0, len(got))
				for _, e := range got {
					descriptions = append(descriptions, e.Description)
				}
				requireMemorySearchOrder(t, descriptions, tc.want)
				if _, err := events.SearchEvents(ctx, ws, user, agent, "", store.MemoryEventFilters{}); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid for an empty search query, got %v", err)
				}
				return
			}

			found, err := events.SearchEvents(ctx, ws, user, agent, tc.query, store.MemoryEventFilters{})
			if err != nil {
				t.Fatalf("search events: %v", err)
			}
			descriptions := make([]string, 0, len(found))
			for _, e := range found {
				descriptions = append(descriptions, e.Description)
			}
			requireMemorySearchOrder(t, descriptions, tc.want)
		})
	}
}

// The event haystack is Description+" "+Outcome: a term that appears only in
// the outcome must be found too.
func TestMemoryEventStore_SearchCoversOutcome(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, user, agent := seedMemoryFixtures(t, s, "mem-search-out-ws", "mem-search-out@example.com", "atlas")
	events := s.MemoryEvents()

	e := newMemoryEvent(ws, agent, "sess-outcome", "turn-outcome", domain.MemoryVisibilityShared, "", "payment provider incident review")
	e.Outcome = "billing migrated to the new processor"
	if err := events.InsertEvent(ctx, e); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	found, err := events.SearchEvents(ctx, ws, user, agent, "billing", store.MemoryEventFilters{})
	if err != nil {
		t.Fatalf("search events: %v", err)
	}
	if len(found) != 1 || found[0].ID != e.ID {
		t.Fatalf("expected the outcome-only term to match, got %d hits", len(found))
	}
}
