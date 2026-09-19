package memory

import (
	"context"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Caller is the read identity. Every read computes its visible set from it
// structurally in the store layer (D8) — shared rows, the caller's own
// user-visibility rows, and the serving agent's agent-visibility rows. No
// query argument can widen or override it.
type Caller struct {
	WorkspaceID string
	UserID      string
	AgentID     string
}

// Query is the agent-settable filter surface (D8): text, time window,
// visibility bucket, topic, and a result cap. It deliberately carries no
// identity fields — the Caller is the only scope input, and the visibility
// filter can only narrow the already-visible set, never widen it.
type Query struct {
	Text             string
	TimeWindow       *store.MemoryTimeWindow
	VisibilityFilter domain.MemoryVisibility
	Topic            string
	Limit            int
}

// Candidate is one prefetch injection unit: its text, evidence pointer, and
// visibility stamp. The prefetch set is bounded at ≤5 candidates and a hard
// ≤500-char total text budget (design parameter pin).
type Candidate struct {
	Kind          string                  `json:"kind"` // "note" | "event"
	ID            string                  `json:"id"`
	SourceEventID string                  `json:"source_event_id"`
	Visibility    domain.MemoryVisibility `json:"visibility"`
	Text          string                  `json:"text"`
}

const (
	prefetchCandidates  = 5
	prefetchBudgetChars = 500
)

// SearchResult is the scope-filtered answer: notes first, then events,
// combined capped at the query limit. Zero results are structured — empty
// slices, never an error.
type SearchResult struct {
	Notes  []domain.MemoryNote
	Events []domain.MemoryEvent
}

// Searcher serves the read path over both memory stores (tasks 4.2/4.3
// core): identity pass-through scope filtering, zero model calls, zero
// writes. The later wave binds it to the memory.search tool and the compose
// prefetch.
type Searcher struct {
	notes  store.MemoryNoteStore
	events store.MemoryEventStore
}

// NewSearcher constructs the searcher over its two stores.
func NewSearcher(notes store.MemoryNoteStore, events store.MemoryEventStore) *Searcher {
	return &Searcher{notes: notes, events: events}
}

// Search runs the read-only hybrid lexical search over notes and events
// with the caller's identity passed through untouched. An empty or
// whitespace query is a structured zero result, not an error — the stores'
// own empty-query invalid-input contract is never tripped.
func (s *Searcher) Search(ctx context.Context, caller Caller, q Query) (SearchResult, error) {
	result := SearchResult{Notes: []domain.MemoryNote{}, Events: []domain.MemoryEvent{}}
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return result, nil
	}

	notes, err := s.notes.SearchNotes(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryNoteFilters{
		Visibility: q.VisibilityFilter,
		Topic:      q.Topic,
		TimeWindow: q.TimeWindow,
		Limit:      q.Limit,
	})
	if err != nil {
		return SearchResult{}, err
	}
	events, err := s.events.SearchEvents(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryEventFilters{
		Visibility: q.VisibilityFilter,
		TimeWindow: q.TimeWindow,
		Limit:      q.Limit,
	})
	if err != nil {
		return SearchResult{}, err
	}

	if len(notes) == 0 {
		notes = []domain.MemoryNote{}
	}
	if len(events) == 0 {
		events = []domain.MemoryEvent{}
	}
	if q.Limit > 0 && len(notes)+len(events) > q.Limit {
		if len(notes) >= q.Limit {
			notes = notes[:q.Limit]
			events = []domain.MemoryEvent{}
		} else {
			events = events[:q.Limit-len(notes)]
		}
	}
	result.Notes = notes
	result.Events = events
	return result, nil
}

// Prefetch returns the bounded injection set (D8): the top ≤5 note/event
// candidates within the hard ≤500-char total text budget, each carrying its
// evidence pointer and visibility stamp. Notes rank ahead of events — the
// semantic tier is the injection tier. Zero model calls; an empty or
// whitespace text returns no candidates.
func (s *Searcher) Prefetch(ctx context.Context, caller Caller, text string) ([]Candidate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []Candidate{}, nil
	}

	notes, err := s.notes.SearchNotes(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryNoteFilters{Limit: prefetchCandidates})
	if err != nil {
		return nil, err
	}
	events, err := s.events.SearchEvents(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryEventFilters{Limit: prefetchCandidates})
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, prefetchCandidates)
	budget := prefetchBudgetChars
	add := func(kind, id, sourceID string, visibility domain.MemoryVisibility, body string) {
		if len(candidates) >= prefetchCandidates || budget <= 0 {
			return
		}
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		if len(body) > budget {
			body = body[:budget]
		}
		candidates = append(candidates, Candidate{
			Kind:          kind,
			ID:            id,
			SourceEventID: sourceID,
			Visibility:    visibility,
			Text:          body,
		})
		budget -= len(body)
	}
	for i := range notes {
		add("note", notes[i].ID, notes[i].SourceEventID, notes[i].Visibility, notes[i].Content)
	}
	for i := range events {
		add("event", events[i].ID, events[i].SourceEventID, events[i].Visibility, events[i].Description+" "+events[i].Outcome)
	}
	return candidates, nil
}
