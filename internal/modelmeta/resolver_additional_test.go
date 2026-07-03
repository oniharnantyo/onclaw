package modelmeta_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/modelmeta"
)

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestEnumerateAndHttpEdgeCases(t *testing.T) {
	// Reset the HTTP client after tests
	defer modelmeta.SetHTTPClient(&http.Client{Timeout: 5 * time.Second})

	t.Run("applyBaseURLDefault cases", func(t *testing.T) {
		// We can test Resolve with empty baseURL and different providerTypes, which calls applyBaseURLDefault.
		// Since we don't have direct access to applyBaseURLDefault, we verify its behavior through public API Resolve.
		ctx := context.Background()

		// Case 1: Anthropic default base URL
		meta := modelmeta.Resolve(ctx, "claude-v1", "anthropic", "", "", nil)
		if len(meta.InputModalities) != 1 || meta.InputModalities[0] != "text" {
			t.Errorf("expected default modalities, got %v", meta.InputModalities)
		}

		// Case 2: OpenAI default base URL (will make HTTP request and fail if not mocked, but we pass empty api key and no cached response, so it fails getNativeContextWindow and continues)
		metaOpenAI := modelmeta.Resolve(ctx, "gpt-4", "openai", "", "", nil)
		_ = metaOpenAI

		// Case 3: Ollama default base URL
		metaOllama := modelmeta.Resolve(ctx, "llama", "ollama", "", "", nil)
		_ = metaOllama

		// Case 4: Unrecognized provider
		metaOther := modelmeta.Resolve(ctx, "model", "other-prov", "", "", nil)
		_ = metaOther
	})

	t.Run("Enumerate Ollama", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/api/tags") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3:latest"}, {"name": "mistral"}]}`))
		}))
		defer server.Close()

		models, err := modelmeta.Enumerate(context.Background(), "ollama", server.URL, "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(models) != 2 || models[0] != "llama3:latest" || models[1] != "mistral" {
			t.Errorf("unexpected models: %v", models)
		}
	})

	t.Run("Enumerate OpenAI-compatible and Anthropic Header Check", func(t *testing.T) {
		var authHeader, xApiKey, anthropicVersion string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			xApiKey = r.Header.Get("x-api-key")
			anthropicVersion = r.Header.Get("anthropic-version")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o", "context_length": 128000}]}`))
		}))
		defer server.Close()

		// Test OpenAI authorization header
		_, err := modelmeta.Enumerate(context.Background(), "openai", server.URL, "my-secret-key")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if authHeader != "Bearer my-secret-key" {
			t.Errorf("expected Bearer token, got %q", authHeader)
		}

		// Test Anthropic headers
		_, err = modelmeta.Enumerate(context.Background(), "anthropic", server.URL, "anthropic-key")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if xApiKey != "anthropic-key" || anthropicVersion != "2023-06-01" {
			t.Errorf("expected anthropic headers, got key=%q version=%q", xApiKey, anthropicVersion)
		}
	})

	t.Run("Enumerate HTTP and JSON Errors", func(t *testing.T) {
		// 1. HTTP error (status 500) for OpenAI
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		_, err := modelmeta.Enumerate(context.Background(), "openai", server.URL, "")
		if err == nil {
			t.Error("expected error for status 500, got nil")
		}

		// 2. Invalid JSON response for OpenAI
		serverBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json`))
		}))
		defer serverBadJSON.Close()

		_, err = modelmeta.Enumerate(context.Background(), "openai", serverBadJSON.URL, "")
		if err == nil {
			t.Error("expected error for bad JSON, got nil")
		}

		// 3. HTTP error (status 500) for Ollama
		serverOllamaErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer serverOllamaErr.Close()

		_, err = modelmeta.Enumerate(context.Background(), "ollama", serverOllamaErr.URL, "")
		if err == nil {
			t.Error("expected error for Ollama status 500, got nil")
		}

		// 4. Invalid JSON response for Ollama
		serverOllamaBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json`))
		}))
		defer serverOllamaBadJSON.Close()

		_, err = modelmeta.Enumerate(context.Background(), "ollama", serverOllamaBadJSON.URL, "")
		if err == nil {
			t.Error("expected error for Ollama bad JSON, got nil")
		}
	})

	t.Run("FetchOpenAIModels Cache Logic", func(t *testing.T) {
		serverCallCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serverCallCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "cached-model", "context_length": 8192}]}`))
		}))
		defer server.Close()

		// Set up context cache
		cache := &modelmeta.ModelCache{}
		ctx := context.WithValue(context.Background(), modelmeta.OpenaiModelsCacheKey, cache)

		// First call should request server
		models1, err := modelmeta.Enumerate(ctx, "openai", server.URL, "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if serverCallCount != 1 {
			t.Errorf("expected server calls = 1, got %d", serverCallCount)
		}

		// Second call with same context should hit the cache and NOT call server
		models2, err := modelmeta.Enumerate(ctx, "openai", server.URL, "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if serverCallCount != 1 {
			t.Errorf("expected server calls to remain 1, got %d", serverCallCount)
		}

		if len(models1) != 1 || len(models2) != 1 || models1[0] != models2[0] {
			t.Errorf("expected same models, got %v and %v", models1, models2)
		}
	})

	t.Run("Resolve with getNativeContextWindow error and show API error", func(t *testing.T) {
		// Ollama show returning 500
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Get native context window fails, so fallback to catalog or 0
		meta := modelmeta.Resolve(context.Background(), "llama", "ollama", server.URL, "", nil)
		if meta.ContextWindow != 0 {
			t.Errorf("expected 0 context window, got %d", meta.ContextWindow)
		}

		// Ollama show returning invalid json
		serverBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json`))
		}))
		defer serverBadJSON.Close()

		meta2 := modelmeta.Resolve(context.Background(), "llama", "ollama", serverBadJSON.URL, "", nil)
		if meta2.ContextWindow != 0 {
			t.Errorf("expected 0 context window, got %d", meta2.ContextWindow)
		}
	})

	t.Run("GetCatalog", func(t *testing.T) {
		// Set HOME to a temporary directory so we don't pollute local machine
		tmpDir, err := os.MkdirTemp("", "onclaw-getcatalog-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		oldHome := os.Getenv("HOME")
		defer os.Setenv("HOME", oldHome)
		os.Setenv("HOME", tmpDir)

		// 1. Success case
		client := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       os.NewFile(0, "dev/null"), // placeholder, but we will write custom body
				}
			}),
		}
		_ = client // We will mock via httptest instead to avoid custom RoundTripper complex body handling

		catalogJSON := `{
			"openai": {
				"models": {
					"gpt-4o": {
						"limit": {"context": 128000},
						"reasoning": false,
						"modalities": {"input": ["text"]}
					}
				}
			}
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(catalogJSON))
		}))
		defer server.Close()

		// Change package-level client to point to our mock server or override the URL fetch behavior
		// Wait, GetCatalog calls LoadOrRefreshCatalog("https://models.dev/api.json") directly!
		// But LoadOrRefreshCatalog calls FetchCatalog(ctx, url) which uses the package level httpClient.
		// So if we set httpClient to a custom Client with a RoundTripper that redirects everything to our server or returns custom response:
		mockClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) *http.Response {
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusOK)
				_, _ = rec.Write([]byte(catalogJSON))
				return rec.Result()
			}),
		}
		modelmeta.SetHTTPClient(mockClient)

		cat, err := modelmeta.GetCatalog()
		if err != nil {
			t.Fatalf("unexpected GetCatalog err: %v", err)
		}
		if _, ok := cat.Providers["openai"]; !ok {
			t.Errorf("expected openai provider in catalog")
		}

		// 2. FetchCatalog returns non-200 error
		errClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) *http.Response {
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result()
			}),
		}
		modelmeta.SetHTTPClient(errClient)

		// Delete cached files to force fetch
		cacheDir, _ := modelmeta.CacheDir()
		_ = os.RemoveAll(cacheDir)

		_, err = modelmeta.GetCatalog()
		if err == nil {
			t.Error("expected error for non-200 status catalog fetch, got nil")
		}

		// 3. Bad JSON catalog unmarshal error
		badJSONClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) *http.Response {
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusOK)
				_, _ = rec.Write([]byte(`{invalid-json`))
				return rec.Result()
			}),
		}
		modelmeta.SetHTTPClient(badJSONClient)

		_ = os.RemoveAll(cacheDir)
		_, err = modelmeta.GetCatalog()
		if err == nil {
			t.Error("expected error for bad JSON catalog, got nil")
		}
	})
}
