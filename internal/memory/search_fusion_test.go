package memory

// The wave3 fused read path (tasks 3.1–3.4, 5.1–5.4): RRF ordering vs
// hand-computed ranks, the paraphrase-only vector channel, the lexical-only
// degradation without a configured embedder, stale-dimension exclusion,
// caller-visible traversal, depth-1 containment, the shared prefetch budget,
// and zero embedder calls in pure traversal.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// scriptedSearchEmbedder serves fixed vectors per text and records every
// text it was asked to embed — the tests' control over the vector channel
// and their evidence about who called it.
type scriptedSearchEmbedder struct {
	mu      sync.Mutex
	vectors map[string][]float32
	calls   []string
	noCfg   bool
	err     error
}

func (e *scriptedSearchEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, texts...)
	if e.noCfg {
		return nil, ErrEmbeddingNotConfigured
	}
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.vectors[text]
	}
	return out, nil
}

func (e *scriptedSearchEmbedder) embedCalls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

// lexicalOnlyEmbedder is the no-model world (wave3 D4): every embed request
// answers ErrEmbeddingNotConfigured and the read path degrades to the
// lexical channel.
func lexicalOnlyEmbedder() Embedder {
	return &scriptedSearchEmbedder{noCfg: true}
}

func testCaller() Caller {
	return Caller{WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: testAgentID}
}

func seedEntity(t *testing.T, s store.Store, label string) domain.MemoryEntity {
	t.Helper()
	e := &domain.MemoryEntity{
		WorkspaceID:     testWorkspaceID,
		Label:           label,
		NormalizedLabel: domain.NormalizeEntityLabel(label),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "src-entity",
		LearnedAt:       time.Now().UTC(),
	}
	if err := s.MemoryEntities().ResolveEntity(context.Background(), e); err != nil {
		t.Fatalf("seed entity %q: %v", label, err)
	}
	return *e
}

func seedEdge(t *testing.T, s store.Store, entityID string, targetType domain.MemoryTargetType, targetID string, visibility domain.MemoryVisibility) {
	t.Helper()
	_, err := s.MemoryEntities().AddEdges(context.Background(), testWorkspaceID, []domain.MemoryEntityEdge{{
		EntityID:      entityID,
		TargetType:    targetType,
		TargetID:      targetID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		SourceEventID: "src-edge",
	}})
	if err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

func seedEmbedding(t *testing.T, s store.Store, targetType domain.MemoryTargetType, targetID string, vector []float32, visibility domain.MemoryVisibility, ownerUserID string) {
	t.Helper()
	emb := domain.MemoryEmbedding{
		WorkspaceID:   testWorkspaceID,
		TargetType:    targetType,
		TargetID:      targetID,
		Dimension:     len(vector),
		Embedding:     vector,
		Visibility:    visibility,
		SourceEventID: targetID,
		LearnedAt:     time.Now().UTC(),
	}
	if visibility == domain.MemoryVisibilityUser {
		id := ownerUserID
		emb.UserID = &id
	}
	if err := s.MemoryEmbeddings().InsertEmbeddings(context.Background(), []domain.MemoryEmbedding{emb}); err != nil {
		t.Fatalf("seed embedding: %v", err)
	}
}

func noteIDs(notes []domain.MemoryNote) []string {
	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	return ids
}

// TestRRFFusionOrderingHandComputed (task 3.4, wave3 D2): with the lexical
// channel ranking n1 > n2 > n3 and the vector channel ranking n2 > n3 > n1,
// RRF k=60 scores are
//
//	n1 = 1/61 + 1/63 = 0.032266…
//	n2 = 1/62 + 1/61 = 0.032522…   ← the fused winner
//	n3 = 1/63 + 1/62 = 0.032002…
//
// so the fused order is n2, n1, n3 — different from either channel alone.
func TestRRFFusionOrderingHandComputed(t *testing.T) {
	s := seedWorld(t)
	n1 := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "deploy window tuesday")
	n2 := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "deploy window")
	n3 := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "deploy plans")
	seedEmbedding(t, s, domain.MemoryTargetNote, n1.ID, []float32{0, 1}, domain.MemoryVisibilityShared, "")
	seedEmbedding(t, s, domain.MemoryTargetNote, n2.ID, []float32{1, 0}, domain.MemoryVisibilityShared, "")
	seedEmbedding(t, s, domain.MemoryTargetNote, n3.ID, []float32{0.6, 0.8}, domain.MemoryVisibilityShared, "")

	embedder := &scriptedSearchEmbedder{vectors: map[string][]float32{
		"deploy window tuesday": {1, 0}, // nearest to n2, farthest from n1
	}}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)

	res, err := searcher.Search(context.Background(), testCaller(), Query{Text: "deploy window tuesday"})
	if err != nil {
		t.Fatalf("fused search: %v", err)
	}
	got := noteIDs(res.Notes)
	want := []string{n2.ID, n1.ID, n3.ID}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RRF order drifted at %d: got %v want %v (hand-computed n2 > n1 > n3)", i, got, want)
		}
	}
}

// TestParaphraseOnlySurfacesViaVectorChannel (wave3 spec: vector channel
// contributes): a paraphrase with zero lexical overlap reaches the fact only
// through the embedding channel; without the embedder the same query finds
// nothing.
func TestParaphraseOnlySurfacesViaVectorChannel(t *testing.T) {
	s := seedWorld(t)
	note := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "The annual audit closes in March")
	seedEmbedding(t, s, domain.MemoryTargetNote, note.ID, []float32{1, 0}, domain.MemoryVisibilityShared, "")

	embedder := &scriptedSearchEmbedder{vectors: map[string][]float32{
		"yearly financial review finishes in spring": {1, 0},
	}}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)
	ctx := context.Background()

	res, err := searcher.Search(ctx, testCaller(), Query{Text: "yearly financial review finishes in spring"})
	if err != nil {
		t.Fatalf("paraphrase search: %v", err)
	}
	if len(res.Notes) != 1 || res.Notes[0].ID != note.ID {
		t.Fatalf("the paraphrase must surface the note via the vector channel, got %+v", res.Notes)
	}

	lexicalOnly := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	res, err = lexicalOnly.Search(ctx, testCaller(), Query{Text: "yearly financial review finishes in spring"})
	if err != nil {
		t.Fatalf("lexical-only search: %v", err)
	}
	if len(res.Notes) != 0 {
		t.Fatalf("with no embedder the paraphrase must find nothing, got %+v", res.Notes)
	}
}

// TestUnconfiguredOrFailingEmbedderFallsBackToLexical (task 3.4, wave3 D4):
// an unconfigured embedder degrades to lexical-only, structured and quiet —
// never an error, never empty-handed when the lexical channel has hits. A
// hard embedder failure degrades identically.
func TestUnconfiguredOrFailingEmbedderFallsBackToLexical(t *testing.T) {
	for name, embedder := range map[string]Embedder{
		"no model configured": lexicalOnlyEmbedder(),
		"embedder failing":    &scriptedSearchEmbedder{err: errors.New("embeddings endpoint down")},
	} {
		t.Run(name, func(t *testing.T) {
			s := seedWorld(t)
			seedNote(t, s, domain.MemoryVisibilityShared, "", "", "The deploy window is Tuesday")
			searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)

			res, err := searcher.Search(context.Background(), testCaller(), Query{Text: "deploy window"})
			if err != nil {
				t.Fatalf("lexical fallback must not error: %v", err)
			}
			if len(res.Notes) != 1 {
				t.Fatalf("the lexical channel must still serve, got %+v", res.Notes)
			}
			candidates, err := searcher.Prefetch(context.Background(), testCaller(), Query{Text: "deploy window"})
			if err != nil || len(candidates) != 1 {
				t.Fatalf("prefetch must ride the lexical floor, got %+v err %v", candidates, err)
			}
		})
	}
}

// TestDimensionFilteringStaleRowsStayLexicalOnly (task 3.4, wave3 D5): the
// vector channel searches the workspace's current dimension only — a
// stale-dimension row is never vector-returned but stays lexical-retrievable.
func TestDimensionFilteringStaleRowsStayLexicalOnly(t *testing.T) {
	s := seedWorld(t)
	current := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "fixture row alpha")
	stale := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "fixture row beta")
	seedEmbedding(t, s, domain.MemoryTargetNote, current.ID, []float32{1, 0, 0}, domain.MemoryVisibilityShared, "")
	seedEmbedding(t, s, domain.MemoryTargetNote, stale.ID, []float32{0, 1, 0, 0}, domain.MemoryVisibilityShared, "") // stale dimension 4

	embedder := &scriptedSearchEmbedder{vectors: map[string][]float32{
		// No lexical overlap with either note: the vector channel alone decides.
		"unrelated words quixotic": {1, 0, 0},
		// One term of lexical overlap with both notes; vector again dim-3.
		"fixture": {1, 0, 0},
	}}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)
	ctx := context.Background()

	res, err := searcher.Search(ctx, testCaller(), Query{Text: "unrelated words quixotic"})
	if err != nil {
		t.Fatalf("vector-only search: %v", err)
	}
	if len(res.Notes) != 1 || res.Notes[0].ID != current.ID {
		t.Fatalf("only the current-dimension row may surface via the vector channel, got %+v", res.Notes)
	}

	// Lexically, both rows match: the stale row stays reachable (D5's
	// exclusion is vector-channel-only, never a deletion).
	res, err = searcher.Search(ctx, testCaller(), Query{Text: "fixture"})
	if err != nil {
		t.Fatalf("mixed-channel search: %v", err)
	}
	got := noteIDs(res.Notes)
	if len(got) != 2 {
		t.Fatalf("both rows stay lexical-retrievable, got %v", got)
	}
}

// TestTraversalVisibilityBudiVsSari (task 5.4, spec: traversal is
// identity-bound): the same entity traversal shows each caller only their own
// visible set — the other member's user-visibility rows never surface.
func TestTraversalVisibilityBudiVsSari(t *testing.T) {
	s := seedWorld(t)
	otherUser := secondUserID(t, s)
	entity := seedEntity(t, s, "ProjectX")
	shared := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "ProjectX launch is on track")
	budis := seedNote(t, s, domain.MemoryVisibilityUser, testUserID, "", "ProjectX budget note for Budi")
	saris := seedNote(t, s, domain.MemoryVisibilityUser, otherUser, "", "ProjectX private note of Sari")
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, shared.ID, domain.MemoryVisibilityShared)
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, budis.ID, domain.MemoryVisibilityUser)
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, saris.ID, domain.MemoryVisibilityUser)

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	ctx := context.Background()

	res, err := searcher.Search(ctx, testCaller(), Query{Entity: "ProjectX"})
	if err != nil {
		t.Fatalf("traversal as budi: %v", err)
	}
	got := noteIDs(res.Notes)
	if len(got) != 2 {
		t.Fatalf("budi must see shared + own linked rows, got %v", got)
	}
	for _, id := range got {
		if id == saris.ID {
			t.Fatal("traversal leaked sari's user-visibility row to budi")
		}
	}

	sariCaller := Caller{WorkspaceID: testWorkspaceID, UserID: otherUser, AgentID: testAgentID}
	res, err = searcher.Search(ctx, sariCaller, Query{Entity: "projectx"}) // case-insensitive identity
	if err != nil {
		t.Fatalf("traversal as sari: %v", err)
	}
	got = noteIDs(res.Notes)
	if len(got) != 2 {
		t.Fatalf("sari must see shared + own linked rows, got %v", got)
	}
	for _, id := range got {
		if id == budis.ID {
			t.Fatal("traversal leaked budi's user-visibility row to sari")
		}
	}
}

// TestTraversalDepthStaysOne (task 5.4, wave3 D8): an edge to a note
// mentioning another entity does NOT chain — traversal expands the seed's
// edges only, never entity-to-entity hops.
func TestTraversalDepthStaysOne(t *testing.T) {
	s := seedWorld(t)
	alpha := seedEntity(t, s, "Alpha")
	beta := seedEntity(t, s, "Beta")
	alphaNote := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "Alpha note mentioning Beta in passing")
	betaNote := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "Beta note reachable only through Beta")
	seedEdge(t, s, alpha.ID, domain.MemoryTargetNote, alphaNote.ID, domain.MemoryVisibilityShared)
	seedEdge(t, s, beta.ID, domain.MemoryTargetNote, betaNote.ID, domain.MemoryVisibilityShared)

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	res, err := searcher.Search(context.Background(), testCaller(), Query{Entity: "Alpha"})
	if err != nil {
		t.Fatalf("traversal: %v", err)
	}
	got := noteIDs(res.Notes)
	if len(got) != 1 || got[0] != alphaNote.ID {
		t.Fatalf("depth must stay 1: only Alpha's linked row, got %v", got)
	}
}

// TestTraversalHonorsTimeWindowAndLimit (wave3 graph spec: traversal
// respects the same filters as direct search): the event-time window and the
// result cap narrow the traversed rows exactly as they narrow direct hits.
func TestTraversalHonorsTimeWindowAndLimit(t *testing.T) {
	s := seedWorld(t)
	entity := seedEntity(t, s, "Incident")
	base := time.Now().UTC().Add(-time.Hour)
	oldEvent := domain.MemoryEvent{
		WorkspaceID: testWorkspaceID, AgentID: testAgentID, SessionID: testSessionID, TurnID: "turn-old",
		Visibility: domain.MemoryVisibilityShared, Origin: domain.MemoryOriginDialogue,
		EventTime: base.Add(-time.Hour), LearnedAt: base, SourceEventID: "src-old",
		Description: "Incident older report", Outcome: "recorded",
	}
	newEvent := oldEvent
	newEvent.TurnID = "turn-new"
	newEvent.EventTime = base
	newEvent.LearnedAt = base.Add(time.Minute)
	newEvent.Description = "Incident newer report"
	newEvent.SourceEventID = "src-new"
	if err := s.MemoryEvents().InsertEvent(context.Background(), &oldEvent); err != nil {
		t.Fatalf("seed old event: %v", err)
	}
	if err := s.MemoryEvents().InsertEvent(context.Background(), &newEvent); err != nil {
		t.Fatalf("seed new event: %v", err)
	}
	seedEdge(t, s, entity.ID, domain.MemoryTargetEvent, oldEvent.ID, domain.MemoryVisibilityShared)
	seedEdge(t, s, entity.ID, domain.MemoryTargetEvent, newEvent.ID, domain.MemoryVisibilityShared)

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	ctx := context.Background()

	window := &store.MemoryTimeWindow{From: base.Add(-time.Minute)}
	res, err := searcher.Search(ctx, testCaller(), Query{Entity: "Incident", TimeWindow: window})
	if err != nil {
		t.Fatalf("windowed traversal: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].ID != newEvent.ID {
		t.Fatalf("the window must keep only the newer linked event, got %+v", res.Events)
	}

	res, err = searcher.Search(ctx, testCaller(), Query{Entity: "Incident", Limit: 1})
	if err != nil {
		t.Fatalf("limited traversal: %v", err)
	}
	if len(res.Events)+len(res.Notes) != 1 {
		t.Fatalf("the limit must cap the traversed set, got %+v", res)
	}
}

// TestTraversalIssuesZeroEmbedderCalls (task 5.4, wave3 D8): pure traversal
// is database hops only — no embedder call is issued for it, with or without
// text alongside.
func TestTraversalIssuesZeroEmbedderCalls(t *testing.T) {
	s := seedWorld(t)
	entity := seedEntity(t, s, "Alpha")
	note := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "Alpha linked note")
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, note.ID, domain.MemoryVisibilityShared)

	embedder := &scriptedSearchEmbedder{vectors: map[string][]float32{}}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)

	res, err := searcher.Search(context.Background(), testCaller(), Query{Entity: "Alpha"})
	if err != nil {
		t.Fatalf("pure traversal: %v", err)
	}
	if len(res.Notes) != 1 {
		t.Fatalf("traversal must reach the linked row, got %+v", res.Notes)
	}
	if calls := embedder.embedCalls(); len(calls) != 0 {
		t.Fatalf("pure traversal must issue zero embedder calls, got %v", calls)
	}
}

// TestAssociativePrefetchMergesSharedBudget (task 5.4, wave3 spec: the
// budget is shared, not per-channel): associative traversal candidates and
// fused text candidates merge into the same ≤5 candidate / ≤500 char budget,
// and a traversal-only row (lexically unreachable) rides along.
func TestAssociativePrefetchMergesSharedBudget(t *testing.T) {
	s := seedWorld(t)
	entity := seedEntity(t, s, "Alpha")
	long := "alpha "
	// Three lexical-only notes saturate the budget.
	for i := 0; i < 3; i++ {
		seedNote(t, s, domain.MemoryVisibilityShared, "", "", long+string(rune('a'+i)))
	}
	// The traversal-only linked note carries no lexical overlap with the query.
	linked := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "unlinked phrasing zebra")
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, linked.ID, domain.MemoryVisibilityShared)

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	candidates, err := searcher.Prefetch(context.Background(), testCaller(), Query{Text: "alpha", Entity: "Alpha"})
	if err != nil {
		t.Fatalf("associative prefetch: %v", err)
	}
	if len(candidates) > prefetchCandidates {
		t.Fatalf("the shared budget still caps at %d, got %d", prefetchCandidates, len(candidates))
	}
	total := 0
	reachedTraversal := false
	for _, c := range candidates {
		if c.SourceEventID == "" || c.Visibility == "" {
			t.Fatalf("every candidate carries its pointer and stamp: %+v", c)
		}
		total += len(c.Text)
		if c.ID == linked.ID {
			reachedTraversal = true
		}
	}
	if total > prefetchBudgetChars {
		t.Fatalf("the shared char budget holds, got %d", total)
	}
	if !reachedTraversal {
		t.Fatalf("the traversal-only row must merge into the shared budget, got %+v", candidates)
	}
}

// TestSearchEntityPlusTextFusesTraversal (wave3 retrieval spec: entity +
// free-text together): traversal seeds fuse with the text channels, so both
// the linked row and the lexically-matching row surface in one result.
func TestSearchEntityPlusTextFusesTraversal(t *testing.T) {
	s := seedWorld(t)
	entity := seedEntity(t, s, "Alpha")
	linked := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "unlinked phrasing zebra")
	seedEdge(t, s, entity.ID, domain.MemoryTargetNote, linked.ID, domain.MemoryVisibilityShared)
	lexical := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "the quixotic lexicon")

	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), lexicalOnlyEmbedder())
	res, err := searcher.Search(context.Background(), testCaller(), Query{Text: "quixotic lexicon", Entity: "Alpha"})
	if err != nil {
		t.Fatalf("entity+text search: %v", err)
	}
	got := map[string]bool{}
	for _, id := range noteIDs(res.Notes) {
		got[id] = true
	}
	if !got[linked.ID] || !got[lexical.ID] {
		t.Fatalf("entity+text must surface the traversed and the lexical row, got %v", noteIDs(res.Notes))
	}
}

// TestRawEvidenceSurfacesWithCitation (tasks 3.2/3.4, wave3 D9): when the
// raw turn leads the vector ranking (nothing extracted matches better), the
// raw hit surfaces with its hydrated turn text citing the source event id;
// when an extracted row outranks it, no raw evidence surfaces.
func TestRawEvidenceSurfacesWithCitation(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "The staging database resets nightly at three."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Understood."),
	)
	seedEmbedding(t, s, domain.MemoryTargetRaw, "e2", []float32{0, 1}, domain.MemoryVisibilityUser, testUserID)

	// No lexical overlap with the raw turn; the query vector points at the raw row.
	embedder := &scriptedSearchEmbedder{vectors: map[string][]float32{
		"schedule maintenance cadence quixotic": {0, 1},
	}}
	searcher := NewSearcher(s.MemoryNotes(), s.MemoryEvents(), s.MemoryEmbeddings(), s.MemoryEntities(), s.SessionEvents(), embedder)
	ctx := context.Background()

	res, err := searcher.Search(ctx, testCaller(), Query{Text: "schedule maintenance cadence quixotic"})
	if err != nil {
		t.Fatalf("raw-evidence search: %v", err)
	}
	if len(res.Raw) != 1 {
		t.Fatalf("the raw hit must surface when nothing extracted matches better, got %+v", res.Raw)
	}
	if res.Raw[0].SourceEventID != "e2" {
		t.Fatalf("the raw hit must cite its source event id, got %+v", res.Raw[0])
	}
	if !strings.Contains(res.Raw[0].Text, "resets nightly at three") {
		t.Fatalf("the raw hit must carry the hydrated turn text, got %q", res.Raw[0].Text)
	}

	// An extracted row that outranks the raw hit suppresses raw evidence:
	// the extracted stores hold the better match. A second query vector —
	// nearer the note than the raw turn — flips the lead to the note.
	note := seedNote(t, s, domain.MemoryVisibilityShared, "", "", "unreachable text")
	seedEmbedding(t, s, domain.MemoryTargetNote, note.ID, []float32{0.9, 0.1}, domain.MemoryVisibilityShared, "")
	embedder.vectors["nearer probe words"] = []float32{1, 0.05}
	res, err = searcher.Search(ctx, testCaller(), Query{Text: "nearer probe words"})
	if err != nil {
		t.Fatalf("extracted-better search: %v", err)
	}
	if len(res.Raw) != 0 {
		t.Fatalf("raw evidence must not surface when an extracted row leads, got %+v", res.Raw)
	}
	if len(res.Notes) != 1 || res.Notes[0].ID != note.ID {
		t.Fatalf("the extracted row must surface, got %+v", res.Notes)
	}
}
