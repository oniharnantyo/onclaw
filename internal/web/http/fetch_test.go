package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/web"
)

func TestHttpFetcher_Fetch(t *testing.T) {
	web.AllowLoopbackForTest = true
	defer func() { web.AllowLoopbackForTest = false }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head><style>body { color: red; }</style></head><body><h1>Hello World</h1><script>alert(1);</script></body></html>"))
	}))
	defer server.Close()

	fetcher := &httpFetcher{
		cfg: web.Config{
			TimeoutSeconds: 5,
			MaxBytes:       1024 * 1024,
		},
	}

	ctx := context.Background()
	res, err := fetcher.Fetch(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Hello World"
	if res.Content != expected {
		t.Errorf("expected Content %q, got %q", expected, res.Content)
	}
}

func TestHttpFetcher_SSRF(t *testing.T) {
	// Loopback is blocked by default (AllowLoopbackForTest is false here)
	fetcher := &httpFetcher{
		cfg: web.Config{
			TimeoutSeconds: 5,
			MaxBytes:       1024 * 1024,
		},
	}

	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, "http://127.0.0.1:8500", nil)
	if err == nil {
		t.Error("expected SSRF block error, got nil")
	}
}

func TestHttpFetcher_RedirectSSRF(t *testing.T) {
	web.AllowLoopbackForTest = true
	defer func() { web.AllowLoopbackForTest = false }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a private IP which is always blocked even if loopback is allowed for test
		http.Redirect(w, r, "http://192.168.1.1/private", http.StatusFound)
	}))
	defer server.Close()

	fetcher := &httpFetcher{
		cfg: web.Config{
			TimeoutSeconds: 5,
			MaxBytes:       1024 * 1024,
		},
	}

	ctx := context.Background()
	_, err := fetcher.Fetch(ctx, server.URL, nil)
	if err == nil {
		t.Error("expected redirect SSRF block error, got nil")
	}
}
