package memory

import (
	"math"
	"sort"
)

// CosineSimilarity computes the cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// RankCandidates scores, normalizes, boosts, and deduplicates search candidates.
func RankCandidates(candidates []*Candidate, query *ArchiveQuery) ([]*MemoryHit, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// 1. Calculate cosine similarity for each candidate if query vector is provided
	cosines := make([]float32, len(candidates))
	hasVector := len(query.Vector) > 0
	var minCos, maxCos float32 = 1.0, -1.0
	var minFts, maxFts float64 = math.MaxFloat64, -math.MaxFloat64

	for i, c := range candidates {
		if hasVector && len(c.Vector) > 0 {
			cosines[i] = CosineSimilarity(query.Vector, c.Vector)
			if cosines[i] < minCos {
				minCos = cosines[i]
			}
			if cosines[i] > maxCos {
				maxCos = cosines[i]
			}
		}
		if c.FTSRank < minFts {
			minFts = c.FTSRank
		}
		if c.FTSRank > maxFts {
			maxFts = c.FTSRank
		}
	}

	// 2. Score and normalize
	hits := make([]*MemoryHit, 0, len(candidates))
	for i, c := range candidates {
		// FTS norm: BM25 rank (smaller is better)
		var ftsNorm float64 = 1.0
		if maxFts > minFts {
			ftsNorm = (maxFts - c.FTSRank) / (maxFts - minFts)
		}

		// Cosine norm
		var cosineNorm float64 = 1.0
		if hasVector && len(c.Vector) > 0 {
			if maxCos > minCos {
				cosineNorm = float64(cosines[i]-minCos) / float64(maxCos-minCos)
			} else {
				cosineNorm = float64(cosines[i])
			}
		}

		// Hybrid combination — use configurable weights; default to 0.3 FTS / 0.7 cosine.
		ftsW := query.FtsWeight
		if ftsW <= 0 {
			ftsW = 0.3
		}
		cosW := query.VectorWeight
		if cosW <= 0 {
			cosW = 0.7
		}
		var score float64
		if hasVector && len(c.Vector) > 0 {
			score = ftsW*ftsNorm + cosW*cosineNorm
		} else {
			// FTS-only fallback
			score = ftsNorm
		}

		// Scope boost: if candidate scope matches query scope (and it is not global), boost it
		if query.Scope != "" && query.Scope != "global" && c.Document.Scope == query.Scope {
			score *= 1.2
		}

		hits = append(hits, &MemoryHit{
			Document: c.Document,
			Score:    score,
		})
	}

	// 3. Deduplicate by document content (keeping the highest score)
	dedupedMap := make(map[string]*MemoryHit)
	for _, hit := range hits {
		content := hit.Document.Content
		if existing, ok := dedupedMap[content]; ok {
			if hit.Score > existing.Score {
				dedupedMap[content] = hit
			}
		} else {
			dedupedMap[content] = hit
		}
	}

	// Convert back to slice
	result := make([]*MemoryHit, 0, len(dedupedMap))
	for _, hit := range dedupedMap {
		result = append(result, hit)
	}

	// Sort descending by score
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Document.CreatedAt > result[j].Document.CreatedAt
		}
		return result[i].Score > result[j].Score
	})

	// Apply limit
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}

	return result, nil
}
