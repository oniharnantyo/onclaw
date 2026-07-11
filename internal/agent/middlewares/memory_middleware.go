package middlewares

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// MemoryMiddleware handles curated core memory injection and episodic memory extraction.
type MemoryMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	CoreStore        memory.CoreStore
	MemoryStore      memory.MemoryStore
	Embedder         *memory.Embedder
	KVStore          store.KVStore
	ChatModel        model.AgenticModel
	ReviewModel      model.AgenticModel
	Workspace        string
	AgentName        string
	ConversationID   int64
	CharLimit        int
	SkipSecurityScan bool
	ExtractionEnabled bool

	EpisodicStore   memory.EpisodicStore
	Dreamer         *memory.Dreamer
	EpisodicTTLDays int
	// KGStore is the knowledge graph store for entity extraction.
	KGStore memory.KGStore
	// CompactionSummary holds the text of the most recent conversation compaction
	// summary so that episodic summarization can reuse it instead of a second LLM call.
	CompactionSummary string

	mu         sync.Mutex
	frozenCore string
	loaded     bool
}

// NewMemoryMiddleware constructs a new MemoryMiddleware.
func NewMemoryMiddleware(
	coreStore memory.CoreStore,
	memoryStore memory.MemoryStore,
	embedder *memory.Embedder,
	kvStore store.KVStore,
	chatModel model.AgenticModel,
	reviewModel model.AgenticModel,
	workspace string,
	agentName string,
	conversationID int64,
	charLimit int,
	episodicStore memory.EpisodicStore,
	dreamer *memory.Dreamer,
	episodicTTLDays int,
	kgStore memory.KGStore,
) *MemoryMiddleware {
	if episodicTTLDays <= 0 {
		episodicTTLDays = 90
	}
	return &MemoryMiddleware{
		CoreStore:       coreStore,
		MemoryStore:     memoryStore,
		Embedder:        embedder,
		KVStore:         kvStore,
		ChatModel:       chatModel,
		ReviewModel:     reviewModel,
		Workspace:       workspace,
		AgentName:       agentName,
		ConversationID:  conversationID,
		CharLimit:       charLimit,
		EpisodicStore:   episodicStore,
		Dreamer:         dreamer,
		EpisodicTTLDays: episodicTTLDays,
		KGStore:         kgStore,
		ExtractionEnabled: true, // Default to true, overridden in AssembleAgent
	}
}

// BeforeAgent injects the curated memory core once per session.
func (m *MemoryMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.AgenticMessage]) (context.Context, *adk.ChatModelAgentContext[*schema.AgenticMessage], error) {
	m.mu.Lock()
	if !m.loaded {
		core, err := m.CoreStore.ReadCore(ctx, m.Workspace)
		if err == nil {
			if len(core) > m.CharLimit {
				core = fmt.Sprintf("WARNING: Curated core memory was truncated because it exceeded the character cap of %d.\n\n%s", m.CharLimit, core[:m.CharLimit])
			}
			m.frozenCore = core
		}
		m.loaded = true
	}
	frozen := m.frozenCore
	m.mu.Unlock()

	if frozen != "" {
		memMsg := schema.SystemAgenticMessage(fmt.Sprintf("## CURATED LONG-TERM MEMORY\n\n%s", frozen))
		runCtx.AgentInput.Messages = append([]*schema.AgenticMessage{memMsg}, runCtx.AgentInput.Messages...)
	}

	return ctx, runCtx, nil
}

// AfterAgent is a no-op; memory extraction fires either via the compaction callback
// (flush-before-compaction, D3 primary path) or via EventStop (short-session path).
func (m *MemoryMiddleware) AfterAgent(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage]) (context.Context, error) {
	return ctx, nil
}

// FlushMessages runs ExtractAndFlush and episodic summarization against the provided messages.
// compactionSummary, when non-empty, is reused to avoid a second LLM call for sessions that
// already compacted. Pass "" to force a fresh LLM summarization.
func (m *MemoryMiddleware) FlushMessages(ctx context.Context, messages []*schema.AgenticMessage, compactionSummary string) {
	if m.MemoryStore == nil {
		return
	}
	if m.ExtractionEnabled {
		_ = memory.ExtractAndFlush(ctx, m.ChatModel, m.MemoryStore, m.Embedder, m.KVStore, m.AgentName, m.ConversationID, messages, m.SkipSecurityScan)
	}

	if m.EpisodicStore != nil && len(messages) > 0 {
		summary, l0Abstract, keyTopics, err := memory.SummarizeSession(ctx, m.ChatModel, compactionSummary, messages)
		if err == nil && summary != "" {
			sourceID := fmt.Sprintf("conversation_%d", m.ConversationID)
			expiresAt := memory.ComputeEpisodicTTL(m.EpisodicTTLDays)
			episodeID, err := m.EpisodicStore.AppendEpisodic(ctx, m.AgentName, summary, l0Abstract, keyTopics, sourceID, expiresAt)

			// Trigger knowledge graph extraction after episodic write
			if err == nil && m.KGStore != nil && m.ChatModel != nil {
				m.extractAndIngestEntities(ctx, summary, episodeID)
			}
		}
	}

	if m.Dreamer != nil {
		_ = m.Dreamer.MaybeDream(ctx)
	}
}

// extractAndIngestEntities extracts entities from the summary and ingests them into the knowledge graph.
// Uses ReviewModel if set, falling back to ChatModel.
func (m *MemoryMiddleware) extractAndIngestEntities(ctx context.Context, summary string, episodeID int64) {
	sourceID := fmt.Sprintf("episodic_%d", episodeID)

	extractionModel := m.ChatModel
	if m.ReviewModel != nil {
		extractionModel = m.ReviewModel
	}

	ext, err := memory.ExtractEntitiesWithSecurity(ctx, extractionModel, summary, m.AgentName, sourceID, m.SkipSecurityScan)
	if err != nil {
		// Security threat or extraction failure - log and skip ingest
		return
	}

	if len(ext.Entities) == 0 && len(ext.Relations) == 0 {
		return
	}

	// Ingest extraction into knowledge graph
	_ = m.KGStore.IngestExtraction(ctx, ext)

	// Trigger deduplication after extraction
	_ = m.KGStore.DedupAfterExtraction(ctx, m.AgentName)
}
