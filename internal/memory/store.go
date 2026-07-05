package memory

import "context"

// MemoryStore defines operations for persisting and retrieving documents in the archive.
type MemoryStore interface {
	IndexDocument(ctx context.Context, doc *MemoryDocument, vector []float32) (int64, error)
	SearchArchive(ctx context.Context, query *ArchiveQuery) ([]*MemoryHit, error)
	GetDocument(ctx context.Context, id int64) (*MemoryDocument, error)
	DeleteDocument(ctx context.Context, id int64) error

	GetCachedEmbedding(ctx context.Context, contentHash string) ([]float32, error)
	PutCachedEmbedding(ctx context.Context, contentHash string, vector []float32) error
}

// CoreStore defines operations for managing the curated memory core file (MEMORY.md).
type CoreStore interface {
	ReadCore(ctx context.Context, workspace string) (string, error)
	WriteCore(ctx context.Context, workspace string, op string, target string, content string) (string, error)
}

// StagedWriteStore defines operations for managing staged memory writes pending approval.
type StagedWriteStore interface {
	StageWrite(ctx context.Context, agent string, op string, target string, content string) (int64, error)
	ListStaged(ctx context.Context, agent string) ([]*StagedWrite, error)
	GetStagedWrite(ctx context.Context, id int64) (*StagedWrite, error)
	ApproveWrite(ctx context.Context, id int64) error
	RejectWrite(ctx context.Context, id int64) error
}

// EpisodicStore defines operations for managing episodic session summaries.
type EpisodicStore interface {
	AppendEpisodic(ctx context.Context, agent string, summary string, l0Abstract string, keyTopics string, sourceID string, expiresAt string) (int64, error)
	ListUnpromoted(ctx context.Context, agent string) ([]*EpisodicSummary, error)
	CountUnpromoted(ctx context.Context, agent string) (int, error)
	MarkPromoted(ctx context.Context, id int64) error
	PruneExpired(ctx context.Context) (int64, error)
	GetEpisodic(ctx context.Context, id int64) (*EpisodicSummary, error)
}

// StagedWrite represents a staged memory write awaiting approval.
type StagedWrite struct {
	ID        int64  `json:"id"`
	Agent     string `json:"agent"`
	Operation string `json:"operation"` // "add", "replace", "remove"
	Target    string `json:"target,omitempty"`
	Content   string `json:"content,omitempty"`
	Status    string `json:"status"` // "pending", "approved", "rejected"
	CreatedAt string `json:"created_at"`
}
