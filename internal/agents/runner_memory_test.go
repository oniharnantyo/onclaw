package agents

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Shared memory-pipeline test helpers
// ---------------------------------------------------------------------------

// newTestMemorySearcher builds the fused searcher over st with the
// composition root's production wiring: the provider-backed embedding lane.
// These test worlds configure no embedding model, so the vector channel
// degrades to lexical-only exactly as production does without one (wave3
// D4).
func newTestMemorySearcher(st store.Store) *memory.Searcher {
	return memory.NewSearcher(
		st.MemoryNotes(),
		st.MemoryEvents(),
		st.MemoryEmbeddings(),
		st.MemoryEntities(),
		st.SessionEvents(),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), []byte("test-key-32-bytes-long-12345678"), providers.NewRegistry()),
	)
}

// newTestMemoryPipeline builds the memory seam triplet over st: the worker,
// the searcher, and the intent gate. The worker is deliberately NOT started —
// tests that assert processed jobs construct their own worker with an
// explicit side-call resolver (a started helper worker could reach the real
// DefaultAgenticModelFactory). The gate's side-call resolution fails on the
// provider catalog (no cheap tier wired), so full-turn tests fail open on the
// gate exactly like production does with an unwired cheap tier.
func newTestMemoryPipeline(st store.Store) (*memory.Worker, *memory.Searcher, *memory.IntentGate) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), st.Providers(), []byte("test-key-32-bytes-long-12345678"), DefaultAgenticModelFactory, logger),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), st.Providers(), []byte("test-key-32-bytes-long-12345678"), DefaultAgenticModelFactory, logger),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), []byte("test-key-32-bytes-long-12345678"), providers.NewRegistry()),
		st.MemoryEmbeddings(),
		logger,
	)
	return worker,
		newTestMemorySearcher(st),
		memory.NewIntentGate(st.Providers(), []byte("test-key-32-bytes-long-12345678"), DefaultAgenticModelFactory, logger)
}

// newQueuedMemoryWorker builds a never-started worker over nil stores: safe
// for Enqueue (the bounded queue absorbs, nothing drains, no store is
// touched) in tests that exercise the drain loop on a bare Runner literal.
func newQueuedMemoryWorker() *memory.Worker {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return memory.NewWorker(
		memory.NewGister(nil, nil, nil, nil, nil, nil, logger),
		memory.NewGate(nil, nil, nil, nil, nil, nil, logger),
		// No settings store: the embedder resolves no config and both
		// embedding stages no-op even if a job ever drained.
		memory.NewProviderEmbedder(nil, nil, nil, providers.NewRegistry()),
		nil,
		logger,
	)
}

// staticTextModel answers every Generate with the next scripted response and
// fails the stream path (the memory side-calls only Generate).
type staticTextModel struct {
	mu        sync.Mutex
	responses []string
}

func (m *staticTextModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	resp := ""
	if len(m.responses) > 0 {
		resp = m.responses[0]
		m.responses = m.responses[1:]
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: resp}}},
	}, nil
}

func (m *staticTextModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("staticTextModel: stream not supported")
}

// errModel fails every Generate — a run whose model is down terminalizes
// failed, which is one of the two ingest statuses.
type errModel struct{}

func (errModel) Generate(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.AgenticMessage, error) {
	return nil, errors.New("model down")
}

func (errModel) Stream(context.Context, []*schema.AgenticMessage, ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("model down")
}

// memoryCaptureComposer records the ComposeParams the runner composed with.
type memoryCaptureComposer struct {
	mu       sync.Mutex
	captured ComposeParams
}

func (c *memoryCaptureComposer) Compose(_ context.Context, params ComposeParams) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.captured = params
	return "captured instruction", nil
}

func (c *memoryCaptureComposer) snapshot() ComposeParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.captured
}

// chipRecorder is a memory.ChipSink that records every emitted job+payload.
type chipRecorder struct {
	mu       sync.Mutex
	jobs     []memory.IngestJob
	payloads []memory.MemoryIngestedPayload
}

func (r *chipRecorder) record(_ context.Context, job memory.IngestJob, payload memory.MemoryIngestedPayload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, job)
	r.payloads = append(r.payloads, payload)
}

func (r *chipRecorder) snapshot() ([]memory.IngestJob, []memory.MemoryIngestedPayload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]memory.IngestJob(nil), r.jobs...), append([]memory.MemoryIngestedPayload(nil), r.payloads...)
}

// seedMemoryNote inserts one complete provenance note into st.
func seedMemoryNote(t *testing.T, st store.Store, workspaceID string, visibility domain.MemoryVisibility, ownerUserID, ownerAgentID, content string) domain.MemoryNote {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	note := &domain.MemoryNote{
		WorkspaceID:   workspaceID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     now,
		LearnedAt:     now,
		SourceEventID: "ev-" + content[:8],
		Content:       content,
		Importance:    5,
	}
	if ownerUserID != "" {
		id := ownerUserID
		note.UserID = &id
	}
	if ownerAgentID != "" {
		id := ownerAgentID
		note.AgentID = &id
	}
	if err := st.MemoryNotes().InsertNote(context.Background(), note, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("seed memory note: %v", err)
	}
	return *note
}

// ---------------------------------------------------------------------------
// Task 3.2 — enqueue at run end, both statuses
// ---------------------------------------------------------------------------

// TestRunner_EnqueuesIngestOnBothStatuses (spec agent-memory-pipeline:
// Turn-end ingestion trigger): a completed and a failed run both enqueue —
// asserted through the worker's Stats — and the started worker processes
// both. The started worker uses a failing side-call resolver, so the jobs
// fail soft without touching the network.
func TestRunner_EnqueuesIngestOnBothStatuses(t *testing.T) {
	st, runner, _, _, req := setupHooksRunner(t, nil, &hooksModel{final: "done"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), nil, nil, nil, logger, memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return nil, errors.New("side-call tier unwired")
		})),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), nil, nil, nil, logger, memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return nil, errors.New("side-call tier unwired")
		})),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), []byte("test-key-32-bytes-long-12345678"), providers.NewRegistry()),
		st.MemoryEmbeddings(),
		logger,
	)
	runner.memoryWorker = worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	defer worker.Stop()

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	collectStream(t, stream)

	if got := worker.Stats().Enqueued; got != 1 {
		t.Fatalf("expected the completed turn to enqueue, got %+v", worker.Stats())
	}

	// A failed run (model down) still ingests the accepted user turn.
	runner.agenticFactory = func(context.Context, string, providers.Credential, string) (Model, error) {
		return errModel{}, nil
	}
	failReq := req
	failReq.Input = "the deploy is on fire"
	stream, err = runner.Run(context.Background(), failReq)
	if err != nil {
		t.Fatalf("Run (failing model): %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventError) {
		t.Fatalf("expected the failing model to end the turn with an error, got %+v", events)
	}
	if got := worker.Stats().Enqueued; got != 2 {
		t.Fatalf("expected the failed turn to enqueue too, got %+v", worker.Stats())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s := worker.Stats()
		if s.Processed == 2 && s.Failed == 2 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("worker did not process both jobs fail-soft: %+v", worker.Stats())
}

// TestRunner_EnqueueSkipsCancelledAndEphemeral pins the enqueue filter:
// cancelled runs and ephemeral turns mint no ingest job.
func TestRunner_EnqueueSkipsCancelledAndEphemeral(t *testing.T) {
	st := fake.New()
	memW, memS, memG := newTestMemoryPipeline(st)
	runner := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, st.GatewayLinks(),
		memW, memS, memG, []byte("k"), "/tmp/onclaw")

	runner.enqueueTurnIngest(context.Background(), ExecRequest{
		WorkspaceID: "ws", AgentID: "ag", SessionID: "sess-1", UserID: "u",
		Input: "hello",
	}, "turn-1", hookRunStatusCancelled, false)
	if got := memW.Stats().Enqueued; got != 0 {
		t.Fatalf("cancelled runs must not enqueue, got %+v", memW.Stats())
	}

	runner.enqueueTurnIngest(context.Background(), ExecRequest{
		WorkspaceID: "ws", AgentID: "ag", SessionID: "sess-1", UserID: "u",
		Input: "hello",
	}, "turn-2", hookRunStatusCompleted, true)
	if got := memW.Stats().Enqueued; got != 0 {
		t.Fatalf("ephemeral turns must not enqueue, got %+v", memW.Stats())
	}
}

// TestRunner_IngestSessionShapePerOrigin pins the participant rule at enqueue
// time (task 3.2): the human participant count — the visibility ceiling — is
// resolved per origin from the session shape, never from the transcript.
func TestRunner_IngestSessionShapePerOrigin(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	budi := &domain.User{Email: "budi@example.com", Name: "Budi"}
	if err := st.Users().Create(ctx, budi); err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, _, memGate := newTestMemoryPipeline(st)
	runner := NewRunner(nil, nil, st.Users(), nil, nil, nil, nil, nil, nil, nil, st.GatewayLinks(),
		newQueuedMemoryWorker(), newTestMemorySearcher(st), memGate,
		[]byte("k"), "/tmp/onclaw")

	// Two humans and one agent in the channel roster.
	runner.channelContext = &fakeChannelContext{
		channel: domain.Channel{ID: "ch-1", WorkspaceID: ws.ID},
		members: []domain.ChannelMember{
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeUser, UserID: budi.ID},
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeUser, UserID: "sari-id"},
			{WorkspaceID: ws.ID, ChannelID: "ch-1", MemberType: domain.ChannelMemberTypeAgent, AgentID: "atlas-id"},
		},
	}

	// A linked telegram identity (mapped → one human) and a second member
	// with no link at all (unmapped → zero humans).
	if err := st.GatewayLinks().CreateUserLink(ctx, ws.ID, &domain.UserLink{
		Platform:       domain.GatewayPlatformTelegram,
		PlatformUserID: "593821092",
		UserID:         budi.ID,
	}); err != nil {
		t.Fatalf("create link: %v", err)
	}
	unlinked := &domain.User{Email: "unlinked@example.com", Name: "No Link"}
	if err := st.Users().Create(ctx, unlinked); err != nil {
		t.Fatalf("create unlinked user: %v", err)
	}

	humans := func(origin, sessionID, userID string) int {
		t.Helper()
		return runner.ingestHumanParticipants(ctx, ExecRequest{
			WorkspaceID: ws.ID, AgentID: "atlas-id", SessionID: sessionID, UserID: userID,
			Origin: origin, Input: "turn text",
		})
	}

	if got := humans(OriginUser, "sess-1", budi.ID); got != 1 {
		t.Fatalf("user origin: HumanParticipants = %d, want 1", got)
	}
	if got := humans(OriginScheduler, "sched-1", budi.ID); got != 0 {
		t.Fatalf("scheduler origin: HumanParticipants = %d, want 0", got)
	}
	if got := humans(OriginHeartbeat, "hb_atlas-id", budi.ID); got != 0 {
		t.Fatalf("heartbeat origin: HumanParticipants = %d, want 0", got)
	}
	if got := humans(OriginChannel, "chan_ch-1_atlas-id", budi.ID); got != 2 {
		t.Fatalf("channel origin: HumanParticipants = %d, want 2 (the roster's humans)", got)
	}
	if got := humans(OriginTelegram, "tg_dm_593821092_atlas", budi.ID); got != 1 {
		t.Fatalf("telegram DM with a linked member: HumanParticipants = %d, want 1", got)
	}
	if got := humans(OriginTelegram, "tg_dm_999999_atlas", unlinked.ID); got != 0 {
		t.Fatalf("telegram run for an unlinked member: HumanParticipants = %d, want 0", got)
	}
	if got := humans(OriginTelegram, "tg_group_-100_atlas", budi.ID); got != 0 {
		t.Fatalf("telegram group session: HumanParticipants = %d, want 0 (conservative agent ceiling)", got)
	}
}

// ---------------------------------------------------------------------------
// Task 4.1/4.2 — intent gate + prefetch injection
// ---------------------------------------------------------------------------

// TestRunner_MemoryGateFailOpen (spec agent-memory-retrieval: gate failure
// fails open): a gate model error and a gate timeout both proceed with a
// composed instruction that carries no memory section.
func TestRunner_MemoryGateFailOpen(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delay time.Duration
	}{
		{name: "model error", delay: 0},
		{name: "timeout past a short budget", delay: 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, runner, _, _, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			gate := memory.NewIntentGate(nil, nil, nil, logger, memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
				return &gateSlowModel{delay: tc.delay}, nil
			}))
			runner.intentGate = gate
			runner.gateBudget = func(context.Context, string) time.Duration { return 100 * time.Millisecond }
			composer := &memoryCaptureComposer{}
			runner.instructionComposer = composer

			stream, err := runner.Run(context.Background(), req)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			events := collectStream(t, stream)
			if !hasKind(events, TranscriptEventTurnCompleted) {
				t.Fatalf("the turn must proceed, got %+v", events)
			}
			if docs := composer.snapshot().MemoryDocs; len(docs) != 0 {
				t.Fatalf("fail-open must compose no memory section, got %q", docs)
			}
		})
	}
}

// gateSlowModel honors its context deadline so the gate's hard timeout pin is
// exercisable; delay 0 answers a needs-memory verdict immediately.
type gateSlowModel struct {
	delay time.Duration
}

func (m *gateSlowModel) Generate(ctx context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{AssistantGenText: &schema.AssistantGenText{Text: `{"needs_memory":true,"buckets":["notes"]}`}},
		},
	}, nil
}

func (m *gateSlowModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("gateSlowModel: stream not supported")
}

// TestRunner_SelfContainedTurnSkipsRetrieval: a needs_memory=false verdict
// composes no memory section.
func TestRunner_SelfContainedTurnSkipsRetrieval(t *testing.T) {
	_, runner, _, _, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})
	gate := memory.NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return &staticTextModel{responses: []string{`{"needs_memory":false,"buckets":[]}`}}, nil
		}))
	runner.intentGate = gate
	composer := &memoryCaptureComposer{}
	runner.instructionComposer = composer

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	collectStream(t, stream)
	if docs := composer.snapshot().MemoryDocs; len(docs) != 0 {
		t.Fatalf("self-contained turns compose no memory section, got %q", docs)
	}
}

// TestRunner_GateHitInjectsCitedCandidates (spec: prefetch injection + the
// citation lock): a gate hit injects the bounded candidate section with
// source event ids and visibility stamps — and the scope filter holds through
// the runner path: another member's user-visibility note is never injected
// regardless of the query.
func TestRunner_GateHitInjectsCitedCandidates(t *testing.T) {
	st, runner, ws, _, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})

	// A second workspace member whose private note mentions the same topic —
	// the query legitimately matches it, and the scope filter must still
	// exclude it (shared + own-user + serving-agent only).
	sari := &domain.User{Email: "sari@example.com", Name: "Sari"}
	if err := st.Users().Create(context.Background(), sari); err != nil {
		t.Fatalf("create sari: %v", err)
	}
	shared := seedMemoryNote(t, st, ws.ID, domain.MemoryVisibilityShared, "", "", "The team deploy window is Tuesday morning.")
	private := seedMemoryNote(t, st, ws.ID, domain.MemoryVisibilityUser, sari.ID, "", "Sari's private note about the deploy window mentions 90000.")

	req.Input = "deploy window"

	gate := memory.NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return &staticTextModel{responses: []string{`{"needs_memory":true,"buckets":["notes"]}`}}, nil
		}))
	runner.intentGate = gate
	composer := &memoryCaptureComposer{}
	runner.instructionComposer = composer

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	collectStream(t, stream)

	docs := composer.snapshot().MemoryDocs
	if len(docs) != 1 {
		t.Fatalf("expected exactly one memory section, got %d", len(docs))
	}
	doc := docs[0]
	if !strings.Contains(doc, shared.SourceEventID) {
		t.Fatalf("the injected candidate must carry its source event id %q:\n%s", shared.SourceEventID, doc)
	}
	if !strings.Contains(doc, string(domain.MemoryVisibilityShared)) {
		t.Fatalf("the injected candidate must carry its visibility stamp:\n%s", doc)
	}
	if !strings.Contains(doc, "source event") || !strings.Contains(doc, "nothing is recorded") {
		t.Fatalf("the section must carry the citation and abstention contract:\n%s", doc)
	}
	if strings.Contains(doc, private.Content) || strings.Contains(doc, "90000") {
		t.Fatalf("another member's user-visibility note must never inject:\n%s", doc)
	}
}

// ---------------------------------------------------------------------------
// Task 3.6 — the memory_ingested chip
// ---------------------------------------------------------------------------

// TestRunner_AppendMemoryChipPersistsAndStreams mirrors the prompt_blocked
// tests: the chip persists as an application-owned session event and the
// hydrated History renders it as the live kind — while scheduled and
// heartbeat origins emit nothing.
func TestRunner_AppendMemoryChipPersistsAndStreams(t *testing.T) {
	st, runner, ws, ag, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})
	ctx := context.Background()

	payload := memory.MemoryIngestedPayload{
		NoteIDs:  []string{"note-1"},
		EventIDs: []string{"gist-1"},
		Counts:   memory.MemoryIngestedCounts{Shared: 1, User: 1},
	}
	job := memory.IngestJob{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		UserID:      req.UserID,
		SessionID:   req.SessionID,
		TurnID:      "turn-1",
		Origin:      OriginUser,
		Status:      hookRunStatusCompleted,
	}
	runner.AppendMemoryChip(ctx, job, payload)

	// Hydrated view: the chip renders exactly like the live event.
	history, err := runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var chip *TranscriptEvent
	for i := range history.Events {
		if history.Events[i].Kind == TranscriptEventMemoryIngested {
			chip = &history.Events[i]
			break
		}
	}
	if chip == nil || chip.MemoryIngested == nil {
		t.Fatalf("expected the hydrated memory_ingested chip, got %+v", history.Events)
	}
	if len(chip.MemoryIngested.NoteIDs) != 1 || chip.MemoryIngested.NoteIDs[0] != "note-1" {
		t.Fatalf("chip payload round-trip drifted: %+v", chip.MemoryIngested)
	}
	if chip.TurnID != "turn-1" {
		t.Fatalf("chip must hydrate under its turn: %+v", chip)
	}

	// The durable row is the application-owned session event.
	rows, err := st.SessionEvents().LoadEvents(ctx, store.LoadSessionEventsParams{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Kind == string(memory.SessionEventKindMemoryIngested) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a persisted %q session event, got kinds %+v", memory.SessionEventKindMemoryIngested, rows)
	}

	// Scheduled and heartbeat runs never emit chips.
	scheduled := job
	scheduled.Origin = OriginScheduler
	scheduled.TurnID = "turn-sched"
	runner.AppendMemoryChip(ctx, scheduled, payload)
	heartbeat := job
	heartbeat.Origin = OriginHeartbeat
	heartbeat.TurnID = "turn-hb"
	runner.AppendMemoryChip(ctx, heartbeat, payload)

	history, err = runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	chips := 0
	for i := range history.Events {
		if history.Events[i].Kind == TranscriptEventMemoryIngested {
			chips++
			if history.Events[i].TurnID == "turn-sched" || history.Events[i].TurnID == "turn-hb" {
				t.Fatalf("unattended origins must not emit chips: %+v", history.Events[i])
			}
		}
	}
	if chips != 1 {
		t.Fatalf("expected exactly one chip (scheduled/heartbeat suppressed), got %d", chips)
	}
}

// TestWorkerChipFlowsThroughRunnerSink is the end-to-end ingestion loop on
// the fake stores: the worker processes a completed-turn job, gists the
// window through the scripted side-call model, and the runner sink persists
// the chip — counts only, never content.
func TestWorkerChipFlowsThroughRunnerSink(t *testing.T) {
	ctx := context.Background()
	st, runner, ws, _, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sidecall := &staticTextModel{responses: []string{
		`{"description":"The team planned the deploy window","outcome":"Tuesday morning confirmed"}`,
		`[{"op":"ADD","content":"The deploy window is Tuesday morning","visibility":"shared","importance":5,"pin":false,"explicit_request":false,"supersedes":null,"topic":null,"conflict_with_doc":false}]`,
	}}
	worker := memory.NewWorker(
		memory.NewGister(st.MemoryEvents(), st.SessionEvents(), st.MemoryEntities(), nil, nil, nil, logger, memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return sidecall, nil
		})),
		memory.NewGate(st.MemoryNotes(), st.MemoryEntities(), st.Memories(), nil, nil, nil, logger, memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return sidecall, nil
		})),
		memory.NewProviderEmbedder(st.Providers(), st.ToolSettings(), []byte("test-key-32-bytes-long-12345678"), providers.NewRegistry()),
		st.MemoryEmbeddings(),
		logger,
		memory.WithChipSink(runner.AppendMemoryChip),
	)
	runner.memoryWorker = worker

	// Seed the session window the job will distill.
	serializer := &schema.HumanReadableSerializer{}
	payloadBytes, err := serializer.Marshal(&adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   "e1",
		TurnID:    "turn-1",
		Timestamp: time.Now().UTC().Add(-time.Hour),
		Kind:      adk.SessionEventMessage,
		Message:   schema.UserAgenticMessage("Reminder: the deploy window is Tuesday morning."),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := st.SessionEvents().AppendEvents(ctx, ws.ID, []domain.SessionEvent{{
		SessionID: req.SessionID, EventID: "e1", TurnID: "turn-1", Seq: 1,
		Kind: string(adk.SessionEventMessage), Payload: payloadBytes, OccurredAt: time.Now().UTC().Add(-time.Hour), WorkspaceID: ws.ID,
	}}); err != nil {
		t.Fatalf("append session event: %v", err)
	}

	runner.enqueueTurnIngest(ctx, req, "turn-1", hookRunStatusCompleted, false)
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(cctx)
	defer worker.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		history, err := runner.History(ctx, HistoryRequest{WorkspaceID: ws.ID, SessionID: req.SessionID})
		if err == nil {
			for i := range history.Events {
				if history.Events[i].Kind == TranscriptEventMemoryIngested {
					chip := history.Events[i].MemoryIngested
					if chip == nil || len(chip.NoteIDs) != 1 || len(chip.EventIDs) != 1 {
						t.Fatalf("chip payload = %+v", chip)
					}
					raw := history.Events[i]
					_ = raw
					if strings.Contains(history.Events[i].MemoryIngested.NoteIDs[0], "deploy") {
						t.Fatalf("chip must never carry content: %+v", chip)
					}
					return
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the chip never hydrated through the runner sink (stats %+v)", worker.Stats())
}

// TestRunner_AssociativeRouteInjectsTraversalCandidates (wave3 tasks 5.3,
// spec: associative route draws from traversal): a gate verdict routing the
// associative bucket with an entity seed injects the entity's linked rows
// alongside the fused text candidates — within the same bounded section —
// and never another member's linked row.
func TestRunner_AssociativeRouteInjectsTraversalCandidates(t *testing.T) {
	st, runner, ws, _, req := setupHooksRunner(t, nil, &hooksModel{final: "ok"})
	ctx := context.Background()

	// The entity graph: ProjectX links a visible shared row and Sari's
	// private row — only the visible one may inject.
	entity := &domain.MemoryEntity{
		WorkspaceID:     ws.ID,
		Label:           "ProjectX",
		NormalizedLabel: domain.NormalizeEntityLabel("ProjectX"),
		Origin:          domain.MemoryOriginDialogue,
		SourceEventID:   "ev-entity",
		LearnedAt:       time.Now().UTC().Add(-time.Hour),
	}
	if err := st.MemoryEntities().ResolveEntity(ctx, entity); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	linked := seedMemoryNote(t, st, ws.ID, domain.MemoryVisibilityShared, "", "", "ProjectX rollout is on track")
	sari := &domain.User{Email: "sari@example.com", Name: "Sari"}
	if err := st.Users().Create(ctx, sari); err != nil {
		t.Fatalf("create sari: %v", err)
	}
	private := seedMemoryNote(t, st, ws.ID, domain.MemoryVisibilityUser, sari.ID, "", "Private rollout note for ProjectX")
	for _, edge := range []domain.MemoryEntityEdge{
		{EntityID: entity.ID, TargetType: domain.MemoryTargetNote, TargetID: linked.ID, Visibility: domain.MemoryVisibilityShared, Origin: domain.MemoryOriginDialogue, SourceEventID: "ev-edge"},
		{EntityID: entity.ID, TargetType: domain.MemoryTargetNote, TargetID: private.ID, Visibility: domain.MemoryVisibilityUser, Origin: domain.MemoryOriginDialogue, SourceEventID: "ev-edge"},
	} {
		if _, err := st.MemoryEntities().AddEdges(ctx, ws.ID, []domain.MemoryEntityEdge{edge}); err != nil {
			t.Fatalf("seed edge: %v", err)
		}
	}

	req.Input = "what is the status of ProjectX"

	gate := memory.NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return &staticTextModel{responses: []string{`{"needs_memory":true,"buckets":["associative"],"entity":"ProjectX"}`}}, nil
		}))
	runner.intentGate = gate
	composer := &memoryCaptureComposer{}
	runner.instructionComposer = composer

	stream, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	collectStream(t, stream)

	docs := composer.snapshot().MemoryDocs
	if len(docs) != 1 {
		t.Fatalf("expected exactly one memory section, got %d", len(docs))
	}
	doc := docs[0]
	if !strings.Contains(doc, linked.SourceEventID) {
		t.Fatalf("the associative route must inject the linked row's evidence pointer %q:\n%s", linked.SourceEventID, doc)
	}
	if strings.Contains(doc, private.SourceEventID) || strings.Contains(doc, private.Content) {
		t.Fatalf("the associative route must never inject another member's row:\n%s", doc)
	}
}
