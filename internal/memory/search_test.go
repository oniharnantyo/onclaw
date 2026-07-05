package memory_test

import (
	"math"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

func TestCosineSimilarity(t *testing.T) {
	// Orthogonal vectors: cosine = 0
	v1 := []float32{1, 0, 0}
	v2 := []float32{0, 1, 0}
	if sim := memory.CosineSimilarity(v1, v2); sim != 0 {
		t.Errorf("expected 0, got %f", sim)
	}

	// Identical vectors: cosine = 1
	v3 := []float32{1, 2, 3}
	if sim := memory.CosineSimilarity(v3, v3); math.Abs(float64(sim-1.0)) > 1e-6 {
		t.Errorf("expected 1, got %f", sim)
	}

	// Inverse vectors: cosine = -1
	v4 := []float32{-1, -2, -3}
	if sim := memory.CosineSimilarity(v3, v4); math.Abs(float64(sim+1.0)) > 1e-6 {
		t.Errorf("expected -1, got %f", sim)
	}
}

func TestRankCandidates(t *testing.T) {
	candidates := []*memory.Candidate{
		{
			Document: &memory.MemoryDocument{
				ID:      1,
				Content: "First unique memory content",
				Scope:   "agent-1",
			},
			Vector:  []float32{1, 0, 0},
			FTSRank: -10.0, // Best FTS rank
		},
		{
			Document: &memory.MemoryDocument{
				ID:      2,
				Content: "Second unique memory content",
				Scope:   "global",
			},
			Vector:  []float32{0, 1, 0},
			FTSRank: -5.0,
		},
		{
			Document: &memory.MemoryDocument{
				ID:      3,
				Content: "First unique memory content", // Duplicate content
				Scope:   "global",
			},
			Vector:  []float32{1, 0, 0},
			FTSRank: -2.0,
		},
	}

	// 1. Test hybrid ranking with vector query
	query := &memory.ArchiveQuery{
		Query:  "some terms",
		Agent:  "agent-1",
		Scope:  "agent-1",
		Vector: []float32{1, 0, 0}, // Matches doc 1 and 3 perfectly
		Limit:  10,
	}

	hits, err := memory.RankCandidates(candidates, query)
	if err != nil {
		t.Fatalf("RankCandidates failed: %v", err)
	}

	// Expected deduplication: doc 3 should be filtered since doc 1 has identical content but better score/rank
	if len(hits) != 2 {
		t.Errorf("expected 2 deduped hits, got %d", len(hits))
	}

	// First hit should be doc 1 (exact vector match + best FTS + scope boost)
	if hits[0].Document.ID != 1 {
		t.Errorf("expected first hit to be doc 1, got doc id %d", hits[0].Document.ID)
	}

	// 2. Test FTS-only fallback (no query vector)
	queryNoVec := &memory.ArchiveQuery{
		Query: "some terms",
		Agent: "agent-1",
		Scope: "agent-1",
		Limit: 10,
	}
	hitsNoVec, err := memory.RankCandidates(candidates, queryNoVec)
	if err != nil {
		t.Fatalf("RankCandidates failed: %v", err)
	}
	if len(hitsNoVec) != 2 {
		t.Errorf("expected 2 hits, got %d", len(hitsNoVec))
	}
	if hitsNoVec[0].Document.ID != 1 {
		t.Errorf("expected first hit to be doc 1 (best FTS rank), got %d", hitsNoVec[0].Document.ID)
	}
}
