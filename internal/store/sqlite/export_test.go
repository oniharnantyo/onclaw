package sqlite

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// VectorScanCandidatesFromStore is a test-only re-export of the vector recall scan,
// reached through the public MemoryStore interface via a concrete type assertion.
func VectorScanCandidatesFromStore(s memory.MemoryStore, ctx context.Context, query *memory.ArchiveQuery, limit int) ([]*memory.Candidate, error) {
	ms, ok := s.(*sqliteMemoryStore)
	if !ok {
		return nil, nil
	}
	return ms.vectorScanCandidates(ctx, query, limit)
}
