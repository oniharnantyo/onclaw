package memory

// MemoryDocument represents a document in the searchable archive.
type MemoryDocument struct {
	ID             int64  `json:"id"`
	Agent          string `json:"agent"`
	Scope          string `json:"scope"`
	Kind           string `json:"kind"` // "episodic", "curated", etc.
	Content        string `json:"content"`
	Source         string `json:"source"` // e.g. "conversation_123"
	EmbeddingModel string `json:"embedding_model"`
	CreatedAt      string `json:"created_at"`
}

// MemoryHit represents a query result with a hybrid score.
type MemoryHit struct {
	Document *MemoryDocument `json:"document"`
	Score    float64         `json:"score"`
}

// CoreEntry represents a structured or raw line entry in MEMORY.md.
type CoreEntry struct {
	Content string `json:"content"`
}

// ArchiveQuery represents options for searching the archive.
type ArchiveQuery struct {
	Query          string    `json:"query"`
	Agent          string    `json:"agent"`
	Scope          string    `json:"scope"` // "global" or agent name
	EmbeddingModel string    `json:"embedding_model"`
	Vector         []float32 `json:"vector,omitempty"`
	Limit          int       `json:"limit"`
	FtsWeight      float64   `json:"fts_weight,omitempty"`    // 0.0 means use default (0.3)
	VectorWeight   float64   `json:"vector_weight,omitempty"` // 0.0 means use default (0.7)
}

// Candidate represents a raw candidate retrieved from the database.
type Candidate struct {
	Document *MemoryDocument
	Vector   []float32
	FTSRank  float64
}

// EpisodicSummary represents an episodic memory row — a summary of a completed session.
type EpisodicSummary struct {
	ID         int64   `json:"id"`
	Agent      string  `json:"agent"`
	Summary    string  `json:"summary"`
	L0Abstract string  `json:"l0_abstract"`
	KeyTopics  string  `json:"key_topics"`
	SourceID   string  `json:"source_id"`
	PromotedAt *string `json:"promoted_at,omitempty"`
	ExpiresAt  string  `json:"expires_at"`
	CreatedAt  string  `json:"created_at"`
}

// DreamSweepRecord represents the output of a single dreaming sweep.
type DreamSweepRecord struct {
	Timestamp     string   `json:"timestamp"`
	Agent         string   `json:"agent"`
	EpisodesCount int      `json:"episodes_count"`
	Promotions    []string `json:"promotions"`
	Supersessions []string `json:"supersessions,omitempty"`
	Score         float64  `json:"score,omitempty"`
	ReviewModel   string   `json:"review_model"`
}

// AgentMemoryConfig defines the per-agent overrides for the memory system.
type AgentMemoryConfig struct {
	CuratedEnabled      *bool  `json:"curated_enabled,omitempty"`
	EpisodicEnabled     *bool  `json:"episodic_enabled,omitempty"`
	KGEnabled           *bool  `json:"kg_enabled,omitempty"`
	EmbeddingProvider   string `json:"embedding_provider,omitempty"`
	EmbeddingModel      string `json:"embedding_model,omitempty"`
	SecurityScanEnabled *bool  `json:"security_scan_enabled,omitempty"`
	ExtractionEnabled   *bool  `json:"extraction_enabled,omitempty"`
	RetrievalEnabled    *bool  `json:"retrieval_enabled,omitempty"`
	DreamingEnabled     *bool  `json:"dreaming_enabled,omitempty"`
	StagedWriteApproval *bool  `json:"staged_write_approval,omitempty"`
}

// ResolvedMemoryConfig holds the fully resolved memory configuration after merging agent-specific overrides with global settings.
type ResolvedMemoryConfig struct {
	Enabled             bool
	CuratedEnabled      bool
	EpisodicEnabled     bool
	KGEnabled           bool
	EmbeddingProvider   string
	EmbeddingModel      string
	SecurityScanEnabled bool
	ExtractionEnabled   bool
	RetrievalEnabled    bool
	DreamingEnabled     bool
	StagedWriteApproval bool
}

// Resolve merges the agent overrides with global settings.
// For booleans, if the agent override is nil, it falls back to the global setting.
// For strings, if the agent override is empty, it falls back to the global setting.
func (c *AgentMemoryConfig) Resolve(
	globalEnabled bool,
	globalCurated bool,
	globalEpisodic bool,
	globalKG bool,
	globalEmbedProvider string,
	globalEmbedModel string,
	globalSecurityScan bool,
	globalExtraction bool,
	globalRetrieval bool,
	globalDreaming bool,
	globalStagedWriteApproval bool,
) *ResolvedMemoryConfig {
	res := &ResolvedMemoryConfig{
		Enabled:             globalEnabled,
		CuratedEnabled:      globalCurated,
		EpisodicEnabled:     globalEpisodic,
		KGEnabled:           globalKG,
		EmbeddingProvider:   globalEmbedProvider,
		EmbeddingModel:      globalEmbedModel,
		SecurityScanEnabled: globalSecurityScan,
		ExtractionEnabled:   globalExtraction,
		RetrievalEnabled:    globalRetrieval,
		DreamingEnabled:     globalDreaming,
		StagedWriteApproval: globalStagedWriteApproval,
	}

	if c.CuratedEnabled != nil {
		res.CuratedEnabled = *c.CuratedEnabled
	}
	if c.EpisodicEnabled != nil {
		res.EpisodicEnabled = *c.EpisodicEnabled
	}
	if c.KGEnabled != nil {
		res.KGEnabled = *c.KGEnabled
	}
	if c.EmbeddingProvider != "" {
		res.EmbeddingProvider = c.EmbeddingProvider
	}
	if c.EmbeddingModel != "" {
		res.EmbeddingModel = c.EmbeddingModel
	}
	if c.SecurityScanEnabled != nil {
		res.SecurityScanEnabled = *c.SecurityScanEnabled
	}
	if c.ExtractionEnabled != nil {
		res.ExtractionEnabled = *c.ExtractionEnabled
	}
	if c.RetrievalEnabled != nil {
		res.RetrievalEnabled = *c.RetrievalEnabled
	}
	if c.DreamingEnabled != nil {
		res.DreamingEnabled = *c.DreamingEnabled
	}
	if c.StagedWriteApproval != nil {
		res.StagedWriteApproval = *c.StagedWriteApproval
	}

	return res
}
