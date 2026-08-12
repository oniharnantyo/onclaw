package middlewares_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/membus"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

type mockCoreStore struct {
	ReadVal  string
	ReadErr  error
	WriteVal string
	WriteErr error
}

func (m *mockCoreStore) ReadCore(ctx context.Context, workspace string) (string, error) {
	return m.ReadVal, m.ReadErr
}

func (m *mockCoreStore) WriteCore(ctx context.Context, workspace string, op, target, content string) (string, error) {
	return m.WriteVal, m.WriteErr
}

type mockMemoryStore struct {
	Docs   []*memory.MemoryDocument
	Embeds map[string][]float32
}

func (m *mockMemoryStore) IndexDocument(ctx context.Context, doc *memory.MemoryDocument, vector []float32) (int64, error) {
	m.Docs = append(m.Docs, doc)
	return int64(len(m.Docs)), nil
}

func (m *mockMemoryStore) UpdateDocument(ctx context.Context, id int64, content string, vector []float32) error {
	return nil
}

func (m *mockMemoryStore) SearchArchive(ctx context.Context, query *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
	return nil, nil
}

func (m *mockMemoryStore) GetDocument(ctx context.Context, id int64) (*memory.MemoryDocument, error) {
	return nil, nil
}

func (m *mockMemoryStore) DeleteDocument(ctx context.Context, id int64) error {
	return nil
}

func (m *mockMemoryStore) GetCachedEmbedding(ctx context.Context, embeddingModel string, hash string) ([]float32, error) {
	return m.Embeds[hash], nil
}

func (m *mockMemoryStore) PutCachedEmbedding(ctx context.Context, embeddingModel string, hash string, vec []float32) error {
	if m.Embeds == nil {
		m.Embeds = make(map[string][]float32)
	}
	m.Embeds[hash] = vec
	return nil
}

type mockKVStore struct {
	Store map[string]string
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	return m.Store[key], nil
}

func (m *mockKVStore) Set(ctx context.Context, key, val string) error {
	if m.Store == nil {
		m.Store = make(map[string]string)
	}
	m.Store[key] = val
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	delete(m.Store, key)
	return nil
}

func getMsgText(msg *schema.AgenticMessage) string {
	if msg == nil || len(msg.ContentBlocks) == 0 {
		return ""
	}
	block := msg.ContentBlocks[0]
	if block.UserInputText != nil {
		return block.UserInputText.Text
	}
	if block.AssistantGenText != nil {
		return block.AssistantGenText.Text
	}
	return ""
}

func TestMemoryMiddleware_BeforeAgent(t *testing.T) {
	ctx := context.Background()
	coreStore := &mockCoreStore{ReadVal: "Curated memory line 1"}
	middleware := middlewares.NewMemoryMiddleware(
		coreStore, nil, nil, nil, nil, nil, "workspace", "agent-1", 123, 100, nil, nil, 0, nil,
	)

	// Turn 1
	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{
				schema.UserAgenticMessage("hello"),
			},
		},
	}

	_, newCtx, err := middleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	if len(newCtx.AgentInput.Messages) != 2 {
		t.Errorf("expected 2 messages after injection, got %d", len(newCtx.AgentInput.Messages))
	}
	txt := getMsgText(newCtx.AgentInput.Messages[0])
	if txt != "## CURATED LONG-TERM MEMORY\n\nCurated memory line 1" {
		t.Errorf("unexpected injected message: %q", txt)
	}

	// Turn 2: change coreStore ReadVal. It should NOT be re-read since it's frozen for the session!
	coreStore.ReadVal = "Updated curated memory line 1"
	runCtx2 := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{
				schema.UserAgenticMessage("hello again"),
			},
		},
	}

	_, newCtx2, err := middleware.BeforeAgent(ctx, runCtx2)
	if err != nil {
		t.Fatalf("BeforeAgent Turn 2 failed: %v", err)
	}

	txt2 := getMsgText(newCtx2.AgentInput.Messages[0])
	if txt2 != "## CURATED LONG-TERM MEMORY\n\nCurated memory line 1" {
		t.Errorf("expected frozen memory 'Curated memory line 1', got %q", txt2)
	}
}

// TestMemoryMiddleware_BeforeAgent_NilCoreStore reproduces the panic in
// BeforeAgent when the middleware is built with a nil CoreStore (curated core
// memory disabled) while memory/extraction remains enabled. Previously this
// dereferenced the nil CoreStore and crashed the request.
func TestMemoryMiddleware_BeforeAgent_NilCoreStore(t *testing.T) {
	ctx := context.Background()
	middleware := middlewares.NewMemoryMiddleware(
		nil, nil, nil, nil, nil, nil, "workspace", "agent-1", 123, 100, nil, nil, 0, nil,
	)

	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{
				schema.UserAgenticMessage("hello"),
			},
		},
	}

	_, newCtx, err := middleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	// No core memory should be injected when CoreStore is nil.
	if len(newCtx.AgentInput.Messages) != 1 {
		t.Errorf("expected 1 message (no injection), got %d", len(newCtx.AgentInput.Messages))
	}
	if getMsgText(newCtx.AgentInput.Messages[0]) != "hello" {
		t.Errorf("unexpected message mutation: %q", getMsgText(newCtx.AgentInput.Messages[0]))
	}
}

func TestMemoryMiddleware_FlushMessages_EventStop(t *testing.T) {
	ctx := context.Background()
	memoryStore := &mockMemoryStore{}
	kvStore := &mockKVStore{}

	middleware := middlewares.NewMemoryMiddleware(
		&mockCoreStore{}, memoryStore, nil, kvStore, nil, nil, "workspace", "agent-1", 123, 100, nil, nil, 0, nil,
	)

	// Turn message sequence with triggers for extractive fallback.
	msg := schema.UserAgenticMessage("Remember that the user always prefers tabs over spaces.")
	msg.Extra = map[string]interface{}{
		"_onclaw_seq": int64(1),
	}

	// FlushMessages is the EventStop path (short sessions that never compact).
	// Empty compactionSummary → fresh LLM call (or extractive fallback when no model).
	middleware.FlushMessages(ctx, []*schema.AgenticMessage{msg}, "")

	// Extractive fallback should have triggered on "prefers" and "remember".
	if len(memoryStore.Docs) != 1 {
		t.Errorf("expected 1 episodic memory extracted, got %d", len(memoryStore.Docs))
	} else {
		if memoryStore.Docs[0].Content != "Remember that the user always prefers tabs over spaces." {
			t.Errorf("unexpected extracted content: %q", memoryStore.Docs[0].Content)
		}
	}

	// Verify cursor was updated in kvStore.
	val, err := kvStore.Get(ctx, "memory_cursor:123")
	if err != nil || val != "1" {
		t.Errorf("expected memory_cursor:123 to be 1, got error=%v, val=%q", err, val)
	}

	// Running again with same sequence should skip extraction (cursor idempotency).
	memoryStore.Docs = nil
	middleware.FlushMessages(ctx, []*schema.AgenticMessage{msg}, "")
	if len(memoryStore.Docs) != 0 {
		t.Errorf("expected 0 new memories (skipped by cursor), got %d", len(memoryStore.Docs))
	}
}

func TestMemoryMiddleware_AfterAgent_IsNoOp(t *testing.T) {
	ctx := context.Background()
	memoryStore := &mockMemoryStore{}
	kvStore := &mockKVStore{}

	middleware := middlewares.NewMemoryMiddleware(
		&mockCoreStore{}, memoryStore, nil, kvStore, nil, nil, "workspace", "agent-1", 123, 100, nil, nil, 0, nil,
	)

	msg := schema.UserAgenticMessage("Always prefer tabs.")
	msg.Extra = map[string]interface{}{"_onclaw_seq": int64(1)}

	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{msg},
	}

	_, err := middleware.AfterAgent(ctx, state)
	if err != nil {
		t.Fatalf("AfterAgent failed: %v", err)
	}

	// D3: AfterAgent must be a no-op — no documents extracted per turn.
	if len(memoryStore.Docs) != 0 {
		t.Errorf("D3 violation: AfterAgent should not extract; got %d docs", len(memoryStore.Docs))
	}
}

func TestMemoryMiddleware_BeforeAgent_RetrievalDisabled(t *testing.T) {
	ctx := context.Background()
	coreStore := &mockCoreStore{ReadVal: "Curated memory line 1"}
	middleware := middlewares.NewMemoryMiddleware(
		coreStore, nil, nil, nil, nil, nil, "workspace", "agent-1", 123, 100, nil, nil, 0, nil,
	)
	middleware.RetrievalEnabled = false

	runCtx := &adk.ChatModelAgentContext[*schema.AgenticMessage]{
		AgentInput: &adk.TypedAgentInput[*schema.AgenticMessage]{
			Messages: []*schema.AgenticMessage{
				schema.UserAgenticMessage("hello"),
			},
		},
	}

	_, newCtx, err := middleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		t.Fatalf("BeforeAgent failed: %v", err)
	}

	// Turn-start auto-injection must be skipped when RetrievalEnabled is false
	if len(newCtx.AgentInput.Messages) != 1 {
		t.Errorf("expected 1 message (auto-injection skipped), got %d", len(newCtx.AgentInput.Messages))
	}
}


// --- Integration tests for CompactionSummary wiring and Bus ---

type mockEpisodicStore struct {
	appended []episodicRow
}

type episodicRow struct {
	Agent      string
	Summary    string
	L0Abstract string
	KeyTopics  string
	SourceID   string
	ExpiresAt  string
}

func (m *mockEpisodicStore) AppendEpisodic(ctx context.Context, agent, summary, l0Abstract, keyTopics, sourceID, expiresAt string) (int64, error) {
	m.appended = append(m.appended, episodicRow{
		Agent:      agent,
		Summary:    summary,
		L0Abstract: l0Abstract,
		KeyTopics:  keyTopics,
		SourceID:   sourceID,
		ExpiresAt:  expiresAt,
	})
	return int64(len(m.appended)), nil
}

func (m *mockEpisodicStore) ListUnpromoted(ctx context.Context, agent string) ([]*memory.EpisodicSummary, error) {
	return nil, nil
}

func (m *mockEpisodicStore) CountUnpromoted(ctx context.Context, agent string) (int, error) {
	return 0, nil
}

func (m *mockEpisodicStore) MarkPromoted(ctx context.Context, id int64) error { return nil }

func (m *mockEpisodicStore) PruneExpired(ctx context.Context) (int64, error) { return 0, nil }

func (m *mockEpisodicStore) GetEpisodic(ctx context.Context, id int64) (*memory.EpisodicSummary, error) {
	return nil, nil
}

type mockChatModel struct {
	calls    int
	response string
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.calls++
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: m.response}),
		},
	}, nil
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, nil
}

func TestFlushMessages_CompactionSummary_SkipsLLMCall(t *testing.T) {
	ctx := context.Background()
	episodicStore := &mockEpisodicStore{}
	chatModel := &mockChatModel{response: "LLM-generated summary"}
	kvStore := &mockKVStore{}

	mw := middlewares.NewMemoryMiddleware(
		nil,           // coreStore
		&mockMemoryStore{}, // memoryStore (non-nil to pass nil check)
		nil,           // embedder
		kvStore,
		chatModel,
		nil, // reviewModel
		"workspace", "agent-1", 123, 100,
		episodicStore,
		nil, // dreamer
		90,
		nil, // kgStore
	)
	mw.ExtractionEnabled = false // skip ExtractAndFlush to isolate episodic path

	// Call with a non-empty compaction summary — should reuse it instead of calling LLM.
	msgs := []*schema.AgenticMessage{
		schema.UserAgenticMessage("Remember that I prefer dark mode for all editors."),
	}
	mw.FlushMessages(ctx, msgs, "Compacted: user discussed editor preferences and dark mode setup.")

	// Verify episodic row was written with the compaction summary, not an LLM-generated one.
	if len(episodicStore.appended) != 1 {
		t.Fatalf("expected 1 episodic row, got %d", len(episodicStore.appended))
	}
	row := episodicStore.appended[0]
	if row.Summary != "Compacted: user discussed editor preferences and dark mode setup." {
		t.Errorf("expected compaction summary to be reused, got %q", row.Summary)
	}

	// The chat model should NOT have been called (0 calls since compaction summary was reused).
	if chatModel.calls != 0 {
		t.Errorf("expected 0 LLM calls (compaction summary reused), got %d", chatModel.calls)
	}
}

func TestFlushMessages_CompactionSummary_NonEmpty(t *testing.T) {
	ctx := context.Background()
	episodicStore := &mockEpisodicStore{}
	kvStore := &mockKVStore{}

	mw := middlewares.NewMemoryMiddleware(
		nil,
		&mockMemoryStore{},
		nil,
		kvStore,
		nil, // no chatModel — compaction summary should be enough
		nil,
		"workspace", "agent-1", 456, 100,
		episodicStore,
		nil,
		90,
		nil,
	)
	mw.ExtractionEnabled = false

	// Assign CompactionSummary (simulating what the Finalize callback does).
	mw.CompactionSummary = "The user refactored the authentication module."

	msgs := []*schema.AgenticMessage{
		schema.UserAgenticMessage("Let's refactor the auth module to use JWT tokens exclusively."),
	}
	mw.FlushMessages(ctx, msgs, mw.CompactionSummary)

	if len(episodicStore.appended) != 1 {
		t.Fatalf("expected 1 episodic row, got %d", len(episodicStore.appended))
	}
	if episodicStore.appended[0].Summary != "The user refactored the authentication module." {
		t.Errorf("expected CompactionSummary to be stored, got %q", episodicStore.appended[0].Summary)
	}
}

func TestFlushMessages_BusReceivesEpisodeCreated(t *testing.T) {
	ctx := context.Background()
	episodicStore := &mockEpisodicStore{}
	kvStore := &mockKVStore{}

	mw := middlewares.NewMemoryMiddleware(
		nil,
		&mockMemoryStore{},
		nil,
		kvStore,
		nil, // no chatModel — compaction summary is enough
		nil,
		"workspace", "agent-1", 789, 100,
		episodicStore,
		nil,
		90,
		nil,
	)
	mw.ExtractionEnabled = false

	// Create a bus with a counting worker to verify event delivery.
	bus := membus.New(64)
	worker := &episodeCountingWorker{}
	bus.Register(worker)
	bus.Start(ctx)

	mw.Bus = bus

	msgs := []*schema.AgenticMessage{
		schema.UserAgenticMessage("Remember that I always use Go modules for dependency management."),
	}
	mw.FlushMessages(ctx, msgs, "Compacted: user configured Go modules.")

	// Stop bus to drain events.
	bus.Stop()

	if worker.count != 1 {
		t.Errorf("expected 1 EpisodeCreated event on bus, got %d", worker.count)
	}
	if worker.lastEvent.AgentName != "agent-1" {
		t.Errorf("expected AgentName 'agent-1', got %q", worker.lastEvent.AgentName)
	}
	if worker.lastEvent.EpisodeID != 1 {
		t.Errorf("expected EpisodeID 1, got %d", worker.lastEvent.EpisodeID)
	}
	if worker.lastEvent.Summary != "Compacted: user configured Go modules." {
		t.Errorf("expected summary to match, got %q", worker.lastEvent.Summary)
	}
}

// episodeCountingWorker counts EpisodeCreated events received via the bus.
type episodeCountingWorker struct {
	count     int
	lastEvent membus.EpisodeCreated
}

func (w *episodeCountingWorker) Subscribes() []string {
	return []string{"episode_created"}
}

func (w *episodeCountingWorker) Handle(ctx context.Context, event membus.Event) error {
	ep, ok := event.(membus.EpisodeCreated)
	if !ok {
		return nil
	}
	w.count++
	w.lastEvent = ep
	return nil
}
