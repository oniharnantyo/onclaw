package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
)

// EinoEmbedder is the eino-ext embedding.Embedder interface used for vector embedding.
// It matches github.com/cloudwego/eino/components/embedding.Embedder.
type EinoEmbedder = embedding.Embedder

// Embedder wraps an eino-ext embedding.Embedder with content-hash caching backed
// by the MemoryStore. The caching layer avoids repeated remote calls for identical
// text and enables FTS-only fallback when the underlying provider is unreachable.
type Embedder struct {
	// Provider is the underlying eino-ext embedding component (openai/gemini/ollama).
	// May be nil; in that case Embed always returns nil, nil (FTS-only mode).
	Provider EinoEmbedder
	// Store is used to read/write the embedding_cache table.
	// May be nil (no caching).
	Store MemoryStore
}

// NewEmbedder constructs an Embedder that wraps the given eino-ext provider and
// caches results in store. Either argument may be nil.
func NewEmbedder(store MemoryStore, provider EinoEmbedder) *Embedder {
	return &Embedder{
		Provider: provider,
		Store:    store,
	}
}

// ComputeHash calculates the SHA-256 hash of a string, used as the cache key.
func ComputeHash(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// Embed returns the vector embedding for text, reading from cache first and
// writing back on a cache miss. Returns nil, nil when text is empty or the
// provider is nil (graceful FTS-only degradation).
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, nil
	}
	if e.Provider == nil {
		// No provider configured — FTS-only mode, callers handle nil vectors.
		return nil, nil
	}

	hash := ComputeHash(text)
	if e.Store != nil {
		if cached, err := e.Store.GetCachedEmbedding(ctx, hash); err == nil && len(cached) > 0 {
			return cached, nil
		}
	}

	// EmbedStrings returns [][]float64; we take the first (only) result.
	vecs, err := e.Provider.EmbedStrings(ctx, []string{text})
	if err != nil {
		// Propagate so call sites can decide to degrade to FTS-only.
		return nil, fmt.Errorf("embed failed: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embed returned empty vector")
	}

	// Convert float64 → float32 for compact BLOB storage.
	vec := make([]float32, len(vecs[0]))
	for i, v := range vecs[0] {
		vec[i] = float32(v)
	}

	if e.Store != nil {
		_ = e.Store.PutCachedEmbedding(ctx, hash, vec)
	}
	return vec, nil
}
