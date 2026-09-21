package memory

// The production embeddings lane (wave3 D4/D5): one openai-compatible POST
// per batch against the configured workspace provider, credentials resolved
// and decrypted from the provider catalog, dimension drift failing soft.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// embedServer is a scripted openai-compatible embeddings endpoint: it
// records the request bodies and answers with the scripted vectors.
type embedServer struct {
	mu      sync.Mutex
	bodies  []embeddingsRequest
	payload []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	status int
}

func (s *embedServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		var body embeddingsRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		status, payload := s.status, s.payload
		s.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": payload})
	}
}

func (s *embedServer) requests() []embeddingsRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]embeddingsRequest(nil), s.bodies...)
}

// TestProviderEmbedderBatchAndOrdering: the configured workspace provider is
// hit once per batch, vectors return in input order, and the batch is one
// HTTP call (D4).
func TestProviderEmbedderBatchAndOrdering(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	srv := &embedServer{}
	srv.mu.Lock()
	srv.payload = []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}{
		{Index: 0, Embedding: []float32{1, 2, 3}},
		{Index: 1, Embedding: []float32{4, 5, 6}},
	}
	srv.mu.Unlock()
	server := httptest.NewServer(srv.handler())
	defer server.Close()

	provider := &domain.ProviderConfig{WorkspaceID: testWorkspaceID, Type: "openai", Name: "Embed", BaseURL: server.URL}
	if err := s.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("seed embedding provider: %v", err)
	}
	if err := s.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
		WorkspaceID: testWorkspaceID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			"embedding_provider_id": provider.ID,
			"embedding_model":       "text-embedding-3-small",
			"embedding_dimension":   3,
		},
	}); err != nil {
		t.Fatalf("seed embedding config: %v", err)
	}

	embedder := NewProviderEmbedder(s.Providers(), s.ToolSettings(), nil, nil)
	vectors, err := embedder.Embed(ctx, testWorkspaceID, []string{"first text", "second text"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 4 {
		t.Fatalf("vectors must return in input order, got %+v", vectors)
	}
	reqs := srv.requests()
	if len(reqs) != 1 {
		t.Fatalf("one batch is one HTTP call, got %d calls", len(reqs))
	}
	if reqs[0].Model != "text-embedding-3-small" || len(reqs[0].Input) != 2 || reqs[0].Input[0] != "first text" {
		t.Fatalf("the batch must carry the model and every text, got %+v", reqs[0])
	}
}

// TestProviderEmbedderNotConfiguredIsSentinel: a workspace without an
// embedding pin gets ErrEmbeddingNotConfigured — the no-model world, never a
// generic failure.
func TestProviderEmbedderNotConfiguredIsSentinel(t *testing.T) {
	s := seedWorld(t)
	embedder := NewProviderEmbedder(s.Providers(), s.ToolSettings(), nil, nil)
	_, err := embedder.Embed(context.Background(), testWorkspaceID, []string{"text"})
	if err == nil || !errors.Is(err, ErrEmbeddingNotConfigured) {
		t.Fatalf("expected ErrEmbeddingNotConfigured, got %v", err)
	}
}

// TestProviderEmbedderDimensionDriftFailsSoft (D5): a drifted endpoint is a
// failed call — never silently mismatched rows.
func TestProviderEmbedderDimensionDriftFailsSoft(t *testing.T) {
	s := seedWorld(t)
	ctx := context.Background()

	srv := &embedServer{}
	srv.mu.Lock()
	srv.payload = []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}{
		{Index: 0, Embedding: []float32{1, 2, 3, 4}},
	}
	srv.mu.Unlock()
	server := httptest.NewServer(srv.handler())
	defer server.Close()

	provider := &domain.ProviderConfig{WorkspaceID: testWorkspaceID, Type: "openai", Name: "Embed", BaseURL: server.URL}
	if err := s.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("seed embedding provider: %v", err)
	}
	if err := s.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
		WorkspaceID: testWorkspaceID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			"embedding_provider_id": provider.ID,
			"embedding_model":       "text-embedding-3-small",
			"embedding_dimension":   3,
		},
	}); err != nil {
		t.Fatalf("seed embedding config: %v", err)
	}

	embedder := NewProviderEmbedder(s.Providers(), s.ToolSettings(), nil, nil)
	if _, err := embedder.Embed(ctx, testWorkspaceID, []string{"drifted"}); err == nil || errors.Is(err, ErrEmbeddingNotConfigured) {
		t.Fatalf("expected a dimension-drift error, got %v", err)
	}
}

// TestProviderEmbedderHonorsTimeoutFloor: the embedder's HTTP client is
// bounded (the probe precedent), so a hung endpoint cannot hold a worker
// slot. Construct-only smoke: the client must exist with a timeout.
func TestProviderEmbedderBoundedClient(t *testing.T) {
	embedder := NewProviderEmbedder(nil, nil, nil, nil)
	if embedder.client == nil || embedder.client.Timeout <= 0 || embedder.client.Timeout > 30*time.Second {
		t.Fatalf("the embeddings client must carry a bounded timeout, got %v", embedder.client)
	}
}
