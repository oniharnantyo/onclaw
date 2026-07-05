package memory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

// mockEinoEmbedder is a controllable fake implementing memory.EinoEmbedder.
type mockEinoEmbedder struct {
	vecs [][]float64
	err  error
	// callCount tracks how many times EmbedStrings was called.
	callCount int
}

func (m *mockEinoEmbedder) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.vecs, nil
}

// mockEmbedStore is an in-memory MemoryStore for embedding tests.
type mockEmbedStore struct {
	cache map[string][]float32
}

func (m *mockEmbedStore) IndexDocument(_ context.Context, _ *memory.MemoryDocument, _ []float32) (int64, error) {
	return 0, nil
}
func (m *mockEmbedStore) SearchArchive(_ context.Context, _ *memory.ArchiveQuery) ([]*memory.MemoryHit, error) {
	return nil, nil
}
func (m *mockEmbedStore) GetDocument(_ context.Context, _ int64) (*memory.MemoryDocument, error) {
	return nil, nil
}
func (m *mockEmbedStore) DeleteDocument(_ context.Context, _ int64) error { return nil }
func (m *mockEmbedStore) GetCachedEmbedding(_ context.Context, hash string) ([]float32, error) {
	if m.cache == nil {
		return nil, nil
	}
	return m.cache[hash], nil
}
func (m *mockEmbedStore) PutCachedEmbedding(_ context.Context, hash string, vec []float32) error {
	if m.cache == nil {
		m.cache = make(map[string][]float32)
	}
	m.cache[hash] = vec
	return nil
}

func TestEmbedder_EmptyText(t *testing.T) {
	e := memory.NewEmbedder(nil, nil)
	vec, err := e.Embed(context.Background(), "")
	if err != nil || vec != nil {
		t.Errorf("expected nil, nil for empty text; got vec=%v err=%v", vec, err)
	}
}

func TestEmbedder_NilProvider_ReturnNil(t *testing.T) {
	e := memory.NewEmbedder(nil, nil)
	vec, err := e.Embed(context.Background(), "some text")
	if err != nil || vec != nil {
		t.Errorf("expected nil, nil when provider is nil (FTS-only mode); got vec=%v err=%v", vec, err)
	}
}

func TestEmbedder_CacheHit(t *testing.T) {
	store := &mockEmbedStore{}
	provider := &mockEinoEmbedder{} // never called; cache should intercept

	hash := memory.ComputeHash("hello")
	_ = store.PutCachedEmbedding(context.Background(), hash, []float32{0.1, 0.2})

	e := memory.NewEmbedder(store, provider)
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 2 || vec[0] != 0.1 || vec[1] != 0.2 {
		t.Errorf("unexpected cached vector: %v", vec)
	}
	if provider.callCount != 0 {
		t.Errorf("provider should not have been called on cache hit, got %d calls", provider.callCount)
	}
}

func TestEmbedder_HappyPath_CachesResult(t *testing.T) {
	store := &mockEmbedStore{}
	provider := &mockEinoEmbedder{
		vecs: [][]float64{{0.5, -0.5, 0.1}},
	}

	e := memory.NewEmbedder(store, provider)
	vec, err := e.Embed(context.Background(), "test content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.5 || vec[1] != -0.5 {
		t.Errorf("unexpected vector: %v", vec)
	}

	// Verify it cached the result.
	hash := memory.ComputeHash("test content")
	cached, _ := store.GetCachedEmbedding(context.Background(), hash)
	if len(cached) != 3 || cached[0] != 0.5 {
		t.Errorf("vector was not cached correctly: %v", cached)
	}

	// Second call must use cache, not provider.
	provider.callCount = 0
	_, _ = e.Embed(context.Background(), "test content")
	if provider.callCount != 0 {
		t.Errorf("second Embed should use cache; provider was called %d times", provider.callCount)
	}
}

func TestEmbedder_ProviderError(t *testing.T) {
	provider := &mockEinoEmbedder{err: fmt.Errorf("provider unavailable")}
	e := memory.NewEmbedder(nil, provider)
	_, err := e.Embed(context.Background(), "fail content")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "provider unavailable") {
		t.Errorf("expected 'provider unavailable' in error, got: %v", err)
	}
}

func TestEmbedder_ProviderEmptyResult(t *testing.T) {
	provider := &mockEinoEmbedder{vecs: [][]float64{}} // empty slice
	e := memory.NewEmbedder(nil, provider)
	_, err := e.Embed(context.Background(), "empty response")
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Errorf("expected 'empty vector' error, got: %v", err)
	}
}

func TestEmbedder_Float64ToFloat32Conversion(t *testing.T) {
	provider := &mockEinoEmbedder{
		vecs: [][]float64{{1.0, 2.0, 3.0}},
	}
	e := memory.NewEmbedder(nil, provider)
	vec, err := e.Embed(context.Background(), "conversion test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 || vec[0] != float32(1.0) || vec[1] != float32(2.0) || vec[2] != float32(3.0) {
		t.Errorf("float64→float32 conversion incorrect: %v", vec)
	}
}

func TestEmbedder_NilStore_NoCache(t *testing.T) {
	provider := &mockEinoEmbedder{
		vecs: [][]float64{{1.0}},
	}
	e := memory.NewEmbedder(nil, provider) // nil store
	vec, err := e.Embed(context.Background(), "no-store text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 1 || vec[0] != 1.0 {
		t.Errorf("unexpected vector: %v", vec)
	}
	// Second call goes to provider again (no cache).
	provider.callCount = 0
	_, _ = e.Embed(context.Background(), "no-store text")
	if provider.callCount != 1 {
		t.Errorf("expected provider call on nil-store second embed, got %d", provider.callCount)
	}
}

func TestEmbedder_ComputeHash_Stable(t *testing.T) {
	h1 := memory.ComputeHash("same text")
	h2 := memory.ComputeHash("same text")
	if h1 != h2 {
		t.Errorf("hash is not stable: %v != %v", h1, h2)
	}
	h3 := memory.ComputeHash("different text")
	if h1 == h3 {
		t.Errorf("different text produced the same hash")
	}
}
