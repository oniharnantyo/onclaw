package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
		{Name: "Brave 1", Provider: first},
		{Name: "Exa 1", Provider: second},
		{Name: "SearXNG 1", Provider: third},
	})
	require.NoError(t, err)

	results, err := chain.Search(context.Background(), "q", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "https://a.example", results[0].URL)
	require.Equal(t, 1, first.calls)
	require.Zero(t, second.calls, "later providers must not be called")
	require.Zero(t, third.calls, "later providers must not be called")
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
		failing := &fakeSearchProvider{err: &providerStatusError{name: "brave", code: http.StatusTooManyRequests}}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Brave 1", Provider: failing},
			fallback,
		})
		require.NoError(t, err)
		results, err := chain.Search(context.Background(), "q", 5)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "https://f.example", results[0].URL)
		require.Equal(t, 1, failing.calls)
	})

	t.Run("5xx status", func(t *testing.T) {
		failing := &fakeSearchProvider{err: &providerStatusError{name: "exa", code: http.StatusBadGateway}}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Exa 1", Provider: failing},
			fallback,
		})
		require.NoError(t, err)
		results, err := chain.Search(context.Background(), "q", 5)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "https://f.example", results[0].URL)
		require.Equal(t, 1, failing.calls)
	})

	t.Run("connection refused", func(t *testing.T) {
		dead := &searxngProvider{client: &http.Client{}, baseURL: newClosedServer(t)}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "SearXNG 1", Provider: dead},
			fallback,
		})
		require.NoError(t, err)
		results, err := chain.Search(context.Background(), "q", 5)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "https://f.example", results[0].URL)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"results": [{not-json`)
		}))
		defer srv.Close()

		broken := &searxngProvider{client: srv.Client(), baseURL: srv.URL}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "SearXNG 1", Provider: broken},
			fallback,
		})
		require.NoError(t, err)
		results, err := chain.Search(context.Background(), "q", 5)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "https://f.example", results[0].URL)
	})
}

func TestChain_ZeroResultsReturnsWithoutAdvancing(t *testing.T) {
	empty := &fakeSearchProvider{results: []SearchResult{}}
	never := &fakeSearchProvider{}

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Brave 1", Provider: empty},
		{Name: "Exa 1", Provider: never},
	})
	require.NoError(t, err)

	results, err := chain.Search(context.Background(), "q", 5)
	require.NoError(t, err)
	require.NotNil(t, results)
	require.Empty(t, results)
	require.Equal(t, 1, empty.calls)
	require.Zero(t, never.calls, "zero results is a valid answer; must not advance")
}

func TestChain_AllFailReturnsLastEntryNamedError(t *testing.T) {
	t.Run("status errors", func(t *testing.T) {
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Brave 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "brave", code: 429}}},
			{Name: "Exa 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "exa", code: 503}}},
			{Name: "SearXNG 1", Provider: &fakeSearchProvider{err: &providerStatusError{name: "searxng", code: 500}}},
		})
		require.NoError(t, err)
		_, err = chain.Search(context.Background(), "q", 5)
		require.Error(t, err)
		require.EqualError(t, err, "web_search: SearXNG 1 returned status 500")
	})

	t.Run("generic errors", func(t *testing.T) {
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Brave 1", Provider: &fakeSearchProvider{err: errors.New("first boom")}},
			{Name: "Exa 1", Provider: &fakeSearchProvider{err: errors.New("second boom")}},
			{Name: "SearXNG 1", Provider: &fakeSearchProvider{err: errors.New("last boom")}},
		})
		require.NoError(t, err)
		_, err = chain.Search(context.Background(), "q", 5)
		require.Error(t, err)
		require.EqualError(t, err, "web_search: SearXNG 1: last boom")
	})
}

func TestChain_RealProviderStatusErrorNamedAfterEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Brave 1", Provider: &fakeSearchProvider{err: errors.New("first boom")}},
		{Name: "SearXNG 1", Provider: &searxngProvider{client: srv.Client(), baseURL: srv.URL}},
	})
	require.NoError(t, err)

	_, err = chain.Search(context.Background(), "q", 5)
	require.Error(t, err)
	require.EqualError(t, err, "web_search: SearXNG 1 returned status 429")
}

func TestChain_TimeoutAdvances(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	slow := &searxngProvider{client: &http.Client{Timeout: 50 * time.Millisecond}, baseURL: srv.URL}
	fast := &fakeSearchProvider{results: []SearchResult{{Title: "F", URL: "https://f.example"}}}

	chain, err := NewChainSearchProvider([]NamedSearchProvider{
		{Name: "Slow", Provider: slow},
		{Name: "Fast", Provider: fast},
	})
	require.NoError(t, err)

	results, err := chain.Search(context.Background(), "q", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "https://f.example", results[0].URL)
	require.Equal(t, 1, fast.calls, "timed-out attempt must advance")
}

func TestChain_SingleEntry(t *testing.T) {
	t.Run("success passes through", func(t *testing.T) {
		only := &fakeSearchProvider{results: []SearchResult{{Title: "A"}}}
		chain, err := NewChainSearchProvider([]NamedSearchProvider{{Name: "Only", Provider: only}})
		require.NoError(t, err)
		results, err := chain.Search(context.Background(), "q", 5)
		require.NoError(t, err)
		require.Len(t, results, 1)
	})
	t.Run("failure is entry-named", func(t *testing.T) {
		chain, err := NewChainSearchProvider([]NamedSearchProvider{
			{Name: "Only", Provider: &fakeSearchProvider{err: &providerStatusError{name: "brave", code: 403}}},
		})
		require.NoError(t, err)
		_, err = chain.Search(context.Background(), "q", 5)
		require.Error(t, err)
		require.EqualError(t, err, "web_search: Only returned status 403")
	})
}

func TestChain_ConstructionErrors(t *testing.T) {
	_, err := NewChainSearchProvider(nil)
	require.Error(t, err, "expected error for nil entries")

	_, err = NewChainSearchProvider([]NamedSearchProvider{})
	require.Error(t, err, "expected error for empty entries")

	_, err = NewChainSearchProvider([]NamedSearchProvider{{Name: "Broken"}})
	require.Error(t, err, "expected error for nil provider entry")
	require.Contains(t, err.Error(), "Broken", "error should name the offending entry")
}
