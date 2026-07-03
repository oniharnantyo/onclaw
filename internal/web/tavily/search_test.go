package tavily

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/web"
)

type mockSecretResolver struct {
	resolve func(ctx context.Context, envVar, secretKey string) (string, error)
}

func (m *mockSecretResolver) Resolve(ctx context.Context, envVar, secretKey string) (string, error) {
	return m.resolve(ctx, envVar, secretKey)
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTavilySearcher_Success(t *testing.T) {
	resolver := &mockSecretResolver{
		resolve: func(ctx context.Context, envVar, secretKey string) (string, error) {
			if envVar == "ONCLAW_WEB_TAVILY_API_KEY" && secretKey == "web.tavily" {
				return "test-key", nil
			}
			return "", errors.New("unexpected key")
		},
	}

	responseJSON := `{
		"results": [
			{
				"title": "Tavily Title",
				"url": "https://tavily.com",
				"content": "Tavily snippet content",
				"score": 0.99
			}
		]
	}`

	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.tavily.com/search" {
				t.Errorf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected content type: %s", req.Header.Get("Content-Type"))
			}

			// Read body to verify JSON
			bodyBytes, _ := io.ReadAll(req.Body)
			if !bytes.Contains(bodyBytes, []byte(`"query":"go search"`)) {
				t.Errorf("expected query in body, got %s", string(bodyBytes))
			}
			if !bytes.Contains(bodyBytes, []byte(`"api_key":"test-key"`)) {
				t.Errorf("expected api_key in body, got %s", string(bodyBytes))
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(responseJSON)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	searcher := &tavilySearcher{
		cfg: web.Config{
			MaxBytes: 1024 * 1024,
		},
		resolver: resolver,
		client:   mockClient,
	}

	results, err := searcher.Search(context.Background(), "go search", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Tavily Title" {
		t.Errorf("expected title 'Tavily Title', got %q", results[0].Title)
	}
	if results[0].URL != "https://tavily.com" {
		t.Errorf("expected URL 'https://tavily.com', got %q", results[0].URL)
	}
	if results[0].Snippet != "Tavily snippet content" {
		t.Errorf("expected snippet, got %q", results[0].Snippet)
	}
}

func TestTavilySearcher_MissingKey(t *testing.T) {
	resolver := &mockSecretResolver{
		resolve: func(ctx context.Context, envVar, secretKey string) (string, error) {
			return "", errors.New("not found")
		},
	}

	searcher := &tavilySearcher{
		resolver: resolver,
	}

	_, err := searcher.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}
