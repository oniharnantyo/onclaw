package memory

// The wave3 embedding write stages (D3/D4/D5): stage ordering
// (index-before-extract), row shapes and visibility snapshots, batched row
// embeddings, fail-soft behavior, the no-model no-op, the raw toggle, and
// mixed-dimension coexistence in the vector index.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// orderRecorder records the stage markers in the exact order the worker
// executed them — the stage-ordering evidence.
type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) mark(stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, stage)
}

func (r *orderRecorder) stages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// orderRecordingModel marks the recorder before delegating to the scripted
// model — the gister and gate each get their own wrapper so their side-calls
// land on the shared timeline.
type orderRecordingModel struct {
	inner Model
	stage string
	rec   *orderRecorder
}

func (m *orderRecordingModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.rec.mark(m.stage)
	return m.inner.Generate(ctx, input, opts...)
}

func (m *orderRecordingModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.inner.Stream(ctx, input, opts...)
}

// recordingEmbedder is the scripted Embedder: every call lands on the shared
// timeline as "embed", the batches are recorded in call order, and the
// vectors, a failure, or the no-config state are scriptable.
type recordingEmbedder struct {
	rec     *orderRecorder
	mu      sync.Mutex
	batches [][]string
	vectors [][][]float32
	err     error
	noCfg   bool
}

func (e *recordingEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rec.mark("embed")
	e.batches = append(e.batches, append([]string(nil), texts...))
	if e.noCfg {
		return nil, ErrEmbeddingNotConfigured
	}
	if e.err != nil {
		return nil, e.err
	}
	if len(e.vectors) > 0 {
		vs := e.vectors[0]
		e.vectors = e.vectors[1:]
		return vs, nil
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2, 0.3}
	}
	return out, nil
}

func (e *recordingEmbedder) calls() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][]string(nil), e.batches...)
}

// TestWorkerRawEmbedRunsBeforeExtraction (D3 index-before-extract): the raw
// evidence embed is the first stage of the job — before the gister's and the
// gate's side-calls — and the row embeddings land last.
func TestWorkerRawEmbedRunsBeforeExtraction(t *testing.T) {
	s := seedWorld(t)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, time.Now().UTC().Add(-time.Hour), schema.AgenticRoleTypeUser, "Reminder: the deploy window is Tuesdays."),
	)

	rec := &orderRecorder{}
	shared := &scriptedModel{responses: []string{
		`{"description":"Deploy window discussed","outcome":"Tuesday confirmed"}`,
		`[{"op":"ADD","content":"The deploy window is Tuesday","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	gister := newTestGister(s, &orderRecordingModel{inner: shared, stage: "gist", rec: rec})
	gate := newTestGate(s, &orderRecordingModel{inner: shared, stage: "gate", rec: rec})
	embedder := &recordingEmbedder{rec: rec}
	w := NewWorker(gister, gate, embedder, s.MemoryEmbeddings(), testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	got := rec.stages()
	want := []string{"embed", "gist", "gate", "embed"}
	if len(got) != len(want) {
		t.Fatalf("stage order drifted, got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage order drifted at %d: got %v want %v", i, got, want)
		}
	}
}

// TestWorkerRawEmbedRowShape (raw evidence requirement): the raw row is
// stamped with the participant-rule visibility snapshot, the source event id
// as its citation pointer, and the turn's last event id as its target —
// one row per turn.
func TestWorkerRawEmbedRowShape(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "The staging database resets nightly."),
		chatEvent(t, "e2", "turn-1", 2, base.Add(time.Minute), schema.AgenticRoleTypeAssistant, "Understood."),
	)

	embedder := &recordingEmbedder{rec: &orderRecorder{}, vectors: [][][]float32{{{0.1, 0.2, 0.3}}}}
	gister := newTestGister(s, &scriptedModel{responses: []string{`{"description":"d","outcome":"o"}`, `[]`}})
	w := NewWorker(gister, newTestGate(s, &scriptedModel{responses: []string{`[]`}}), embedder, s.MemoryEmbeddings(), testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob()) // one human direct chat: user-visibility ceiling
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	hits, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0.1, 0.2, 0.3}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search raw: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly one raw row, got %d", len(hits))
	}
	row := hits[0]
	if row.TargetID != "e2" || row.SourceEventID != "e2" {
		t.Fatalf("raw row must cite the turn's last event, got target %q source %q", row.TargetID, row.SourceEventID)
	}
	if row.Visibility != domain.MemoryVisibilityUser || row.UserID == nil || *row.UserID != testUserID {
		t.Fatalf("raw row must carry the participant-rule snapshot, got %+v user %+v", row.Visibility, row.UserID)
	}
	if row.Dimension != 3 {
		t.Fatalf("raw row must carry the produced dimension, got %d", row.Dimension)
	}
	// The raw text is the same rendered window the gate sees.
	calls := embedder.calls()
	if len(calls) == 0 || len(calls[0]) != 1 || calls[0][0] != "user: The staging database resets nightly.\nassistant: Understood.\n" {
		t.Fatalf("raw embed must carry the turn's rendered text, got %#v", calls)
	}
}

// TestWorkerCommittedRowsEmbedInOneBatch (D4): the committed gist and note
// are embedded in ONE embeddings call and upserted as note/event rows with
// their own visibility snapshots and per-row dimension.
func TestWorkerCommittedRowsEmbedInOneBatch(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Team update: the launch moved to October."),
	)

	embedder := &recordingEmbedder{rec: &orderRecorder{}, vectors: [][][]float32{
		{{0.1, 0.2, 0.3}},      // raw turn
		{{1, 1, 1}, {2, 2, 2}}, // rows: gist then note, one shared dimension
	}}
	gistModel := &scriptedModel{responses: []string{
		`{"description":"Launch moved to October","outcome":"Confirmed"}`,
		`[{"op":"ADD","content":"The launch date moved to October","visibility":"user","importance":6,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	w := NewWorker(newTestGister(s, gistModel), newTestGate(s, gistModel), embedder, s.MemoryEmbeddings(), testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	calls := embedder.calls()
	if len(calls) != 2 {
		t.Fatalf("expected two embed calls (raw + rows), got %d", len(calls))
	}
	if len(calls[1]) != 2 {
		t.Fatalf("row embeddings must ride one batch of two texts, got %d", len(calls[1]))
	}

	hits, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		[]domain.MemoryTargetType{domain.MemoryTargetNote, domain.MemoryTargetEvent}, 3, []float32{1, 1, 1}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search rows: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected one note row and one event row, got %d", len(hits))
	}
	kinds := map[domain.MemoryTargetType]int{}
	for _, h := range hits {
		kinds[h.TargetType]++
		if h.Dimension != 3 {
			t.Fatalf("rows must carry the batch dimension, got %d", h.Dimension)
		}
	}
	if kinds[domain.MemoryTargetNote] != 1 || kinds[domain.MemoryTargetEvent] != 1 {
		t.Fatalf("expected one note + one event row, got %+v", kinds)
	}
}

// TestWorkerEmbedderFailureFailsSoft (D3/D4 fail-soft): a dead embedder
// skips both embedding stages — the extraction stages still commit, the job
// still succeeds, and the embed-failure counter rises.
func TestWorkerEmbedderFailureFailsSoft(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Remember that the API gateway rotates keys monthly."),
	)

	embedder := &recordingEmbedder{rec: &orderRecorder{}, err: errors.New("embeddings endpoint down")}
	gistModel := &scriptedModel{responses: []string{
		`{"description":"Key rotation cadence","outcome":"Monthly"}`,
		`[{"op":"ADD","content":"The API gateway rotates keys monthly","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	w := NewWorker(newTestGister(s, gistModel), newTestGate(s, gistModel), embedder, s.MemoryEmbeddings(), testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	stats := w.Stats()
	if stats.Succeeded != 1 || stats.Failed != 0 {
		t.Fatalf("an embedding failure must not fail the job, got %+v", stats)
	}
	if stats.EmbedFailures != 2 {
		t.Fatalf("both failed stages must be counted, got %+v", stats)
	}
	notes, err := s.MemoryNotes().ListNotesForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryNoteFilters{})
	if err != nil || len(notes) != 1 {
		t.Fatalf("extraction must still commit, got %d notes err %v", len(notes), err)
	}
	all, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		nil, 3, []float32{0.1, 0.2, 0.3}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil || len(all) != 0 {
		t.Fatalf("no rows may exist after a dead embedder, got %d err %v", len(all), err)
	}
}

// TestWorkerNoEmbeddingModelNoOps (D4): without a configured embedding model
// both stages no-op quietly — the lexical-only world, byte-identical to the
// pre-wave-3 pipeline, with zero embed-failure counts.
func TestWorkerNoEmbeddingModelNoOps(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Remember that the invoices close on Fridays."),
	)

	embedder := &recordingEmbedder{rec: &orderRecorder{}, noCfg: true}
	gistModel := &scriptedModel{responses: []string{
		`{"description":"Invoice cadence","outcome":"Fridays"}`,
		`[{"op":"ADD","content":"Invoices close on Fridays","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	w := NewWorker(newTestGister(s, gistModel), newTestGate(s, gistModel), embedder, s.MemoryEmbeddings(), testLogger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	stats := w.Stats()
	if stats.Succeeded != 1 || stats.Failed != 0 || stats.EmbedFailures != 0 {
		t.Fatalf("the no-model world is a quiet no-op, got %+v", stats)
	}
	notes, err := s.MemoryNotes().ListNotesForUI(ctx, testWorkspaceID, testUserID, testAgentID, store.MemoryNoteFilters{})
	if err != nil || len(notes) != 1 {
		t.Fatalf("extraction must be untouched, got %d notes err %v", len(notes), err)
	}
}

// TestWorkerRawEmbeddingToggleOff: the per-workspace raw toggle (absence =
// ON) skips only the raw stage — committed rows still embed.
func TestWorkerRawEmbeddingToggleOff(t *testing.T) {
	s := seedWorld(t)
	base := time.Now().UTC().Add(-time.Hour)
	appendEvents(t, s,
		chatEvent(t, "e1", "turn-1", 1, base, schema.AgenticRoleTypeUser, "Team update: the launch moved to October."),
	)

	embedder := &recordingEmbedder{rec: &orderRecorder{}}
	gistModel := &scriptedModel{responses: []string{
		`{"description":"Launch moved","outcome":"October"}`,
		`[{"op":"ADD","content":"The launch date moved to October","visibility":"user","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	w := NewWorker(newTestGister(s, gistModel), newTestGate(s, gistModel), embedder, s.MemoryEmbeddings(), testLogger,
		WithRawEmbeddingEnabled(func(context.Context, string) bool { return false }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob())
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	calls := embedder.calls()
	if len(calls) != 1 {
		t.Fatalf("only the row stage may embed, got %d calls", len(calls))
	}
	if len(calls[0]) != 2 {
		t.Fatalf("the row batch carries the gist and the note, got %d texts", len(calls[0]))
	}
	raw, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		[]domain.MemoryTargetType{domain.MemoryTargetRaw}, 3, []float32{0.1, 0.2, 0.3}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil || len(raw) != 0 {
		t.Fatalf("no raw rows may exist with the toggle off, got %d err %v", len(raw), err)
	}
}

// TestEmbeddingMixedDimensionsCoexist (D5): rows embedded at different
// dimensions coexist in the index; the vector channel's dimension filter
// excludes the stale-dimension rows, which stay present for the lexical
// channel.
func TestEmbeddingMixedDimensionsCoexist(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()
	// Note/event rows take read scope from the LIVE linked row, so the
	// coexistence fixture needs the real notes behind the two pointers.
	for _, id := range []string{"note-current", "note-stale"} {
		note := &domain.MemoryNote{
			ID: id, WorkspaceID: testWorkspaceID, Visibility: domain.MemoryVisibilityShared,
			Origin: domain.MemoryOriginDialogue, EventTime: time.Now().UTC().Add(-time.Hour),
			LearnedAt: time.Now().UTC().Add(-time.Hour), SourceEventID: "evt-x",
			Content: "Mixed dimension fixture " + id,
		}
		if err := s.MemoryNotes().InsertNote(ctx, note, domain.MemoryVisibilityShared); err != nil {
			t.Fatalf("seed note %s: %v", id, err)
		}
	}
	batch := []domain.MemoryEmbedding{
		{
			WorkspaceID: testWorkspaceID, TargetType: domain.MemoryTargetNote, TargetID: "note-current",
			Dimension: 3, Embedding: []float32{1, 0, 0},
			Visibility: domain.MemoryVisibilityShared, SourceEventID: "evt-1", LearnedAt: time.Now().UTC(),
		},
		{
			WorkspaceID: testWorkspaceID, TargetType: domain.MemoryTargetNote, TargetID: "note-stale",
			Dimension: 4, Embedding: []float32{0, 1, 0, 0},
			Visibility: domain.MemoryVisibilityShared, SourceEventID: "evt-2", LearnedAt: time.Now().UTC(),
		},
	}
	if err := s.MemoryEmbeddings().InsertEmbeddings(ctx, batch); err != nil {
		t.Fatalf("insert mixed-dimension rows: %v", err)
	}
	current, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		nil, 3, []float32{1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search at the current dimension: %v", err)
	}
	if len(current) != 1 || current[0].TargetID != "note-current" {
		t.Fatalf("the current dimension must see only its own rows, got %+v", current)
	}
	stale, err := s.MemoryEmbeddings().SearchByVector(ctx, testWorkspaceID, testUserID, testAgentID,
		nil, 4, []float32{0, 1, 0, 0}, store.MemoryEmbeddingFilters{}, 0)
	if err != nil {
		t.Fatalf("search at the stale dimension: %v", err)
	}
	if len(stale) != 1 || stale[0].TargetID != "note-stale" {
		t.Fatalf("the stale-dimension row must stay retrievable at its own dimension, got %+v", stale)
	}
}
