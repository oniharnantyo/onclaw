package memory

// MemoryDocument represents a document in the searchable archive.
type MemoryDocument struct {
	ID        int64  `json:"id"`
	Agent     string `json:"agent"`
	Scope     string `json:"scope"`
	Kind      string `json:"kind"` // "episodic", "curated", etc.
	Content   string `json:"content"`
	Source    string `json:"source"` // e.g. "conversation_123"
	CreatedAt string `json:"created_at"`
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
	Query        string    `json:"query"`
	Agent        string    `json:"agent"`
	Scope        string    `json:"scope"` // "global" or agent name
	Vector       []float32 `json:"vector,omitempty"`
	Limit        int       `json:"limit"`
	FtsWeight    float64   `json:"fts_weight,omitempty"`    // 0.0 means use default (0.3)
	VectorWeight float64   `json:"vector_weight,omitempty"` // 0.0 means use default (0.7)
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
