package memory

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The fused read path (wave3-memory-vectors-and-graph D2/D5/D8/D9): lexical
// and vector channels over notes and events fused with Reciprocal Rank
// Fusion, raw-evidence hits when the extracted stores hold no better match,
// and depth-1 entity traversal. Every channel computes inside the caller's
// structural visible set; fusion is rank-based, so no channel ever needs
// score calibration against another; and the vector channel is an
// enhancement, never a dependency — without a configured embedding model the
// world is byte-identical to the lexical-only pre-wave-3 read path (D4).

// rrfK is the Reciprocal Rank Fusion constant (wave3 D2, per the paper):
// a hit at 1-based rank r in a channel contributes 1/(rrfK + r). The value
// dampens single-channel rank differences so the fusion stays dominated by
// agreement across channels.
const rrfK = 60

// The prefetch budget (design parameter pin, unchanged by wave3): at most 5
// candidates within a hard 500-char total text budget, shared by every
// channel.
const (
	prefetchCandidates  = 5
	prefetchBudgetChars = 500
)

const (
	// traversalSeedLimit bounds the D8 seed fallback: when the exact
	// normalized label misses, at most this many prefix/trigram candidates
	// seed the expansion.
	traversalSeedLimit = 5
	// rawEvidenceCap bounds the raw-evidence result set: raw turns are the
	// citation floor for failed extraction, not a second result list.
	rawEvidenceCap = 5
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

// Query is the agent-settable filter surface (D8): text, an optional entity
// seed (wave3 D8 — resolved exact-normalized first, then prefix/trigram),
// time window, visibility bucket, topic, and a result cap. It deliberately
// carries no identity fields — the Caller is the only scope input, and the
// visibility filter can only narrow the already-visible set, never widen it.
type Query struct {
	Text             string
	Entity           string
	TimeWindow       *store.MemoryTimeWindow
	VisibilityFilter domain.MemoryVisibility
	Topic            string
	Limit            int
}

// Candidate is one prefetch injection unit: its text, evidence pointer, and
// visibility stamp. The prefetch set is bounded at ≤5 candidates and a hard
// ≤500-char total text budget (design parameter pin) — shared by every
// channel, never per-channel (wave3 spec: the budget is shared).
type Candidate struct {
	Kind          string                  `json:"kind"` // "note" | "event" | "raw"
	ID            string                  `json:"id"`
	SourceEventID string                  `json:"source_event_id"`
	Visibility    domain.MemoryVisibility `json:"visibility"`
	Text          string                  `json:"text"`
}

// RawHit is one raw-evidence result (wave3 D3/D9): the source turn's rendered
// text, hydrated from the session event log at read time, citing the turn's
// source event id under the existing citation lock — a raw hit has no
// extracted row of its own, so the source event id is its whole pointer.
type RawHit struct {
	SourceEventID string                  `json:"source_event_id"`
	Visibility    domain.MemoryVisibility `json:"visibility"`
	LearnedAt     time.Time               `json:"learned_at"`
	Text          string                  `json:"text"`
}

// SearchResult is the scope-filtered answer: notes first, then events, each
// fused across channels, combined capped at the query limit; raw-evidence
// hits ride alongside under their own cap (they are the evidence floor, not
// a third result list). Zero results are structured — empty slices, never an
// error.
type SearchResult struct {
	Notes  []domain.MemoryNote
	Events []domain.MemoryEvent
	Raw    []RawHit
}

// Searcher serves the fused read path over the memory stores: lexical and
// vector channels per kind fused with RRF (wave3 D2), raw-evidence hits, and
// depth-1 entity traversal (wave3 D8) — identity pass-through scope
// filtering, zero model calls in traversal, and the vector channel alone
// touching the embedding lane (one query-embedding call, fail-soft).
type Searcher struct {
	notes      store.MemoryNoteStore
	events     store.MemoryEventStore
	embeddings store.MemoryEmbeddingStore
	entities   store.MemoryEntityStore
	sessions   store.SessionEventStore
	embedder   Embedder
}

// NewSearcher constructs the searcher over its granular dependencies: the
// two extracted stores, the vector index, the entity graph, the session
// event log (raw-evidence hydration), and the workspace embedding lane.
func NewSearcher(
	notes store.MemoryNoteStore,
	events store.MemoryEventStore,
	embeddings store.MemoryEmbeddingStore,
	entities store.MemoryEntityStore,
	sessions store.SessionEventStore,
	embedder Embedder,
) *Searcher {
	return &Searcher{
		notes:      notes,
		events:     events,
		embeddings: embeddings,
		entities:   entities,
		sessions:   sessions,
		embedder:   embedder,
	}
}

// Search runs the read-only fused search over notes and events with the
// caller's identity passed through untouched. Empty text and entity
// together are a structured zero result, not an error — the stores' own
// empty-query invalid-input contract is never tripped. Entity + text fuse
// the traversal seeds with the text channels per the retrieval spec; entity
// alone runs pure traversal.
func (s *Searcher) Search(ctx context.Context, caller Caller, q Query) (SearchResult, error) {
	result := SearchResult{Notes: []domain.MemoryNote{}, Events: []domain.MemoryEvent{}, Raw: []RawHit{}}
	text := strings.TrimSpace(q.Text)
	entity := strings.TrimSpace(q.Entity)
	if text == "" && entity == "" {
		return result, nil
	}

	notes, events, raw, err := s.fused(ctx, caller, q, text, entity, q.Limit)
	if err != nil {
		return SearchResult{}, err
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
	result.Raw = raw
	return result, nil
}

// Prefetch returns the bounded injection set (D8): the top ≤5 candidates
// within the hard ≤500-char total text budget, each carrying its evidence
// pointer and visibility stamp. Notes rank ahead of events, events ahead of
// raw hits — the semantic tier is the injection tier. The budget is shared
// across every channel (wave3 spec: the budget is shared, not per-channel),
// so traversal candidates from an associative route merge into the same cap.
// Zero model calls; an empty text and entity return no candidates.
func (s *Searcher) Prefetch(ctx context.Context, caller Caller, q Query) ([]Candidate, error) {
	text := strings.TrimSpace(q.Text)
	entity := strings.TrimSpace(q.Entity)
	if text == "" && entity == "" {
		return []Candidate{}, nil
	}

	notes, events, raw, err := s.fused(ctx, caller, q, text, entity, prefetchCandidates)
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
	for i := range raw {
		// A raw hit has no row of its own: the source event id is both the
		// pointer and the citation (wave3 D9).
		add("raw", raw[i].SourceEventID, raw[i].SourceEventID, raw[i].Visibility, raw[i].Text)
	}
	return candidates, nil
}

// fused is the one retrieval path every consumer shares (wave3 task 3.3 —
// prefetch, the search tool, and the notes API free-text filter all land
// here): the lexical channel, the vector channel, and the traversal channel
// per kind, fused with RRF at channelK per channel, plus the raw-evidence
// set under its own cap.
func (s *Searcher) fused(ctx context.Context, caller Caller, q Query, text, entity string, channelK int) ([]domain.MemoryNote, []domain.MemoryEvent, []RawHit, error) {
	var lexNotes []domain.MemoryNote
	var lexEvents []domain.MemoryEvent
	if text != "" {
		var err error
		lexNotes, err = s.notes.SearchNotes(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryNoteFilters{
			Visibility: q.VisibilityFilter,
			Topic:      q.Topic,
			TimeWindow: q.TimeWindow,
			Limit:      channelK,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		lexEvents, err = s.events.SearchEvents(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, text, store.MemoryEventFilters{
			Visibility: q.VisibilityFilter,
			TimeWindow: q.TimeWindow,
			Limit:      channelK,
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	vecNotes, vecEvents, vecRaw, vecRawLeads := s.vectorChannel(ctx, caller, q, text, channelK)
	travNotes, travEvents, err := s.traversalChannel(ctx, caller, q, entity)
	if err != nil {
		return nil, nil, nil, err
	}

	noteKey := func(n domain.MemoryNote) string { return n.ID }
	eventKey := func(e domain.MemoryEvent) string { return e.ID }
	notes := rrfMerge(noteKey, lexNotes, travNotes, vecNotes)
	events := rrfMerge(eventKey, lexEvents, travEvents, vecEvents)

	raw := s.rawEvidence(ctx, caller.WorkspaceID, vecRaw, vecRawLeads)
	return notes, events, raw, nil
}

// vectorChannel runs the vector leg of the fusion (wave3 D2/D4/D5): the
// query text is embedded through the workspace's embedding lane and the
// index searched at the returned vector's length — which is the workspace's
// configured dimension whenever one is pinned (the embedder fails a batch
// that drifts), so a model change's stale-dimension rows are excluded here
// and stay lexical-retrievable (D5 exclusion). Note and event hits hydrate
// through the same scope-filtered batch reads every other channel reads
// through, keeping the hit order as the channel's rank. The channel is an
// enhancement, never a dependency: an unconfigured or failing embedder and
// any index or hydration failure degrade quietly to lexical-only (the D4
// no-model world) — the lexical channel is the read path's floor.
func (s *Searcher) vectorChannel(ctx context.Context, caller Caller, q Query, text string, channelK int) (notes []domain.MemoryNote, events []domain.MemoryEvent, raw []domain.MemoryEmbedding, rawLeads bool) {
	if text == "" {
		return nil, nil, nil, false
	}
	vectors, err := s.embedder.Embed(ctx, caller.WorkspaceID, []string{text})
	if err != nil || len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, nil, nil, false
	}
	hits, err := s.embeddings.SearchByVector(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID,
		nil, len(vectors[0]), vectors[0], store.MemoryEmbeddingFilters{Visibility: q.VisibilityFilter}, channelK)
	if err != nil {
		return nil, nil, nil, false
	}

	var noteIDs, eventIDs []string
	raw = make([]domain.MemoryEmbedding, 0)
	rawLeads = len(hits) > 0 && hits[0].TargetType == domain.MemoryTargetRaw
	for _, h := range hits {
		switch h.TargetType {
		case domain.MemoryTargetNote:
			noteIDs = append(noteIDs, h.TargetID)
		case domain.MemoryTargetEvent:
			eventIDs = append(eventIDs, h.TargetID)
		case domain.MemoryTargetRaw:
			raw = append(raw, h)
		}
	}
	noteRows, noteErr := s.notes.GetNotesByIDs(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, noteIDs)
	notes = hydrateInOrder(noteIDs, noteRows, noteErr, func(n domain.MemoryNote) string { return n.ID })
	eventRows, eventErr := s.events.GetEventsByIDs(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, eventIDs)
	events = hydrateInOrder(eventIDs, eventRows, eventErr, func(e domain.MemoryEvent) string { return e.ID })
	return notes, events, raw, rawLeads
}

// hydrateInOrder restores the channel's rank order after a batch hydration:
// the batch read returns rows in its own deterministic order, so the rows are
// re-laid onto the hit order that produced their ids. Rows the read dropped
// (vanished, foreign, or invisible between the two scope-filtered reads)
// drop out of the channel rather than leak; a read error collapses the
// channel to nothing (the vector channel is fail-soft, never a dependency).
func hydrateInOrder[T any](orderedIDs []string, rows []T, err error, id func(T) string) []T {
	if err != nil || len(orderedIDs) == 0 {
		return nil
	}
	byID := make(map[string]T, len(rows))
	for _, row := range rows {
		byID[id(row)] = row
	}
	out := make([]T, 0, len(orderedIDs))
	for _, targetID := range orderedIDs {
		row, ok := byID[targetID]
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

// traversalChannel resolves the associative seeds and expands exactly one
// hop (wave3 D8): the exact normalized label first, then prefix/trigram on
// labels through the store primitive; the seed entities' caller-visible live
// edges point at linked rows, which are read through the same scope-filtered
// batch reads every other channel uses. Depth stays 1 — entity → rows; a row
// mentioning another entity never chains. ZERO model calls and ZERO
// embedding calls: resolution and expansion are pure database hops. The
// visibility bucket and time window narrow in-memory exactly as the direct
// search filters do (the topic filter is direct-search-only: the spec binds
// traversal to the time-window and limit filters).
func (s *Searcher) traversalChannel(ctx context.Context, caller Caller, q Query, entity string) (notes []domain.MemoryNote, events []domain.MemoryEvent, err error) {
	if entity == "" {
		return nil, nil, nil
	}
	normalized := domain.NormalizeEntityLabel(entity)
	if normalized == "" {
		return nil, nil, nil
	}

	var seedIDs []string
	seed, err := s.entities.GetEntityByLabel(ctx, caller.WorkspaceID, normalized)
	if err != nil {
		return nil, nil, err
	}
	if seed != nil {
		seedIDs = append(seedIDs, seed.ID)
	} else {
		seeds, err := s.entities.FindEntitiesByLabelPrefix(ctx, caller.WorkspaceID, entity, traversalSeedLimit)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range seeds {
			seedIDs = append(seedIDs, candidate.ID)
		}
	}
	if len(seedIDs) == 0 {
		return nil, nil, nil
	}

	edges, err := s.entities.ListEdgesForEntities(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, seedIDs)
	if err != nil {
		return nil, nil, err
	}
	var noteIDs, eventIDs []string
	seenNote, seenEvent := make(map[string]struct{}), make(map[string]struct{})
	for _, edge := range edges {
		switch edge.TargetType {
		case domain.MemoryTargetNote:
			if _, dup := seenNote[edge.TargetID]; !dup {
				seenNote[edge.TargetID] = struct{}{}
				noteIDs = append(noteIDs, edge.TargetID)
			}
		case domain.MemoryTargetEvent:
			if _, dup := seenEvent[edge.TargetID]; !dup {
				seenEvent[edge.TargetID] = struct{}{}
				eventIDs = append(eventIDs, edge.TargetID)
			}
		}
	}

	notes, err = s.notes.GetNotesByIDs(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, noteIDs)
	if err != nil {
		return nil, nil, err
	}
	events, err = s.events.GetEventsByIDs(ctx, caller.WorkspaceID, caller.UserID, caller.AgentID, eventIDs)
	if err != nil {
		return nil, nil, err
	}
	notes = filterNotes(notes, q.VisibilityFilter, q.TimeWindow)
	events = filterEvents(events, q.VisibilityFilter, q.TimeWindow)
	return notes, events, nil
}

// filterNotes applies the query's visibility bucket and event-time window to
// an already scope-filtered batch (the traversal channel's optional
// filters); zero-value filters pass everything through.
func filterNotes(notes []domain.MemoryNote, visibility domain.MemoryVisibility, window *store.MemoryTimeWindow) []domain.MemoryNote {
	keep := make([]domain.MemoryNote, 0, len(notes))
	for _, n := range notes {
		if visibility != "" && n.Visibility != visibility {
			continue
		}
		if !inWindow(n.EventTime, window) {
			continue
		}
		keep = append(keep, n)
	}
	return keep
}

// filterEvents is the episodic counterpart of filterNotes.
func filterEvents(events []domain.MemoryEvent, visibility domain.MemoryVisibility, window *store.MemoryTimeWindow) []domain.MemoryEvent {
	keep := make([]domain.MemoryEvent, 0, len(events))
	for _, e := range events {
		if visibility != "" && e.Visibility != visibility {
			continue
		}
		if !inWindow(e.EventTime, window) {
			continue
		}
		keep = append(keep, e)
	}
	return keep
}

// inWindow applies the [From, To) event_time bounds; zero fields are open
// ends (the store's memoryInWindow convention).
func inWindow(t time.Time, window *store.MemoryTimeWindow) bool {
	if window == nil {
		return true
	}
	if !window.From.IsZero() && t.Before(window.From) {
		return false
	}
	if !window.To.IsZero() && !t.Before(window.To) {
		return false
	}
	return true
}

// rawEvidence decides when raw hits surface (wave3 retrieval spec: raw
// evidence MAY surface when the extracted stores hold no better match): a
// raw hit surfaces only when the vector ranking LEADS with a raw turn — the
// best vector match for this query is raw evidence — or when the vector
// channel holds no extracted rows at all (extraction failed or never ran for
// this content, and the raw turn is the only evidence). The leading raw run
// is capped at rawEvidenceCap and hydrated to its turn text; hydration
// failures drop the hit quietly (evidence without a readable window stays
// uncited rather than half-cited).
func (s *Searcher) rawEvidence(ctx context.Context, workspaceID string, hits []domain.MemoryEmbedding, rawLeads bool) []RawHit {
	if len(hits) == 0 || !rawLeads {
		// An extracted row outranks every raw hit (or the channel is empty):
		// no raw evidence surfaces.
		return nil
	}
	lead := hits
	if len(lead) > rawEvidenceCap {
		lead = lead[:rawEvidenceCap]
	}
	return s.hydrateRawHits(ctx, workspaceID, lead)
}

// hydrateRawHits resolves raw hits' source event ids to their turn windows
// through the session event log and renders the same material the gister and
// the raw embedding stage read — the hit's text is the turn, not a summary
// of it (wave3 D3/D9).
func (s *Searcher) hydrateRawHits(ctx context.Context, workspaceID string, hits []domain.MemoryEmbedding) []RawHit {
	out := make([]RawHit, 0, len(hits))
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.TargetID)
	}
	rows, err := s.sessions.EventsByIDs(ctx, workspaceID, ids)
	if err != nil {
		return out
	}
	type turnRef struct{ session, turn string }
	byEvent := make(map[string]turnRef, len(rows))
	sessions := make(map[string]struct{})
	for _, row := range rows {
		byEvent[row.EventID] = turnRef{session: row.SessionID, turn: row.TurnID}
		sessions[row.SessionID] = struct{}{}
	}
	// turn rows keyed by (session, turn): turn ids are only unique per
	// session, and several seeds may hydrate across sessions in one call.
	turns := make(map[string][]domain.SessionEvent)
	for session := range sessions {
		window, err := s.sessions.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: workspaceID,
			SessionID:   session,
		})
		if err != nil {
			continue
		}
		for _, row := range window {
			key := session + "\x00" + row.TurnID
			turns[key] = append(turns[key], row)
		}
	}
	for _, h := range hits {
		ref, ok := byEvent[h.TargetID]
		if !ok {
			continue
		}
		rows := turns[ref.session+"\x00"+ref.turn]
		if len(rows) == 0 {
			continue
		}
		text := strings.TrimSpace(renderMaterial(turnEvents(rows, ref.turn)))
		if text == "" {
			continue
		}
		out = append(out, RawHit{
			SourceEventID: h.SourceEventID,
			Visibility:    h.Visibility,
			LearnedAt:     h.LearnedAt,
			Text:          text,
		})
	}
	return out
}

// rrfMerge fuses ranked channel lists with Reciprocal Rank Fusion (wave3
// D2): a hit at 1-based rank r in a channel adds 1/(rrfK + r) to its score;
// the fused list orders by score, with best rank then key breaking ties so
// equal-score orderings stay deterministic. Single-channel fusion degenerates
// to that channel's own order — the pre-wave-3 ordering survives unchanged
// in the lexical-only world. Nil channels (routed off) contribute nothing.
func rrfMerge[T any](key func(T) string, lists ...[]T) []T {
	type entry struct {
		item  T
		score float64
		best  int
	}
	merged := make(map[string]*entry)
	for _, list := range lists {
		for i, item := range list {
			k := key(item)
			e, ok := merged[k]
			if !ok {
				e = &entry{item: item, best: i + 1}
				merged[k] = e
			}
			e.score += 1.0 / (float64(rrfK) + float64(i+1))
		}
	}
	if len(merged) == 0 {
		return nil
	}
	entries := make([]*entry, 0, len(merged))
	for _, e := range merged {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		if entries[i].best != entries[j].best {
			return entries[i].best < entries[j].best
		}
		return key(entries[i].item) < key(entries[j].item)
	})
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.item)
	}
	return out
}
