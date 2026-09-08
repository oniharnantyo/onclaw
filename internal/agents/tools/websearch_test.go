package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

// fakeClient is an HTTPClient that returns canned responses regardless of
// the request (the request never reaches a network endpoint).
type fakeClient struct {
	status int
	body   string
}

func (c *fakeClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}

func TestNewWebSearch_RequiresProvider(t *testing.T) {
	_, err := NewWebSearch()
	if err == nil {
		t.Fatal("expected construction error without a provider, got nil")
	}
	if err.Error() != "web.search: no search provider configured" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWebSearch_Info(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(&fakeSearchProvider{}))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "web.search" {
		t.Errorf("name = %q, want web.search", info.Name)
	}
	if info.ParamsOneOf == nil {
		t.Fatal("expected ParamsOneOf populated")
	}
}

func TestWebSearch_InvokableRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[`+
			`{"title":"Result A","url":"https://a.example","content":"first"},`+
			`{"title":"Result B","url":"https://b.example","content":"second"}]}`)
	}))
	defer srv.Close()

	tl, err := NewWebSearch(WithSearchProvider(&tavilyProvider{
		client:   srv.Client(),
		apiKey:   "tv-key",
		endpoint: srv.URL,
	}))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	it, ok := tl.(tool.InvokableTool)
	if !ok {
		t.Fatal("webSearchTool must implement tool.InvokableTool")
	}

	out, err := it.InvokableRun(context.Background(), `{"query":"test","num":5}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	for _, want := range []string{"https://a.example", "https://b.example", "first", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(&fakeSearchProvider{}))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

func TestWebSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tl, err := NewWebSearch(WithSearchProvider(&tavilyProvider{
		client:   srv.Client(),
		apiKey:   "tv-key",
		endpoint: srv.URL,
	}))
	if err != nil {
		t.Fatalf("NewWebSearch: %v", err)
	}
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "returned status 503") {
		t.Errorf("expected status error, got %v", err)
	}
}

func TestTavilyProvider_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"T","url":"https://t.example","content":"snippet"}]}`)
	}))
	defer srv.Close()

	p := &tavilyProvider{client: srv.Client(), apiKey: "tv-key", endpoint: srv.URL}
	results, err := p.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Title != "T" || results[0].URL != "https://t.example" || results[0].Snippet != "snippet" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestTavilyProvider_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := &tavilyProvider{client: srv.Client(), apiKey: "tv-key", endpoint: srv.URL}
	_, err := p.Search(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	if want := "web.search: tavily returned status 429"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestNewSearchProviderByCredential(t *testing.T) {
	t.Run("nil client falls back to internal default", func(t *testing.T) {
		p, err := NewSearchProviderByCredential("tavily", "tv-key", nil)
		if err != nil {
			t.Fatalf("NewSearchProviderByCredential: %v", err)
		}
		if p == nil {
			t.Fatal("expected provider, got nil")
		}
	})
	t.Run("missing api key", func(t *testing.T) {
		if _, err := NewSearchProviderByCredential("tavily", "", nil); err == nil {
			t.Error("expected error for missing tavily API key")
		}
	})
	t.Run("missing base url", func(t *testing.T) {
		if _, err := NewSearchProviderByCredential("searxng", "", nil); err == nil {
			t.Error("expected error for missing searxng base URL")
		}
	})
	t.Run("unknown provider", func(t *testing.T) {
		if _, err := NewSearchProviderByCredential("bogus", "k", nil); err == nil {
			t.Error("expected error for unknown provider")
		}
	})
	t.Run("duckduckgo is no longer registered", func(t *testing.T) {
		if _, err := NewSearchProviderByCredential("duckduckgo", "", nil); err == nil {
			t.Error("expected error for removed duckduckgo provider")
		}
	})
}

func TestSearchProviders_ExcludesDuckDuckGo(t *testing.T) {
	providers := SearchProviders()
	if len(providers) != 6 {
		t.Errorf("expected 6 registered providers, got %d: %+v", len(providers), providers)
	}
	for _, p := range providers {
		if p.ID == "duckduckgo" {
			t.Errorf("duckduckgo must not be registered, got %+v", p)
		}
	}
	if _, ok := SearchProviderInfoFor("duckduckgo"); ok {
		t.Error("SearchProviderInfoFor(duckduckgo) must report unknown")
	}
	if info, ok := SearchProviderInfoFor("tavily"); !ok || info.Credential != SearchCredentialAPIKey {
		t.Errorf("tavily info = %+v, ok = %v", info, ok)
	}
}
