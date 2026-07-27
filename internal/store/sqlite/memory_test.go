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

func TestMemoryStore_UpdateDocument(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ms := sqlite.NewMemoryStore(db)

	doc := &memory.MemoryDocument{
		Agent:          "test-agent",
		Scope:          "global",
		Kind:           "curated",
		Content:        "Original content about architecture.",
		Source:         "remember",
		EmbeddingModel: "test-model",
	}

	id, err := ms.IndexDocument(ctx, doc, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("failed to index doc: %v", err)
	}

	origDoc, err := ms.GetDocument(ctx, id)
	if err != nil || origDoc == nil {
		t.Fatalf("failed to get origDoc: %v", err)
	}

	// 1. Happy path: update content + vector
	newContent := "Updated content about microservices."
	newVector := []float32{0.7, 0.8, 0.9}
	if err := ms.UpdateDocument(ctx, id, newContent, newVector); err != nil {
		t.Fatalf("UpdateDocument failed: %v", err)
	}

	updatedDoc, err := ms.GetDocument(ctx, id)
	if err != nil || updatedDoc == nil {
		t.Fatalf("failed to get updatedDoc: %v", err)
	}
	if updatedDoc.ID != id {
		t.Errorf("expected ID %d, got %d", id, updatedDoc.ID)
	}
	if updatedDoc.CreatedAt != origDoc.CreatedAt {
		t.Errorf("expected CreatedAt %q preserved, got %q", origDoc.CreatedAt, updatedDoc.CreatedAt)
	}
	if updatedDoc.Content != newContent {
		t.Errorf("expected Content %q, got %q", newContent, updatedDoc.Content)
	}

	// Verify FTS re-synced: searching for "microservices" finds updatedDoc
	res, err := ms.SearchArchive(ctx, &memory.ArchiveQuery{
		Query:          "microservices",
		Agent:          "test-agent",
		Scope:          "global",
		EmbeddingModel: "test-model",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchArchive failed: %v", err)
	}
	if len(res) != 1 || res[0].Document.ID != id {
		t.Fatalf("expected 1 result with id %d matching 'microservices', got %d results", id, len(res))
	}

	// 2. Vector-empty path: update content with empty vector
	newerContent := "Newer content about monolithic app."
	if err := ms.UpdateDocument(ctx, id, newerContent, nil); err != nil {
		t.Fatalf("UpdateDocument with empty vector failed: %v", err)
	}
	newerDoc, err := ms.GetDocument(ctx, id)
	if err != nil || newerDoc == nil {
		t.Fatalf("failed to get newerDoc: %v", err)
	}
	if newerDoc.Content != newerContent {
		t.Errorf("expected Content %q, got %q", newerContent, newerDoc.Content)
	}

	// 3. Not found path
	if err := ms.UpdateDocument(ctx, 99999, "Non-existent", nil); err == nil {
		t.Errorf("expected error updating non-existent doc ID 99999, got nil")
	}
}

func TestMemoryStore_EmbeddingModelFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ms := sqlite.NewMemoryStore(db)

	doc := &memory.MemoryDocument{
		Agent:          "test-agent",
		Scope:          "global",
		Kind:           "curated",
		Content:        "Vector indexed under text-embedding-3-small.",
		Source:         "remember",
		EmbeddingModel: "text-embedding-3-small",
	}

	id, err := ms.IndexDocument(ctx, doc, []float32{0.1, 0.2, 0.3})
	if err != nil {
		t.Fatalf("IndexDocument failed: %v", err)
	}

	// 1. Search with matching EmbeddingModel -> finds doc
	resMatch, err := ms.SearchArchive(ctx, &memory.ArchiveQuery{
		Query:          "Vector",
		Agent:          "test-agent",
		Scope:          "global",
		EmbeddingModel: "text-embedding-3-small",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchArchive failed: %v", err)
	}
	if len(resMatch) != 1 || resMatch[0].Document.ID != id {
		t.Fatalf("expected 1 match for model 'text-embedding-3-small', got %d", len(resMatch))
	}

	// 2. Search with non-matching EmbeddingModel -> excludes doc
	resMismatch, err := ms.SearchArchive(ctx, &memory.ArchiveQuery{
		Query:          "Vector",
		Agent:          "test-agent",
		Scope:          "global",
		EmbeddingModel: "text-embedding-004",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchArchive failed: %v", err)
	}
	if len(resMismatch) != 0 {
		t.Fatalf("expected 0 matches for model 'text-embedding-004', got %d", len(resMismatch))
	}
}
