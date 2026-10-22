package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"
)

// searchFakeClient is an HTTPClient that returns canned responses regardless
// of the request (the request never reaches a network endpoint).
type searchFakeClient struct {
	status int
	body   string
}

func (c *searchFakeClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}

type stubSearchProvider struct{}

func (stubSearchProvider) Search(context.Context, string, int) ([]SearchResult, error) {
	return []SearchResult{{Title: "One", URL: "https://stub.example/1", Snippet: "s"}}, nil
}

func TestNewWebSearch_RequiresProvider(t *testing.T) {
	_, err := NewWebSearch()
	require.Error(t, err, "expected construction error without a provider")
	require.EqualError(t, err, "web_search: no search provider configured")
}

func TestWebSearch_Info(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(&fakeSearchProvider{}))
	require.NoError(t, err)
	info, err := tl.Info(context.Background())
	require.NoError(t, err)
	require.Equal(t, "web_search", info.Name)
	require.NotNil(t, info.ParamsOneOf, "expected ParamsOneOf populated")
}

func TestWebSearch_UsesProvider(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(stubSearchProvider{}))
	require.NoError(t, err)
	out, err := tl.(webInvoker).InvokableRun(context.Background(), `{"query":"golang"}`)
	require.NoError(t, err)
	require.Contains(t, out, "https://stub.example/1")
}

func TestWebSearch_InvokableRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[`+
			`{"title":"Result A","url":"https://a.example","content":"first"},`+
			`{"title":"Result B","url":"https://b.example","content":"second"}]}`)
	}))
	defer srv.Close()

	tl, err := NewWebSearch(WithSearchProvider(&searxngProvider{
		client:  srv.Client(),
		baseURL: srv.URL,
	}))
	require.NoError(t, err)
	it, ok := tl.(tool.InvokableTool)
	require.True(t, ok, "webSearchTool must implement tool.InvokableTool")

	out, err := it.InvokableRun(context.Background(), `{"query":"test","num":5}`)
	require.NoError(t, err)
	for _, want := range []string{"https://a.example", "https://b.example", "first", "second"} {
		require.Contains(t, out, want)
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(&fakeSearchProvider{}))
	require.NoError(t, err)
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":""}`)
	require.Error(t, err, "expected error for empty query")
}

// TestWebSearch_ResolverErrorMemoized pins the lazy lane's failure behavior:
// the resolver's error surfaces from InvokableRun on the first call and
// identically on the second, with the resolver itself invoked exactly once —
// the failed resolution is cached on the tool instance.
func TestWebSearch_ResolverErrorMemoized(t *testing.T) {
	calls := 0
	want := "web_search is not configured — add a provider in Settings → Tools"
	tl, err := NewWebSearch(WithSearchProviderResolver(func(context.Context) (SearchProvider, error) {
		calls++
		return nil, errors.New(want)
	}))
	require.NoError(t, err)
	it, ok := tl.(tool.InvokableTool)
	require.True(t, ok, "webSearchTool must implement tool.InvokableTool")

	for i := 0; i < 2; i++ {
		_, err := it.InvokableRun(context.Background(), `{"query":"test"}`)
		require.Error(t, err)
		require.EqualError(t, err, want)
	}
	require.Equal(t, 1, calls, "resolver invoked %d times, want exactly 1", calls)
}

func TestWebSearch_ResolverSuccessServesQuery(t *testing.T) {
	provider := &fakeSearchProvider{results: []SearchResult{
		{Title: "R", URL: "https://r.example", Snippet: "snippet"},
	}}
	tl, err := NewWebSearch(WithSearchProviderResolver(func(context.Context) (SearchProvider, error) {
		return provider, nil
	}))
	require.NoError(t, err)
	out, err := tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.NoError(t, err)
	require.Contains(t, out, "https://r.example")
}

func TestWebSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tl, err := NewWebSearch(WithSearchProvider(&searxngProvider{
		client:  srv.Client(),
		baseURL: srv.URL,
	}))
	require.NoError(t, err)
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.Error(t, err, "expected error for non-200 status")
	require.Contains(t, err.Error(), "returned status 503")
}

// stubChainSource is a SearchConfigSource returning a canned config or error.
type stubChainSource struct {
	cfg WebSearchConfig
	err error
}

func (s stubChainSource) WebSearchConfig(context.Context) (WebSearchConfig, error) {
	return s.cfg, s.err
}

// TestWebSearch_NoProviderConfigured pins the acceptance behavior: with an
// empty chain config the tool builds but returns the descriptive
// not-configured error result instead of failing the turn.
func TestWebSearch_NoProviderConfigured(t *testing.T) {
	tl, err := NewWebSearch(WithSearchConfigSource(stubChainSource{cfg: WebSearchConfig{}}))
	require.NoError(t, err, "web_search must build with an unconfigured chain")

	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.ErrorIs(t, err, errWebSearchNotConfigured)
	require.Contains(t, err.Error(), "add a provider in Settings → Tools")

	// The memoized resolver serves the identical error on repeat calls.
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.ErrorIs(t, err, errWebSearchNotConfigured)
}

func TestWebSearch_ConfigSourceServesChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"S","url":"https://s.example","content":"snippet"}]}`)
	}))
	defer srv.Close()

	cfg := WebSearchConfig{Entries: []WebSearchChainEntry{
		{Name: "Local SearXNG", Provider: SearchProviderSearXNG, BaseURL: srv.URL},
	}}
	tl, err := NewWebSearch(WithSearchConfigSource(stubChainSource{cfg: cfg}))
	require.NoError(t, err)
	out, err := tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.NoError(t, err)
	require.Contains(t, out, "https://s.example")
}

func TestWebSearch_ConfigSourceError(t *testing.T) {
	tl, err := NewWebSearch(WithSearchConfigSource(stubChainSource{err: errors.New("db boom")}))
	require.NoError(t, err)
	_, err = tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"test"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load chain config")
	require.Contains(t, err.Error(), "db boom")
}

// TestSearchProviders_Shape pins each provider's request/parse pair against a
// fake HTTP client: the canned upstream body must map to the expected
// provider-independent results.
func TestSearchProviders_Shape(t *testing.T) {
	tests := []struct {
		name     string
		provider func(HTTPClient) SearchProvider
		body     string
		want     []SearchResult
	}{
		{
			name: "brave",
			provider: func(c HTTPClient) SearchProvider {
				return &braveProvider{client: c, apiKey: "k"}
			},
			body: `{"web":{"results":[{"title":"B","url":"https://b.example","description":"desc"}]}}`,
			want: []SearchResult{{Title: "B", URL: "https://b.example", Snippet: "desc"}},
		},
		{
			name: "exa",
			provider: func(c HTTPClient) SearchProvider {
				return &exaProvider{client: c, apiKey: "k"}
			},
			body: `{"results":[{"title":"E","url":"https://e.example","text":"text"}]}`,
			want: []SearchResult{{Title: "E", URL: "https://e.example", Snippet: "text"}},
		},
		{
			name: "perplexity",
			provider: func(c HTTPClient) SearchProvider {
				return &perplexityProvider{client: c, apiKey: "k"}
			},
			body: `{"choices":[{"message":{"content":"answer"}}],"citations":["https://c.example"]}`,
			want: []SearchResult{
				{Title: "q", Snippet: "answer"},
				{Title: "https://c.example", URL: "https://c.example"},
			},
		},
		{
			name: "firecrawl",
			provider: func(c HTTPClient) SearchProvider {
				return &firecrawlProvider{client: c, apiKey: "k"}
			},
			body: `{"data":[{"title":"F","url":"https://f.example","description":"desc"}]}`,
			want: []SearchResult{{Title: "F", URL: "https://f.example", Snippet: "desc"}},
		},
		{
			name: "searxng",
			provider: func(c HTTPClient) SearchProvider {
				return &searxngProvider{client: c, baseURL: "http://searxng.internal:8080"}
			},
			body: `{"results":[{"title":"S","url":"https://s.example","content":"cont"}]}`,
			want: []SearchResult{{Title: "S", URL: "https://s.example", Snippet: "cont"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.provider(&searchFakeClient{status: http.StatusOK, body: tt.body}).
				Search(context.Background(), "q", 5)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestSearchProviders_StatusError pins the typed non-200 error every provider
// surfaces through doJSONSearch, rendered under the provider's own name.
func TestSearchProviders_StatusError(t *testing.T) {
	tests := []struct {
		name     string
		provider func(HTTPClient) SearchProvider
	}{
		{name: "brave", provider: func(c HTTPClient) SearchProvider { return &braveProvider{client: c, apiKey: "k"} }},
		{name: "exa", provider: func(c HTTPClient) SearchProvider { return &exaProvider{client: c, apiKey: "k"} }},
		{name: "perplexity", provider: func(c HTTPClient) SearchProvider { return &perplexityProvider{client: c, apiKey: "k"} }},
		{name: "firecrawl", provider: func(c HTTPClient) SearchProvider { return &firecrawlProvider{client: c, apiKey: "k"} }},
		{name: "searxng", provider: func(c HTTPClient) SearchProvider {
			return &searxngProvider{client: c, baseURL: "http://searxng.internal:8080"}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.provider(&searchFakeClient{status: http.StatusTooManyRequests, body: `{}`}).
				Search(context.Background(), "q", 5)
			require.Error(t, err)
			require.EqualError(t, err, fmt.Sprintf("web_search: %s returned status 429", tt.name))
		})
	}
}

func TestSearXNGProvider_TruncatesToNum(t *testing.T) {
	p := &searxngProvider{
		client: &searchFakeClient{
			status: http.StatusOK,
			body:   `{"results":[{"title":"1","url":"https://1.example"},{"title":"2","url":"https://2.example"}]}`,
		},
		baseURL: "http://searxng.internal:8080",
	}
	got, err := p.Search(context.Background(), "q", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "https://1.example", got[0].URL)
}

func TestWebSearch_EmptyResultsEncode(t *testing.T) {
	tl, err := NewWebSearch(WithSearchProvider(&fakeSearchProvider{results: []SearchResult{}}))
	require.NoError(t, err)
	out, err := tl.(tool.InvokableTool).InvokableRun(context.Background(), `{"query":"nothing"}`)
	require.NoError(t, err)
	require.Equal(t, `{"results": []}`, out)
}
