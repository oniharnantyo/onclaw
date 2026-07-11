package sqlite_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestMemoryStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ms := sqlite.NewMemoryStore(db)

	doc1 := &memory.MemoryDocument{
		Agent:          "test-agent",
		Scope:          "project-1",
		Kind:           "curated",
		Content:        "Use Go for implementing core logic.",
		Source:         "test",
		EmbeddingModel: "test-model",
	}

	doc2 := &memory.MemoryDocument{
		Agent:          "test-agent",
		Scope:          "global",
		Kind:           "curated",
		Content:        "Always prioritize security scans.",
		Source:         "test",
		EmbeddingModel: "test-model",
	}

	id1, err := ms.IndexDocument(ctx, doc1, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("failed to index doc1: %v", err)
	}

	id2, err := ms.IndexDocument(ctx, doc2, []float32{0.4, 0.5, 0.6})
	if err != nil {
		t.Fatalf("failed to index doc2: %v", err)
	}

	// 1. GetDocument
	got1, err := ms.GetDocument(ctx, id1)
	if err != nil {
		t.Fatalf("failed to get doc1: %v", err)
	}
	if got1.Content != doc1.Content {
		t.Errorf("expected doc1 content %q, got %q", doc1.Content, got1.Content)
	}

	// 2. SearchArchive (FTS and Cosine ranking)
	// Query FTS matching "security"
	res, err := ms.SearchArchive(ctx, &memory.ArchiveQuery{
		Query:          "security",
		Agent:          "test-agent",
		Scope:          "project-1",
		EmbeddingModel: "test-model",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("failed to search archive: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(res))
	}
	if res[0].Document.ID != id2 {
		t.Errorf("expected doc2 to match 'security', got doc id %d", res[0].Document.ID)
	}

	// 3. Embedding cache
	hash := "some-content-hash"
	modelName := "test-model"
	cached, err := ms.GetCachedEmbedding(ctx, modelName, hash)
	if err != nil {
		t.Fatalf("failed to get cached embedding: %v", err)
	}
	if cached != nil {
		t.Errorf("expected nil cached embedding initially, got %+v", cached)
	}

	vec := []float32{0.9, 0.8, 0.7}
	err = ms.PutCachedEmbedding(ctx, modelName, hash, vec)
	if err != nil {
		t.Fatalf("failed to put cached embedding: %v", err)
	}

	cached, err = ms.GetCachedEmbedding(ctx, modelName, hash)
	if err != nil {
		t.Fatalf("failed to get cached embedding: %v", err)
	}
	if len(cached) != 3 || cached[0] != 0.9 || cached[1] != 0.8 || cached[2] != 0.7 {
		t.Errorf("cached embedding mismatch: got %+v", cached)
	}

	// 4. DeleteDocument
	err = ms.DeleteDocument(ctx, id1)
	if err != nil {
		t.Fatalf("failed to delete doc1: %v", err)
	}

	got1AfterDelete, err := ms.GetDocument(ctx, id1)
	if err != nil {
		t.Fatalf("failed to get doc1 after delete: %v", err)
	}
	if got1AfterDelete != nil {
		t.Errorf("expected doc1 to be nil after deletion, got %+v", got1AfterDelete)
	}
}
