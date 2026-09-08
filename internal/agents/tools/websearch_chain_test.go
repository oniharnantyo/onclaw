package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeSearchProvider is a SearchProvider stub with a call counter and
// canned results or error.
type fakeSearchProvider struct {
	calls   int
	results []SearchResult
	err     error
}

func (p *fakeSearchProvider) Search(_ context.Context, _ string, _ int) ([]SearchResult, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func namedResultProvider(name string, results []SearchResult) NamedSearchProvider {
	return NamedSearchProvider{Name: name, Provider: &fakeSearchProvider{results: results}}
}

func TestChain_SuccessFirstDoesNotAdvance(t *testing.T) {
	first := &fakeSearchProvider{results: []SearchResult{{Title: "A", URL: "https://a.example"}}}
	second := &fakeSearchProvider{}
	third := &fakeSearchProvider{}

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Tavily 1", Provider: first},
		{Name: "Exa 1", Provider: second},
		{Name: "Brave 1", Provider: third},
	})
	if err != nil {
		t.Fatalf("NewChainSearchProvider: %v", err)
	}

	results, err := chain.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://a.example" {
		t.Errorf("unexpected results: %+v", results)
	}
	if first.calls != 1 {
		t.Errorf("first.calls = %d, want 1", first.calls)
	}
	if second.calls != 0 || third.calls != 0 {
		t.Errorf("later providers called: second=%d third=%d, want 0/0", second.calls, third.calls)
	}
}

func TestChain_AdvancesOnError(t *testing.T) {
	fallback := namedResultProvider("Fallback", []SearchResult{{Title: "F", URL: "https://f.example"}})

	newClosedServer := func(t *testing.T) string {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		return url
	}

	t.Run("429 status", func(t *testing.T) {
		failing := &fakeSearchProvider{err: &providerStatusError{name: "tavily", code: http.StatusTooManyRequests}}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Tavily 1", Provider: failing},
			fallback,
		})
		if err != nil {
			t.Fatalf("NewChainSearchProvider: %v", err)
		}
		results, err := chain.Search(context.Background(), "q", 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) != 1 || results[0].URL != "https://f.example" {
			t.Errorf("unexpected results: %+v", results)
		}
		if failing.calls != 1 {
			t.Errorf("failing.calls = %d, want 1", failing.calls)
		}
	})

	t.Run("5xx status", func(t *testing.T) {
		failing := &fakeSearchProvider{err: &providerStatusError{name: "exa", code: http.StatusBadGateway}}
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Exa 1", Provider: failing},
			fallback,
		})
		if _, err := chain.Search(context.Background(), "q", 5); err != nil {
			t.Fatalf("Search: %v", err)
		}
		if failing.calls != 1 {
			t.Errorf("failing.calls = %d, want 1", failing.calls)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		dead := &searxngProvider{client: &http.Client{}, baseURL: newClosedServer(t)}
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "SearXNG 1", Provider: dead},
			fallback,
		})
		results, err := chain.Search(context.Background(), "q", 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) != 1 || results[0].URL != "https://f.example" {
			t.Errorf("unexpected results: %+v", results)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"results": [{not-json`)
		}))
		defer srv.Close()

		broken := &tavilyProvider{client: srv.Client(), apiKey: "tv-key", endpoint: srv.URL}
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Tavily 1", Provider: broken},
			fallback,
		})
		results, err := chain.Search(context.Background(), "q", 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) != 1 || results[0].URL != "https://f.example" {
			t.Errorf("unexpected results: %+v", results)
		}
	})
}

func TestChain_ZeroResultsReturnsWithoutAdvancing(t *testing.T) {
	empty := &fakeSearchProvider{results: []SearchResult{}}
	never := &fakeSearchProvider{}

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Tavily 1", Provider: empty},
		{Name: "Exa 1", Provider: never},
	})
	if err != nil {
		t.Fatalf("NewChainSearchProvider: %v", err)
	}

	results, err := chain.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results == nil || len(results) != 0 {
		t.Errorf("expected empty non-nil results, got %+v", results)
	}
	if empty.calls != 1 || never.calls != 0 {
		t.Errorf("calls = %d/%d, want 1/0 (zero results is a valid answer)", empty.calls, never.calls)
	}
}

func TestChain_AllFailReturnsLastEntryNamedError(t *testing.T) {
	t.Run("status errors", func(t *testing.T) {
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Tavily 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "tavily", code: 429}}},
			{Name: "Exa 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "exa", code: 503}}},
			{Name: "Brave 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "brave", code: 500}}},
		})
		_, err := chain.Search(context.Background(), "q", 5)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if want := "web.search: Brave 1 returned status 500"; err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("generic errors", func(t *testing.T) {
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Tavily 1", Provider: &fakeSearchProvider{err: errors.New("first boom")}},
			{Name: "Exa 1", Provider: &fakeSearchProvider{err: errors.New("second boom")}},
			{Name: "Brave 1", Provider: &fakeSearchProvider{err: errors.New("last boom")}},
		})
		_, err := chain.Search(context.Background(), "q", 5)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if want := "web.search: Brave 1: last boom"; err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})
}

func TestChain_RealProviderStatusErrorNamedAfterEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Tavily 1", Provider: &fakeSearchProvider{err: errors.New("first boom")}},
		{Name: "Exa 1", Provider: &tavilyProvider{client: srv.Client(), apiKey: "tv-key", endpoint: srv.URL}},
	})
	if err != nil {
		t.Fatalf("NewChainSearchProvider: %v", err)
	}

	_, err = chain.Search(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := "web.search: Exa 1 returned status 429"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestChain_TimeoutAdvances(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	slow := &tavilyProvider{client: &http.Client{Timeout: 50 * time.Millisecond}, apiKey: "tv-key", endpoint: srv.URL}
	fast := &fakeSearchProvider{results: []SearchResult{{Title: "F", URL: "https://f.example"}}}

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Slow", Provider: slow},
		{Name: "Fast", Provider: fast},
	})
	if err != nil {
		t.Fatalf("NewChainSearchProvider: %v", err)
	}

	results, err := chain.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://f.example" {
		t.Errorf("unexpected results: %+v", results)
	}
	if fast.calls != 1 {
		t.Errorf("fast.calls = %d, want 1 (timed-out attempt must advance)", fast.calls)
	}
}

func TestChain_SingleEntry(t *testing.T) {
	t.Run("success passes through", func(t *testing.T) {
		only := &fakeSearchProvider{results: []SearchResult{{Title: "A"}}}
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{{Name: "Only", Provider: only}})
		results, err := chain.Search(context.Background(), "q", 5)
		if err != nil || len(results) != 1 {
			t.Fatalf("Search = %+v, %v", results, err)
		}
	})
	t.Run("failure is entry-named", func(t *testing.T) {
		chain, _ := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Only", Provider: &fakeSearchProvider{err: &providerStatusError{name: "tavily", code: 403}}},
		})
		_, err := chain.Search(context.Background(), "q", 5)
		if want := "web.search: Only returned status 403"; err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
}

func TestChain_ConstructionErrors(t *testing.T) {
	if _, err := NewChainSearchProvider(nil); err == nil {
		t.Error("expected error for nil entries")
	}
	if _, err := NewChainSearchProvider([]NamedSearchProvider{}); err == nil {
		t.Error("expected error for empty entries")
	}
	if _, err := NewChainSearchProvider([]NamedSearchProvider{{Name: "Broken"}}); err == nil {
		t.Error("expected error for nil provider entry")
	} else if !strings.Contains(err.Error(), "Broken") {
		t.Errorf("error should name the offending entry, got %v", err)
	}
}
