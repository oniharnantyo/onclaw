package middlewares_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
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
