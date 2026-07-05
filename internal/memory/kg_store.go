package memory

import "context"

// KGStore defines the contract for knowledge graph storage and retrieval.
type KGStore interface {
	// IngestExtraction ingests entities and relations from an episodic summary.
	// Sets valid_from = now. If a contradictory relation exists, it supersedes
	// the old one (sets valid_until = now) and inserts a new row.
	IngestExtraction(ctx context.Context, ext *Extraction) error

	// DedupAfterExtraction merges semantically-equivalent entities
	// (same type + normalized name) and re-points their relations.
	// Different-type same-name entities are left unmerged (future enhancement: ambiguous flagging on review surface).
	DedupAfterExtraction(ctx context.Context, agentID string) error

	// SearchGraph traverses the knowledge graph via recursive CTE.
	// Returns entities connected to the seed within max_depth hops.
	// Scoped to the agent. Returns paths of relations.
	SearchGraph(ctx context.Context, query *KGQuery) ([]KGHit, error)
}
