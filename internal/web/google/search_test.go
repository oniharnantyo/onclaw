package google

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
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

func TestGoogleSearcher_Success(t *testing.T) {
	resolver := &mockSecretResolver{
		resolve: func(ctx context.Context, envVar, secretKey string) (string, error) {
			if envVar == "ONCLAW_WEB_GOOGLE_API_KEY" && secretKey == "web.google" {
				return "test-key-google", nil
			}
			return "", errors.New("unexpected key")
		},
	}

	responseJSON := `{
		"items": [
			{
				"title": "Google Title",
				"link": "https://google.com/test",
				"snippet": "Google snippet content"
			}
		]
	}`

	mockClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.HasPrefix(req.URL.String(), "https://customsearch.googleapis.com/customsearch/v1") {
				t.Errorf("unexpected URL: %s", req.URL.String())
			}

			// Parse query params
			u, _ := url.Parse(req.URL.String())
			q := u.Query()
			if q.Get("q") != "go google" {
				t.Errorf("expected q 'go google', got %s", q.Get("q"))
			}
			if q.Get("key") != "test-key-google" {
				t.Errorf("expected key 'test-key-google', got %s", q.Get("key"))
			}
			if q.Get("cx") != "test-cx" {
				t.Errorf("expected cx 'test-cx', got %s", q.Get("cx"))
			}
			if q.Get("num") != "5" {
				t.Errorf("expected num '5', got %s", q.Get("num"))
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(responseJSON)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	searcher := &googleSearcher{
		cfg: web.Config{
			GoogleCX: "test-cx",
			MaxBytes: 1024 * 1024,
		},
		resolver: resolver,
		client:   mockClient,
	}

	results, err := searcher.Search(context.Background(), "go google", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Google Title" {
		t.Errorf("expected title 'Google Title', got %q", results[0].Title)
	}
	if results[0].URL != "https://google.com/test" {
		t.Errorf("expected URL 'https://google.com/test', got %q", results[0].URL)
	}
	if results[0].Snippet != "Google snippet content" {
		t.Errorf("expected snippet, got %q", results[0].Snippet)
	}
}

func TestGoogleSearcher_MissingCX(t *testing.T) {
	resolver := &mockSecretResolver{
		resolve: func(ctx context.Context, envVar, secretKey string) (string, error) {
			return "some-key", nil
		},
	}

	searcher := &googleSearcher{
		cfg:      web.Config{}, // google_cx is empty
		resolver: resolver,
	}

	_, err := searcher.Search(context.Background(), "test", 5)
	if err == nil || !strings.Contains(err.Error(), "Google Custom Search Engine ID (google_cx) is not configured") {
		t.Errorf("expected empty CX error, got %v", err)
	}
}
