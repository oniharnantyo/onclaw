package exa

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

func TestExaSearcher_Success(t *testing.T) {
	resolver := &mockSecretResolver{
		resolve: func(ctx context.Context, envVar, secretKey string) (string, error) {
			if envVar == "ONCLAW_WEB_EXA_API_KEY" && secretKey == "web.exa" {
				return "test-key-exa", nil
			}
			return "", errors.New("unexpected key")
		},
	}

	responseJSON := `{
		"results": [
			{
				"title": "Exa Title",
				"url": "https://exa.ai",
				"text": "Exa snippet content"
			}
		]
	}`

	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.exa.ai/search" {
				t.Errorf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected content type: %s", req.Header.Get("Content-Type"))
			}
			if req.Header.Get("x-api-key") != "test-key-exa" {
				t.Errorf("unexpected API key header: %s", req.Header.Get("x-api-key"))
			}

			// Read body to verify JSON
			bodyBytes, _ := io.ReadAll(req.Body)
			if !bytes.Contains(bodyBytes, []byte(`"query":"go exa"`)) {
				t.Errorf("expected query in body, got %s", string(bodyBytes))
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(responseJSON)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	searcher := &exaSearcher{
		cfg: web.Config{
			MaxBytes: 1024 * 1024,
		},
		resolver: resolver,
		client:   mockClient,
	}

	results, err := searcher.Search(context.Background(), "go exa", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Exa Title" {
		t.Errorf("expected title 'Exa Title', got %q", results[0].Title)
	}
	if results[0].URL != "https://exa.ai" {
		t.Errorf("expected URL 'https://exa.ai', got %q", results[0].URL)
	}
	if results[0].Snippet != "Exa snippet content" {
		t.Errorf("expected snippet, got %q", results[0].Snippet)
	}
}
